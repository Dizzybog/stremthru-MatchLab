package znabsearch

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MunifTanjim/stremthru/internal/torrent_stream"
	"github.com/MunifTanjim/stremthru/internal/torznab/generic"
	"github.com/stretchr/testify/require"
)

const testCaps = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="test"/>
  <searching>
    <search available="yes" supportedParams="q,year"/>
    <tv-search available="yes" supportedParams="q,imdbid,season,ep,year"/>
    <movie-search available="yes" supportedParams="q,imdbid,year"/>
  </searching>
  <categories><category id="5000" name="TV"/></categories>
</caps>`

func testIndexer(t *testing.T) *generic.TorznabClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(testCaps))
	}))
	t.Cleanup(server.Close)
	return generic.NewClient(&generic.TorznabClientConfig{BaseURL: server.URL, APIKey: "key", Name: "test"})
}

// A title-level series id (no season or episode) searches the whole series:
// tvsearch by IMDb id, without season, episode or year.
func TestWholeSeriesIdSearchesAllSeasons(t *testing.T) {
	client := testIndexer(t)
	nsid := &torrent_stream.NormalizedStremId{Id: "tt0903747"}
	meta := &QueryMeta{Titles: []string{"Breaking Bad"}, Year: 2008, IsSeries: true}

	queries, err := BuildQueriesForTorznab(client, QueryBuilderConfig{Meta: meta, NSId: nsid})
	require.NoError(t, err)
	require.Len(t, queries["tt0903747"], 1)
	q := queries["tt0903747"][0].Query
	require.Equal(t, "tvsearch", q.Get("t"))
	require.Equal(t, "tt0903747", q.Get("imdbid"))
	require.Empty(t, q.Get("season"))
	require.Empty(t, q.Get("ep"))
	require.False(t, queries["tt0903747"][0].IsExact)

	// Query mode: the title alone, without the first-air year.
	queries, err = BuildQueriesForTorznab(client, QueryBuilderConfig{Meta: meta, NSId: nsid, SearchMode: "query"})
	require.NoError(t, err)
	require.Len(t, queries["tt0903747"], 1)
	q = queries["tt0903747"][0].Query
	require.Equal(t, "Breaking Bad", q.Get("q"))
	require.Empty(t, q.Get("year"))

	// A movie keeps its movie search.
	movie := &QueryMeta{Titles: []string{"Dune"}, Year: 2021}
	queries, err = BuildQueriesForTorznab(client, QueryBuilderConfig{Meta: movie,
		NSId: &torrent_stream.NormalizedStremId{Id: "tt1160419"}})
	require.NoError(t, err)
	require.Equal(t, "movie", queries["tt1160419"][0].Query.Get("t"))
}
