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

func prepareMatchLabResetTest(t *testing.T) (hash, path string) {
	t.Helper()
	setupTestDB(t)
	hash = strings.Repeat("e", 40)
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
	for _, table := range []string{"imdb_torrent", "anidb_torrent"} {
		var count int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count))
		require.Zero(t, count, table)
	}
	var sid, asid string
	var index int
	var size int64
	require.NoError(t, db.QueryRow(
		"SELECT sid, asid, i, s FROM torrent_stream WHERE h = ? AND p = ?", hash, path,
	).Scan(&sid, &asid, &index, &size))
	require.Equal(t, "*", sid)
	require.Empty(t, asid)
	require.Equal(t, 3, index)
	require.Equal(t, int64(4096), size)

	// The previously cached non-empty identities must not survive.
	files, err := torrent_stream.GetFilesByHashes([]string{hash})
	require.NoError(t, err)
	require.Len(t, files[hash], 1)
	require.Equal(t, "*", files[hash][0].SId)
	require.Empty(t, files[hash][0].ASId)

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
	hash, path := prepareMatchLabResetTest(t)
	require.NoError(t, ResetAllMatchLabMappings(false))
	verifyMatchLabReset(t, hash, path, false)
}

func TestResetAllMatchLabMappingsAndParsing(t *testing.T) {
	hash, path := prepareMatchLabResetTest(t)
	require.NoError(t, ResetAllMatchLabMappings(true))
	verifyMatchLabReset(t, hash, path, true)
}
