package torznab_indexer_syncinfo

import (
	"context"
	"testing"

	"github.com/MunifTanjim/stremthru/internal/logger"
)

func TestFeedScrapeQueryRequestsLatestReleases(t *testing.T) {
	query := feedScrapeQuery()
	if got := query.Get("t"); got != "search" {
		t.Fatalf("expected t=search, got %q", got)
	}
	if got := query.Get("q"); got != "" {
		t.Fatalf("expected blank search query, got %q", got)
	}
	if _, ok := query["q"]; !ok {
		t.Fatal("expected empty q parameter to be explicitly present")
	}
	if got := query.Get("limit"); got != "100" {
		t.Fatalf("expected a bounded feed size of 100, got %q", got)
	}
	if len(query) != 3 {
		t.Fatalf("expected only t, q and limit parameters, got %v", query)
	}
}

// Each release is identified; each distinct title id is queued once and
// unidentified releases queue nothing.
func TestQueueFeedTitlesQueuesEachTitleOnce(t *testing.T) {
	previous := identifyFeedTitle
	t.Cleanup(func() { identifyFeedTitle = previous })
	ids := map[string]string{
		"Breaking.Bad.S01E01.1080p":              "tt0903747",
		"Breaking.Bad.S01E02.1080p":              "tt0903747",
		"[SubsPlease] Sousou no Frieren - 05":    "anidb:17617",
		"Some.Unknown.Release.2026.1080p.WEB-DL": "",
	}
	identifyFeedTitle = func(_ context.Context, title string) (string, error) {
		return ids[title], nil
	}
	titles := []string{}
	for title := range ids {
		titles = append(titles, title)
	}
	if got := queueFeedTitles(context.Background(), titles, logger.Scoped("test")); got != 2 {
		t.Fatalf("queued %d distinct titles, want 2", got)
	}
}
