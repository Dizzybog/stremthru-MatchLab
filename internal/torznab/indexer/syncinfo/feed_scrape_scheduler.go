package torznab_indexer_syncinfo

import (
	"context"
	"encoding/hex"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/MunifTanjim/stremthru/internal/imdb_title"
	"github.com/MunifTanjim/stremthru/internal/job"
	"github.com/MunifTanjim/stremthru/internal/logger"
	"github.com/MunifTanjim/stremthru/internal/matchlab"
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
		"t":     {"search"},
		"q":     {""},
		"limit": {"100"},
	}
}

type feedScrapeResult struct {
	status         string
	fetched        int
	eligible       int
	rejected       int
	magnetAttempts int
	magnetFailures int
	titlesQueued   int
	duration       time.Duration
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
		started := time.Now()
		log := j.Logger()
		// Do not begin an ingestion batch while a MatchLab reset is active.
		db.WaitForMaintenance()

		indexers, err := torznab_indexer.GetAllEnabled()
		if err != nil {
			log.Error("failed to list enabled feed indexers", "error", err)
			return err
		}
		var optedIn []*torznab_indexer.TorznabIndexer
		for i := range indexers {
			if indexers[i].ScrapeFeed {
				optedIn = append(optedIn, &indexers[i])
			}
		}
		if len(optedIn) == 0 {
			log.Debug("no indexers configured for feed scraping", "enabled_indexers", len(indexers))
			return nil
		}

		log.Info("torznab feed scrape cycle started",
			"indexers", len(optedIn), "enabled_indexers", len(indexers),
			"interval", feedScrapeInterval.String(), "max_items_per_indexer", feedScrapeMaxItems)

		var completed, failed, skipped, fetched, eligible, rejected atomic.Int64
		pool := pond.NewPool(3)
		for _, indexer := range optedIn {
			pool.Submit(func() {
				result := scrapeTorznabFeed(indexer, log)
				switch result.status {
				case "done":
					completed.Add(1)
				case "skipped":
					skipped.Add(1)
				default:
					failed.Add(1)
				}
				fetched.Add(int64(result.fetched))
				eligible.Add(int64(result.eligible))
				rejected.Add(int64(result.rejected))
			})
		}
		err = pool.Stop().Wait()
		log.Info("torznab feed scrape cycle finished",
			"indexers", len(optedIn),
			"completed", completed.Load(), "skipped", skipped.Load(), "failed", failed.Load(),
			"fetched", fetched.Load(), "eligible", eligible.Load(), "rejected", rejected.Load(),
			"duration", time.Since(started).String())
		if err != nil {
			log.Error("torznab feed scrape cycle failed", "error", err)
		}
		return err
	},
})

func scrapeTorznabFeed(indexer *torznab_indexer.TorznabIndexer, log *logger.Logger) (result feedScrapeResult) {
	started := time.Now()
	result.status = "failed"
	log.Info("torznab feed scrape started", "indexer_id", indexer.Id, "indexer", indexer.Name, "type", indexer.Type)
	defer func() {
		result.duration = time.Since(started)
		log.Info("torznab feed scrape finished",
			"indexer_id", indexer.Id, "indexer", indexer.Name,
			"status", result.status, "fetched", result.fetched, "eligible", result.eligible,
			"rejected", result.rejected, "magnet_attempts", result.magnetAttempts,
			"magnet_failures", result.magnetFailures, "titles_queued", result.titlesQueued,
			"duration", result.duration.String())
	}()

	// The checkbox or global enabled state can change between scheduling
	// and execution; respect any such change before making HTTP requests.
	current, err := torznab_indexer.GetById(indexer.Id)
	if err != nil {
		log.Error("failed to refresh feed indexer", "indexer_id", indexer.Id, "indexer", indexer.Name, "error", err)
		return result
	}
	if current == nil || current.Disabled || !current.ScrapeFeed {
		result.status = "skipped"
		log.Info("feed scrape skipped: indexer disabled or opt-in removed", "indexer_id", indexer.Id, "indexer", indexer.Name)
		return result
	}
	indexer = current

	limiter, err := indexer.GetRateLimiter()
	if err != nil {
		log.Error("failed to get feed rate limiter", "indexer_id", indexer.Id, "indexer", indexer.Name, "error", err)
		return result
	}
	if limiter != nil {
		allowed, err := limiter.Try()
		if err != nil {
			log.Warn("feed rate-limit check failed", "indexer_id", indexer.Id, "indexer", indexer.Name, "error", err)
			return result
		}
		if !allowed.Allowed {
			result.status = "skipped"
			log.Info("feed scrape skipped: rate limited", "indexer_id", indexer.Id, "indexer", indexer.Name,
				"retry_after", allowed.RetryAfter.String())
			return result
		}
	}

	client, err := indexer.GetClient()
	if err != nil {
		log.Error("failed to get feed indexer client", "indexer_id", indexer.Id, "indexer", indexer.Name, "error", err)
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), feedScrapeTimeout)
	defer cancel()

	log.Debug("requesting latest torznab feed", "indexer_id", indexer.Id, "indexer", indexer.Name,
		"search_type", "search", "query_empty", true, "limit", feedScrapeMaxItems)
	searchStarted := time.Now()
	results, err := client.Search(ctx, feedScrapeQuery())
	if err != nil {
		log.Warn("feed scrape search failed", "indexer_id", indexer.Id, "indexer", indexer.Name,
			"duration", time.Since(searchStarted).String(), "error", err)
		return result
	}
	result.fetched = len(results)
	log.Debug("torznab feed search succeeded", "indexer_id", indexer.Id, "indexer", indexer.Name,
		"fetched", result.fetched, "search_duration", time.Since(searchStarted).String())

	// Some indexers ignore the requested limit. Bound both database writes
	// and any torrent-file downloads per scheduled run.
	if len(results) > feedScrapeMaxItems {
		log.Info("torznab feed results truncated", "indexer_id", indexer.Id, "indexer", indexer.Name,
			"returned", len(results), "max_items", feedScrapeMaxItems)
		results = results[:feedScrapeMaxItems]
	}

	// Reuse the existing Torznab magnet-resolution behavior. Missing magnet
	// metadata may hide the file list required for torrent_stream mapping.
	var magnetFailures atomic.Int64
	fetchPool := pond.NewPool(4)
	for i := range results {
		item := &results[i]
		if item.HasMissingData() && item.SourceLink != "" {
			result.magnetAttempts++
			fetchPool.Submit(func() {
				if err := item.EnsureMagnet(ctx); err != nil {
					magnetFailures.Add(1)
					log.Debug("failed to resolve feed magnet",
						"indexer_id", indexer.Id, "indexer", indexer.Name, "error", err)
				}
			})
		}
	}
	if err := fetchPool.Stop().Wait(); err != nil {
		log.Warn("failed to finish feed magnet downloads", "indexer_id", indexer.Id, "indexer", indexer.Name, "error", err)
	}
	result.magnetFailures = int(magnetFailures.Load())

	items := make([]torrent_info.TorrentItem, 0, len(results))
	for i := range results {
		item := &results[i]
		if item.Title == "" || len(item.Hash) != 40 {
			result.rejected++
			continue
		}
		if _, err := hex.DecodeString(item.Hash); err != nil {
			result.rejected++
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
	result.eligible = len(items)
	log.Debug("torznab feed candidates prepared",
		"indexer_id", indexer.Id, "indexer", indexer.Name,
		"eligible", result.eligible, "rejected", result.rejected,
		"magnet_attempts", result.magnetAttempts, "magnet_failures", result.magnetFailures)

	// A feed has no title identity yet. Leave category unknown so the normal
	// parser and MatchLab mapping workers can classify the newly seen torrent.
	if len(items) > 0 {
		db.WaitForMaintenance()
		updated, err := torznab_indexer.GetById(indexer.Id)
		if err != nil {
			log.Error("failed to recheck feed indexer before saving", "indexer_id", indexer.Id, "indexer", indexer.Name, "error", err)
			return result
		}
		if updated == nil || updated.Disabled || !updated.ScrapeFeed {
			result.status = "skipped"
			log.Info("feed scrape save skipped: indexer disabled", "indexer_id", indexer.Id, "indexer", indexer.Name)
			return result
		}
		saveStarted := time.Now()
		if err := torrent_info.Upsert(items, torrent_info.TorrentInfoCategoryUnknown, false); err != nil {
			log.Error("failed to save scraped feed torrents", "indexer_id", indexer.Id, "indexer", indexer.Name,
				"error", err, "eligible", len(items), "duration", time.Since(saveStarted).String())
			return result
		}
		log.Debug("torznab feed candidates upserted", "indexer_id", indexer.Id, "indexer", indexer.Name,
			"submitted", len(items), "duration", time.Since(saveStarted).String())
	}
	// Identify each release's title (MatchLab, else the local IMDb matcher)
	// and queue it for the regular indexer search, exactly as a request for
	// the title would, so the title's other releases are found too.
	// Own deadline: the search and magnet downloads may have used most of ctx.
	identifyCtx, cancelIdentify := context.WithTimeout(context.Background(), feedScrapeTimeout)
	defer cancelIdentify()
	result.titlesQueued = queueFeedTitles(identifyCtx, items, log)
	// 'eligible' is not a new-torrent count: Upsert deduplicates existing hashes.
	result.status = "done"
	return result
}

// identifyFeedTitle returns the title-level stream id of a release: the
// IMDb id, or "anidb:<id>" when only an AniDB identity is known. The local
// matcher parses the title and searches the IMDb catalog, like the
// map-imdb-torrent worker does without MatchLab.
var identifyFeedTitle = func(ctx context.Context, title string) (string, error) {
	if matchlab.Enabled() {
		match, err := matchlab.Default.Match(ctx, title)
		if err != nil {
			return "", err
		}
		for _, sid := range []*string{match.SIDStart, match.SIDEnd} {
			if sid != nil && strings.HasPrefix(*sid, "tt") {
				id, _, _ := strings.Cut(*sid, ":")
				return id, nil
			}
		}
		for _, asid := range []*string{match.ASIDStart, match.ASIDEnd} {
			if asid != nil && *asid != "" {
				id, _, _ := strings.Cut(*asid, ":")
				return "anidb:" + id, nil
			}
		}
		return "", nil
	}
	tInfo := torrent_info.TorrentInfo{TorrentTitle: title}
	if err := tInfo.Parse(); err != nil || tInfo.Title == "" {
		return "", err
	}
	titleType := imdb_title.SearchTitleTypeMovie
	if len(tInfo.Seasons) > 0 || len(tInfo.Episodes) > 0 {
		titleType = imdb_title.SearchTitleTypeShow
	}
	found, err := imdb_title.SearchOne(tInfo.Title, titleType, tInfo.Year, false)
	if err != nil || found == nil {
		return "", err
	}
	if titleType == imdb_title.SearchTitleTypeShow && !imdb_title.IMDBTitleType(found.Type).IsShow() {
		return "", nil
	}
	return found.TId, nil
}

// queueFeedTitles queues each distinct title of a feed once.
func queueFeedTitles(ctx context.Context, items []torrent_info.TorrentItem, log *logger.Logger) int {
	var mu sync.Mutex
	seen := map[string]struct{}{}
	pool := pond.NewPool(4)
	for i := range items {
		title := items[i].TorrentTitle
		pool.Submit(func() {
			sid, err := identifyFeedTitle(ctx, title)
			if err != nil {
				log.Debug("failed to identify feed title", "title", title, "error", err)
				return
			}
			if sid == "" {
				return
			}
			mu.Lock()
			_, queued := seen[sid]
			seen[sid] = struct{}{}
			mu.Unlock()
			if !queued {
				QueueJob(sid)
			}
		})
	}
	_ = pool.Stop().Wait()
	return len(seen)
}
