package stremio_store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/MunifTanjim/stremthru/internal/matchlab"
	"github.com/MunifTanjim/stremthru/internal/torrent_stream"
	"github.com/MunifTanjim/stremthru/store"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func setupStoreTestDB(t *testing.T) {
	t.Helper()
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(sqlDB, "../../../migrations/sqlite"))
	db.SetForTesting(sqlDB, t.Cleanup)
}

func withMatchLab(t *testing.T, enabled bool) {
	t.Helper()
	prev := matchlab.Default
	t.Cleanup(func() { matchlab.Default = prev })
	if !enabled {
		matchlab.Default = nil
		return
	}
	client, err := matchlab.NewClient("http://matchlab.invalid", time.Second, 1)
	require.NoError(t, err)
	matchlab.Default = client
}

func insertStreamFile(t *testing.T, hash, path string, idx int, size int64, sid, asid string) {
	t.Helper()
	_, err := db.Exec("INSERT INTO torrent_stream (h, p, i, s, sid, asid, src, vhash) VALUES (?, ?, ?, ?, ?, ?, 'tb', '')",
		hash, path, idx, size, sid, asid)
	require.NoError(t, err)
}

// A library item whose name does not resemble the requested title, and whose
// file uses absolute numbering, is still found through its MatchLab identity.
func TestStoreUsesMatchLabFileIdentity(t *testing.T) {
	setupStoreTestDB(t)
	withMatchLab(t, true)

	hash := "0123456789abcdef0123456789abcdef01234567"
	insertStreamFile(t, hash, "/[Grp] Sousou no Frieren/[Grp] Sousou no Frieren - 05 [1080p].mkv", 0, 1_400_000_000, "tt22248376:1:5", "17617:5")
	insertStreamFile(t, hash, "/[Grp] Sousou no Frieren/[Grp] Sousou no Frieren - 06 [1080p].mkv", 1, 1_300_000_000, "tt22248376:1:6", "17617:6")

	items := []CachedCatalogItem{{Hash: hash}, {Hash: ""}}
	idr := &ParsedId{}

	files := getMatchLabFiles(idr, items, "tt22248376:1:5", log)
	require.Len(t, files, 1)
	match := matchedFileFor(files, hash)
	require.NotNil(t, match)
	require.Equal(t, 0, match.Idx)
	require.Nil(t, matchedFileFor(files, ""))

	anime := getMatchLabFiles(idr, items, "17617:6", log)
	require.Equal(t, 1, anime[hash].Idx)

	storeFiles := []store.MagnetFile{
		{Idx: 0, Path: "/[Grp] Sousou no Frieren/[Grp] Sousou no Frieren - 06 [1080p].mkv", Name: "[Grp] Sousou no Frieren - 06 [1080p].mkv", Size: 1_300_000_000},
		{Idx: 1, Path: "/[Grp] Sousou no Frieren/[Grp] Sousou no Frieren - 05 [1080p].mkv", Name: "[Grp] Sousou no Frieren - 05 [1080p].mkv", Size: 1_400_000_000},
	}
	found := findMatchedStoreFile(storeFiles, match)
	require.NotNil(t, found)
	require.Equal(t, 1, found.Idx, "matched by path, not by store index")
}

func TestFindMatchedStoreFileAcceptsNameOnlyPaths(t *testing.T) {
	match := &torrent_stream.File{Path: "/Pack/Show - 05.mkv", Name: "Show - 05.mkv", Size: 500}
	storeFiles := []store.MagnetFile{
		{Idx: 0, Path: "Show - 05.mkv", Name: "Show - 05.mkv", Size: 499},
		{Idx: 1, Path: "Show - 05.mkv", Name: "Show - 05.mkv", Size: 500},
	}
	found := findMatchedStoreFile(storeFiles, match)
	require.NotNil(t, found)
	require.Equal(t, 1, found.Idx)
	require.Nil(t, findMatchedStoreFile(storeFiles[:1], match))
}

func TestStoreSkipsMatchLabWhenDisabledOrNotTorrent(t *testing.T) {
	setupStoreTestDB(t)
	hash := "0123456789abcdef0123456789abcdef01234567"
	insertStreamFile(t, hash, "/Movie.2020.mkv", 0, 1000, "tt0000001", "")
	items := []CachedCatalogItem{{Hash: hash}}

	withMatchLab(t, false)
	require.Empty(t, getMatchLabFiles(&ParsedId{}, items, "tt0000001", log))

	withMatchLab(t, true)
	require.Len(t, getMatchLabFiles(&ParsedId{}, items, "tt0000001", log), 1)
	require.Empty(t, getMatchLabFiles(&ParsedId{isUsenet: true}, items, "tt0000001", log))
	require.Empty(t, getMatchLabFiles(&ParsedId{isWebDL: true}, items, "tt0000001", log))
}
