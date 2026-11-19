package torznab_indexer_syncinfo

import "testing"

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
