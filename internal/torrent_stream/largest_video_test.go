package torrent_stream

import (
	"database/sql"
	"testing"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func TestLargestMatchingVideoSkipsSamplesAndOtherEpisodes(t *testing.T) {
	files := []File{
		{Path: "/Show.S01E02.sample.mkv", Name: "Show.S01E02.sample.mkv", Idx: 0, Size: 40_000_000, SId: "tt1234567:1:2"},
		{Path: "/Show.S01E02.mkv", Name: "Show.S01E02.mkv", Idx: 1, Size: 2_000_000_000, SId: "tt1234567:1:2"},
		{Path: "/Show.S01E03.mkv", Name: "Show.S01E03.mkv", Idx: 2, Size: 3_000_000_000, SId: "tt1234567:1:3"},
		{Path: "/Show.S01E02.nfo", Name: "Show.S01E02.nfo", Idx: 3, Size: 9_000_000_000, SId: "tt1234567:1:2"},
	}
	best := LargestMatchingVideo(files, "tt1234567:1:2")
	require.NotNil(t, best)
	require.Equal(t, 1, best.Idx)
	require.Nil(t, LargestMatchingVideo(files, "tt1234567:1:9"))
	// Equal sizes: the lower index wins, deterministically.
	files[0].Size = files[1].Size
	require.Equal(t, 0, LargestMatchingVideo(files, "tt1234567:1:2").Idx)
}

func TestStreamListAndPlaybackPickTheLargestVideoForAnEpisode(t *testing.T) {
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(sqlDB, "../../migrations/sqlite"))
	db.SetForTesting(sqlDB, t.Cleanup)

	hash := "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	for _, f := range []struct {
		path string
		idx  int
		size int64
	}{
		{"/Show.S01E02/Sample/show.s01e02.sample.mkv", 0, 40_000_000},
		{"/Show.S01E02/Show.S01E02.1080p.mkv", 1, 2_000_000_000},
		{"/Show.S01E02/Sample/show.s01e02.sample2.mkv", 2, 30_000_000},
	} {
		_, err := db.Exec("INSERT INTO torrent_stream (h, p, i, s, sid, asid, src, vhash) VALUES (?, ?, ?, ?, ?, '', 'dht', '')",
			hash, f.path, f.idx, f.size, "tt1234567:1:2")
		require.NoError(t, err)
	}

	matched, err := GetMatchingFilesByHashes([]string{hash}, "tt1234567:1:2")
	require.NoError(t, err)
	require.Equal(t, 1, matched[hash].Idx, "the stream list shows the episode, not a sample")

	played, err := GetFile(hash, "tt1234567:1:2")
	require.NoError(t, err)
	require.NotNil(t, played)
	require.Equal(t, 1, played.Idx, "playback picks the same file as the stream list")
}
