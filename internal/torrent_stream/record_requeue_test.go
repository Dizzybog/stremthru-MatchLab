package torrent_stream

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/MunifTanjim/stremthru/internal/config"
	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func setupRecordTestDB(t *testing.T) {
	t.Helper()
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(sqlDB, "../../migrations/sqlite"))
	db.SetForTesting(sqlDB, t.Cleanup)
}

func markMapped(t *testing.T, hash string) {
	t.Helper()
	_, err := db.Exec("UPDATE torrent_info SET matchlab_mapped_at = "+db.CurrentTimestamp+" WHERE hash = ?", hash)
	require.NoError(t, err)
}

func mappingState(t *testing.T, hash string) (pending bool, revision int64) {
	t.Helper()
	var mappedAt sql.NullString
	require.NoError(t, db.QueryRow(
		"SELECT matchlab_mapped_at, matchlab_file_revision FROM torrent_info WHERE hash = ?", hash,
	).Scan(&mappedAt, &revision))
	return !mappedAt.Valid, revision
}

// Seeing an unchanged torrent again must not send it back to the MatchLab
// mapping queue; a changed file must.
func TestRecordRequeuesOnlyChangedTorrents(t *testing.T) {
	setupRecordTestDB(t)
	prevURL := config.Integration.MatchLab.URL
	config.Integration.MatchLab.URL = "http://matchlab.test"
	t.Cleanup(func() { config.Integration.MatchLab.URL = prevURL })

	hash := strings.Repeat("e", 40)
	_, err := db.Exec("INSERT INTO torrent_info (hash, t_title, src) VALUES (?, ?, ?)", hash, "Example S01E01", "dht")
	require.NoError(t, err)

	item := func(size int64) InsertData {
		return InsertData{Hash: hash, File: File{Path: "/Example.S01E01.mkv", Idx: 0, Size: size, Source: "dht"}}
	}

	require.NoError(t, Record([]InsertData{item(1000)}, false))
	pending, revision := mappingState(t, hash)
	require.True(t, pending, "a new file queues the torrent")
	markMapped(t, hash)

	// Bypass the in-memory write cache, as after its expiry or a restart.
	prevRecordCache.Remove(hash + ":/Example.S01E01.mkv")
	require.NoError(t, Record([]InsertData{item(1000)}, false))
	pending, again := mappingState(t, hash)
	require.False(t, pending, "an unchanged file keeps the mapping")
	require.Equal(t, revision, again)

	// A new file in the torrent requeues it.
	require.NoError(t, Record([]InsertData{
		item(1000),
		{Hash: hash, File: File{Path: "/Example.S01E02.mkv", Idx: 1, Size: 1000, Source: "dht"}},
	}, false))
	pending, again = mappingState(t, hash)
	require.True(t, pending, "a new file requeues the torrent")
	require.Equal(t, revision+1, again)
}
