package torrent_info

import (
	"strings"
	"testing"
	"time"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/MunifTanjim/stremthru/internal/matchlab"
	"github.com/stretchr/testify/require"
)

// The pending queue is read in hash order from a cursor, skipping rows that
// wait for a future retry, and including retries that are due.
func TestGetMatchLabPendingHashesUsesCursorAndRetries(t *testing.T) {
	setupTestDB(t)
	insert := func(digit string, retry any) string {
		hash := strings.Repeat(digit, 40)
		_, err := db.Exec(
			"INSERT INTO torrent_info (hash, t_title, src, parser_input, parser_version, matchlab_retry_at) VALUES (?, ?, ?, ?, ?, ?)",
			hash, "Title "+digit, "dht", "Title "+digit, matchlab.ParserVersion, retry,
		)
		require.NoError(t, err)
		return hash
	}
	a := insert("a", nil)
	insert("b", db.Timestamp{Time: time.Now().Add(time.Hour)}) // future retry
	c := insert("c", db.Timestamp{Time: time.Now().Add(-time.Hour)})
	d := insert("d", nil)
	e := insert("e", nil)

	// The due retry comes first, then never-tried rows from the cursor.
	page, next, err := GetMatchLabPendingHashes("", 2)
	require.NoError(t, err)
	require.Equal(t, []string{c, a}, pendingHashes(page))
	require.Equal(t, a, next)

	_, err = db.Exec("UPDATE torrent_info SET matchlab_mapped_at = "+db.CurrentTimestamp+" WHERE hash IN (?, ?)", a, c)
	require.NoError(t, err)
	page, next, err = GetMatchLabPendingHashes(next, 2)
	require.NoError(t, err)
	require.Equal(t, []string{d, e}, pendingHashes(page))
	require.Equal(t, e, next)

	page, next, err = GetMatchLabPendingHashes(next, 2)
	require.NoError(t, err)
	require.Empty(t, page)
	require.Equal(t, e, next)
}

func pendingHashes(items []MatchLabPendingHash) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Hash
	}
	return out
}

// Both pending queries must be served by their partial indexes.
func TestMatchLabPendingQueriesUsePartialIndexes(t *testing.T) {
	setupTestDB(t)
	plan := func(query string, args ...any) string {
		rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
		require.NoError(t, err)
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			require.NoError(t, rows.Scan(&id, &parent, &notused, &detail))
			out = append(out, detail)
		}
		return strings.Join(out, "\n")
	}
	require.Contains(t, plan("SELECT hash FROM torrent_info WHERE matchlab_mapped_at IS NULL AND matchlab_retry_at IS NULL AND t_title = parser_input AND parser_version = ? AND hash > ? ORDER BY hash LIMIT ?", matchlab.ParserVersion, "", 200),
		"idx_torrent_info_matchlab_pending_fresh")
	require.Contains(t, plan("SELECT hash FROM torrent_info WHERE matchlab_mapped_at IS NULL AND matchlab_retry_at IS NOT NULL AND matchlab_retry_at <= "+db.CurrentTimestamp+" AND t_title = parser_input AND parser_version = ? ORDER BY matchlab_retry_at LIMIT ?", matchlab.ParserVersion, 200),
		"idx_torrent_info_matchlab_pending_retry")
}
