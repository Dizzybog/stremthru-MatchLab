package torznab_indexer_syncinfo

import (
	"database/sql"
	"testing"
	"time"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

// Only finished searches older than the cutoff are deleted; recent and
// pending searches stay.
func TestDeleteSyncedBeforeKeepsRecentAndPendingSearches(t *testing.T) {
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(sqlDB, "../../../../migrations/sqlite"))
	db.SetForTesting(sqlDB, t.Cleanup)

	now := time.Now()
	insert := func(sid string, status Status, synced *time.Time) {
		var syncedAt any
		if synced != nil {
			syncedAt = db.Timestamp{Time: *synced}
		}
		_, err := db.Exec("INSERT INTO "+TableName+" (indexer_id, sid, queued_at, synced_at, status, queries) VALUES (?, ?, ?, ?, ?, ?)",
			1, sid, db.Timestamp{Time: now.Add(-30 * 24 * time.Hour)}, syncedAt, string(status), "[]")
		require.NoError(t, err)
	}
	old, recent := now.Add(-8*24*time.Hour), now.Add(-2*24*time.Hour)
	insert("tt0000001", StatusSynced, &old)
	insert("tt0000002", StatusSynced, &recent)
	insert("tt0000003", StatusQueued, nil)
	insert("tt0000004", StatusQueued, &old)

	deleted, err := DeleteSyncedBefore(now.Add(-syncedRetention))
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	rows, err := db.Query("SELECT sid FROM " + TableName + " ORDER BY sid")
	require.NoError(t, err)
	defer rows.Close()
	var left []string
	for rows.Next() {
		var sid string
		require.NoError(t, rows.Scan(&sid))
		left = append(left, sid)
	}
	require.Equal(t, []string{"tt0000002", "tt0000003", "tt0000004"}, left)
}
