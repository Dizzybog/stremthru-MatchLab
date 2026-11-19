package torznab_indexer_syncinfo

import (
	"context"
	"encoding/hex"
	"net/url"
	"time"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/MunifTanjim/stremthru/internal/job"
	"github.com/MunifTanjim/stremthru/internal/logger"
	"github.com/MunifTanjim/stremthru/internal/torrent_info"
	torznab_indexer "github.com/MunifTanjim/stremthru/internal/torznab/indexer"
	"github.com/alitto/pond/v2"
)

const (
	feedScrapeSchedulerId = "scrape-torznab-feed"
	feedScrapeInterval    = 10 * time.Minute
	feedScrapeMaxItems    = 100
	feedScrapeTimeout     = 45 * time.Second
)

// A blank Torznab search requests the newest releases rather than looking up
// a particular title. Keep q present but empty for indexers that expect it.
func feedScrapeQuery() url.Values {
	return url.Values{
		"t": {"search"},
		"q": {""},
		"limit": {"100"},
	}
}

// This is deliberately separate from the on-demand, SID-based indexer sync.
// Only explicitly opted-in, enabled indexers are polled.
var _ = job.NewScheduler(&job.SchedulerConfig[struct{}]{
	Id:                feedScrapeSchedulerId,
	Title:             "Scrape Torznab Feeds",
	Interval:          feedScrapeInterval,
	RunAtStartupAfter: 45 * time.Second,
	RunExclusive:      true,
	Disabled:          queue.IsDisabled(),
	ShouldSkip: func() bool {
		return !torznab_indexer.Exists()
	},
	Executor: func(j *job.Scheduler[struct{}]) error {
		// Do not begin an ingestion batch while a MatchLab reset is active.
		db.WaitForMaintenance()

		indexers, err := torznab_indexer.GetAllEnabled()
		if err != nil {
			return err
		}

		pool := pond.NewPool(3)
		for i := range indexers {
			indexer := &indexers[i]
			if !indexer.ScrapeFeed {
				continue
			}
			pool.Submit(func() {
				scrapeTorznabFeed(indexer, j.Logger())
			})
		}
		return pool.Stop().Wait()
	},
})

func scrapeTorznabFeed(indexer *torznab_indexer.TorznabIndexer, log *logger.Logger) {
	// The checkbox or global enabled state can change between scheduling
	// and execution; respect any such change before making HTTP requests.
	current, err := torznab_indexer.GetById(indexer.Id)
	if err != nil {
		log.Error("failed to refresh feed indexer", "id", indexer.Id, "error", err)
		return
	}
	if current == nil || current.Disabled || !current.ScrapeFeed {
		return
	}
	indexer = current

	limiter, err := indexer.GetRateLimiter()
	if err != nil {
		log.Error("failed to get feed rate limiter", "indexer", indexer.Name, "error", err)
		return
	}
	if limiter != nil {
		allowed, err := limiter.Try()
		if err != nil {
			log.Warn("feed rate-limit check failed", "indexer", indexer.Name, "error", err)
			return
		}
		if !allowed.Allowed {
			log.Debug("feed scrape skipped by rate limit", "indexer", indexer.Name, "retry_after", allowed.RetryAfter.String())
			return
		}
	}

	client, err := indexer.GetClient()
	if err != nil {
		log.Error("failed to get feed indexer client", "indexer", indexer.Name, "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), feedScrapeTimeout)
	defer cancel()

	results, err := client.Search(ctx, feedScrapeQuery())
	if err != nil {
		log.Warn("feed scrape search failed", "indexer", indexer.Name, "error", err)
		return
	}
	fetched := len(results)
	// Some indexers ignore the requested limit. Bound both database writes
	// and any torrent-file downloads per scheduled run.
	if len(results) > feedScrapeMaxItems {
		results = results[:feedScrapeMaxItems]
	}

	// Reuse the existing Torznab magnet-resolution behavior. Missing magnet
	// metadata may hide the file list required for torrent_stream mapping.
	fetchPool := pond.NewPool(4)
	for i := range results {
		item := &results[i]
		if item.HasMissingData() && item.SourceLink != "" {
			fetchPool.Submit(func() {
				if err := item.EnsureMagnet(ctx); err != nil {
					log.Debug("failed to resolve feed infohash", "indexer", indexer.Name, "error", err)
				}
			})
		}
	}
	if err := fetchPool.Stop().Wait(); err != nil {
		log.Warn("failed to finish feed torrent downloads", "indexer", indexer.Name, "error", err)
	}

	items := make([]torrent_info.TorrentItem, 0, len(results))
	for i := range results {
		item := &results[i]
		if item.Title == "" || len(item.Hash) != 40 {
			continue
		}
		if _, err := hex.DecodeString(item.Hash); err != nil {
			continue
		}
		if item.Indexer == "" {
			item.Indexer = client.GetName()
		}
		items = append(items, torrent_info.TorrentItem{
			Hash:         item.Hash,
			TorrentTitle: item.Title,
			Size:         item.Size,
			Indexer:      item.Indexer,
			Source:       torrent_info.TorrentInfoSourceIndexer,
			Seeders:      item.Seeders,
			Leechers:     item.Leechers,
			Private:      item.Private,
			Files:        item.Files,
		})
	}

	// A feed has no title identity yet. Leave category unknown so the normal
	// parser and MatchLab mapping workers can classify the newly seen torrent.
	if len(items) > 0 {
		db.WaitForMaintenance()
		if !torznab_indexer.IsEnabled(indexer.Id) {
			return
		}
		if err := torrent_info.Upsert(items, torrent_info.TorrentInfoCategoryUnknown, false); err != nil {
			log.Error("failed to save scraped feed torrents", "indexer", indexer.Name, "error", err, "count", len(items))
			return
		}
	}
	log.Info("scraped torznab feed", "indexer", indexer.Name, "fetched", fetched, "accepted", len(items))
}

