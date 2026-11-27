package torznab_indexer_syncinfo

import (
	"testing"

	tznc "github.com/MunifTanjim/stremthru/internal/torznab/client"
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

// Each distinct IMDb id of a feed is queued once; releases without one are
// left to the MatchLab mapping workers.
func TestQueueFeedIMDbIdsQueuesEachTitleOnce(t *testing.T) {
	results := []tznc.Torz{
		{Title: "Breaking.Bad.S01E01", IMDBId: "tt0903747"},
		{Title: "Breaking.Bad.S01E02", IMDBId: "tt0903747"},
		{Title: "The.Sopranos.S01E01", IMDBId: "tt0141842"},
		{Title: "Unknown.Release"},
	}
	if got := queueFeedIMDbIds(results); got != 2 {
		t.Fatalf("queued %d distinct titles, want 2", got)
	}
}
