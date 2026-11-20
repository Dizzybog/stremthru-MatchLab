package torrent_info

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/MunifTanjim/stremthru/internal/matchlab"
	"github.com/MunifTanjim/stremthru/internal/torrent_stream"
	"github.com/stretchr/testify/require"
)

func prepareMatchLabResetTest(t *testing.T, hashDigit string) (hash, path string) {
	t.Helper()
	setupTestDB(t)
	hash = strings.Repeat(hashDigit, 40)
	path = "/Series/S01E02.mkv"
	require.NoError(t, Upsert([]TorrentInfoInsertData{{
		Hash: hash, TorrentTitle: "Example Series S01E02", Source: TorrentInfoSourceDHT,
		Size: 4096, Files: torrent_stream.Files{{
			Path: path, Name: "S01E02.mkv", Idx: 3, Size: 4096, SId: "tt1234567:1:2", ASId: "42:2",
		}},
	}}, TorrentInfoCategorySeries, false))

	_, err := db.Exec(
		"UPDATE torrent_info SET parser_input = t_title, parser_version = ?, matchlab_mapped_at = "+
			db.CurrentTimestamp+", matchlab_file_revision = 3 WHERE hash = ?",
		matchlab.ParserVersion, hash,
	)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO imdb_torrent (hash, tid) VALUES (?, ?)", hash, "tt1234567")
	require.NoError(t, err)
	_, err = db.Exec(
		"INSERT INTO anidb_torrent (tid, hash, s_type, s, ep_start, ep_end, eps) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"42", hash, "ani", 1, 2, 2, "2",
	)
	require.NoError(t, err)
	// Verify the global reset invalidates a previously cached file list.
	_, err = torrent_stream.GetFilesByHashes([]string{hash})
	require.NoError(t, err)
	return hash, path
}

func verifyMatchLabReset(t *testing.T, hash, path string, includeParsing bool) {
	t.Helper()
	// Dashboard reset only invalidates MatchLab processing state. Existing
	// derived identities stay available until each new mapping replaces them.
	for _, table := range []string{"imdb_torrent", "anidb_torrent"} {
		var count int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE hash = ?", hash).Scan(&count))
		require.Equal(t, 1, count, table)
	}
	var sid, asid string
	var index int
	var size int64
	require.NoError(t, db.QueryRow(
		"SELECT sid, asid, i, s FROM torrent_stream WHERE h = ? AND p = ?", hash, path,
	).Scan(&sid, &asid, &index, &size))
	require.Equal(t, "tt1234567:1:2", sid)
	require.Equal(t, "42:2", asid)
	require.Equal(t, 3, index)
	require.Equal(t, int64(4096), size)

	files, err := torrent_stream.GetFilesByHashes([]string{hash})
	require.NoError(t, err)
	require.Len(t, files[hash], 1)
	require.Equal(t, sid, files[hash][0].SId)
	require.Equal(t, asid, files[hash][0].ASId)

	var parserVersion int
	var parserInput string
	var revision int64
	var mappedAt sql.NullInt64
	require.NoError(t, db.QueryRow(
		"SELECT parser_version, parser_input, matchlab_file_revision, matchlab_mapped_at FROM torrent_info WHERE hash = ?",
		hash,
	).Scan(&parserVersion, &parserInput, &revision, &mappedAt))
	require.False(t, mappedAt.Valid)
	require.Equal(t, int64(4), revision)

	pending, err := GetMatchLabPendingHashes(10)
	require.NoError(t, err)
	if includeParsing {
		require.Zero(t, parserVersion)
		require.Empty(t, parserInput)
		require.Empty(t, pending, "mapping must wait for the parser to run again")
		unparsed, err := GetUnparsed(10)
		require.NoError(t, err)
		require.Len(t, unparsed, 1)
		require.Equal(t, hash, unparsed[0].Hash)
	} else {
		require.Equal(t, matchlab.ParserVersion, parserVersion)
		require.Equal(t, "Example Series S01E02", parserInput)
		require.Len(t, pending, 1)
		require.Equal(t, hash, pending[0].Hash)
	}
}

func TestResetAllMatchLabMappingsOnly(t *testing.T) {
	hash, path := prepareMatchLabResetTest(t, "e")
	require.NoError(t, ResetAllMatchLabMappings(false))
	verifyMatchLabReset(t, hash, path, false)
}

func TestResetAllMatchLabMappingsAndParsing(t *testing.T) {
	hash, path := prepareMatchLabResetTest(t, "f")
	require.NoError(t, ResetAllMatchLabMappings(true))
	verifyMatchLabReset(t, hash, path, true)
}

func TestResetSelectedMatchLabMappingsDoesNotTouchOtherHashes(t *testing.T) {
	hash, path := prepareMatchLabResetTest(t, "9")
	otherHash := strings.Repeat("8", 40)
	otherPath := "/Other/S01E01.mkv"
	require.NoError(t, Upsert([]TorrentInfoInsertData{{
		Hash: otherHash, TorrentTitle: "Other Show S01E01", Source: TorrentInfoSourceDHT,
		Size: 1024, Files: torrent_stream.Files{{
			Path: otherPath, Name: "S01E01.mkv", Size: 1024, SId: "tt7777777:1:1", ASId: "777:1",
		}},
	}}, TorrentInfoCategorySeries, false))
	_, err := db.Exec(
		"UPDATE torrent_info SET parser_input = t_title, parser_version = ?, matchlab_mapped_at = "+
			db.CurrentTimestamp+", matchlab_file_revision = 9 WHERE hash = ?",
		matchlab.ParserVersion, otherHash,
	)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO imdb_torrent (hash, tid) VALUES (?, ?)", otherHash, "tt7777777")
	require.NoError(t, err)

	require.NoError(t, ResetMatchLabMappingsByHashes([]string{hash}, true))

	var sid, asid string
	require.NoError(t, db.QueryRow(
		"SELECT sid, asid FROM torrent_stream WHERE h = ? AND p = ?", hash, path,
	).Scan(&sid, &asid))
	require.Equal(t, "*", sid)
	require.Empty(t, asid)
	var affected int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM imdb_torrent WHERE hash = ?", hash).Scan(&affected))
	require.Zero(t, affected)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM anidb_torrent WHERE hash = ?", hash).Scan(&affected))
	require.Zero(t, affected)

	var version int
	var revision int64
	var mappedAt sql.NullTime
	require.NoError(t, db.QueryRow(
		"SELECT parser_version, matchlab_mapped_at, matchlab_file_revision FROM torrent_info WHERE hash = ?", hash,
	).Scan(&version, &mappedAt, &revision))
	require.Zero(t, version)
	require.False(t, mappedAt.Valid)
	require.Equal(t, int64(4), revision)

	// The other hash stays mapped. This endpoint must not emulate a global reset.
	require.NoError(t, db.QueryRow(
		"SELECT sid, asid FROM torrent_stream WHERE h = ? AND p = ?", otherHash, otherPath,
	).Scan(&sid, &asid))
	require.Equal(t, "tt7777777:1:1", sid)
	require.Equal(t, "777:1", asid)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM imdb_torrent WHERE hash = ?", otherHash).Scan(&affected))
	require.Equal(t, 1, affected)
	require.NoError(t, db.QueryRow(
		"SELECT parser_version, matchlab_mapped_at, matchlab_file_revision FROM torrent_info WHERE hash = ?", otherHash,
	).Scan(&version, &mappedAt, &revision))
	require.Equal(t, matchlab.ParserVersion, version)
	require.True(t, mappedAt.Valid)
	require.Equal(t, int64(9), revision)

	// Existing torrent file cache must reflect the cleared SID/ASID.
	files, err := torrent_stream.GetFilesByHashes([]string{hash})
	require.NoError(t, err)
	require.Len(t, files[hash], 1)
	require.Equal(t, "*", files[hash][0].SId)
	require.Empty(t, files[hash][0].ASId)

	toParse, err := GetUnparsed(10)
	require.NoError(t, err)
	// Tests run with the native parser disabled: its queue can also include
	// other rows parsed by MatchLab. Verify inclusion, not total queue size.
	found := false
	for _, item := range toParse {
		if item.Hash == hash {
			found = true
		}
	}
	require.True(t, found, "selected torrent must be queued for reparsing")
}

func TestResetSelectedMatchLabMappingOnlyPreservesParser(t *testing.T) {
	hash, path := prepareMatchLabResetTest(t, "7")
	require.NoError(t, ResetMatchLabMappingsByHashes([]string{hash}, false))

	var version int
	var parserInput string
	var mappedAt sql.NullInt64
	require.NoError(t, db.QueryRow(
		"SELECT parser_version, parser_input, matchlab_mapped_at FROM torrent_info WHERE hash = ?", hash,
	).Scan(&version, &parserInput, &mappedAt))
	require.Equal(t, matchlab.ParserVersion, version)
	require.Equal(t, "Example Series S01E02", parserInput)
	require.False(t, mappedAt.Valid)

	var sid string
	require.NoError(t, db.QueryRow("SELECT sid FROM torrent_stream WHERE h = ? AND p = ?", hash, path).Scan(&sid))
	require.Equal(t, "*", sid)

	pending, err := GetMatchLabPendingHashes(10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, hash, pending[0].Hash)
}

func TestResetAllMatchLabMappingsReportsAffectedRows(t *testing.T) {
	hash, path := prepareMatchLabResetTest(t, "5")
	stats, err := ResetAllMatchLabMappingsWithStats(false)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.Torrents)

	var sid string
	require.NoError(t, db.QueryRow("SELECT sid FROM torrent_stream WHERE h = ? AND p = ?", hash, path).Scan(&sid))
	require.Equal(t, "tt1234567:1:2", sid)

	// An additional reset must invalidate any match that was in flight during
	// the earlier reset, without clearing the stream or the identity tables.
	stats, err = ResetAllMatchLabMappingsWithStats(false)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.Torrents)
	var revision int64
	require.NoError(t, db.QueryRow(
		"SELECT matchlab_file_revision FROM torrent_info WHERE hash = ?", hash,
	).Scan(&revision))
	require.Equal(t, int64(5), revision)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM imdb_torrent WHERE hash = ?", hash).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM anidb_torrent WHERE hash = ?", hash).Scan(&count))
	require.Equal(t, 1, count)
}
