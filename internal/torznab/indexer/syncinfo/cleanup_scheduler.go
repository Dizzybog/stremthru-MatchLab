package torznab_indexer_syncinfo

import (
	"fmt"
	"time"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/MunifTanjim/stremthru/internal/job"
)

const (
	cleanupSchedulerId = "clean-torznab-indexer-syncinfo"
	// A finished search is only consulted for staleTime (24h) to avoid
	// searching the same title again. Keep a week for the dashboard history.
	syncedRetention = 7 * 24 * time.Hour
)

var query_delete_synced_before = fmt.Sprintf(
	"DELETE FROM %s WHERE %s = '%s' AND %s IS NOT NULL AND %s < ?",
	TableName, Column.Status, StatusSynced, Column.SyncedAt, Column.SyncedAt,
)

// DeleteSyncedBefore removes finished searches that synced before cutoff.
// A deleted title is searched again the next time it is requested or seen
// in a feed, exactly like a title that was never searched.
func DeleteSyncedBefore(cutoff time.Time) (int64, error) {
	result, err := db.Exec(query_delete_synced_before, db.Timestamp{Time: cutoff})
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

var _ = job.NewScheduler(&job.SchedulerConfig[struct{}]{
	Id:                cleanupSchedulerId,
	Title:             "Clean Torznab Indexer Sync Info",
	Interval:          24 * time.Hour,
	RunAtStartupAfter: 5 * time.Minute,
	RunExclusive:      true,
	Disabled:          queue.IsDisabled(),
	Executor: func(j *job.Scheduler[struct{}]) error {
		db.WaitForMaintenance()
		deleted, err := DeleteSyncedBefore(time.Now().Add(-syncedRetention))
		if err != nil {
			j.Logger().Error("failed to delete finished torznab searches", "error", err)
			return err
		}
		j.Logger().Info("deleted finished torznab searches", "count", deleted,
			"older_than", syncedRetention.String())
		return nil
	},
})
