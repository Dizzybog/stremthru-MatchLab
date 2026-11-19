package worker

import (
	"database/sql"
	"testing"
	"time"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/MunifTanjim/stremthru/internal/torrent_info"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func TestMatchLabResetRunsOutsideRequestAndPersistsStatus(t *testing.T) {
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(sqlDB, "../../migrations/sqlite"))
	db.SetForTesting(sqlDB, t.Cleanup)

	previous := matchLabResetExecute
	unblock := make(chan struct{})
	t.Cleanup(func() { matchLabResetExecute = previous })
	matchLabResetExecute = func(includeParsing bool) (torrent_info.MatchLabResetStats, error) {
		<-unblock
		if includeParsing {
			return torrent_info.MatchLabResetStats{Torrents: 17, StreamFiles: 42}, nil
		}
		return torrent_info.MatchLabResetStats{}, nil
	}

	start, err := StartMatchLabReset(true)
	require.NoError(t, err)
	require.Equal(t, "started", start.Status)
	require.True(t, start.IncludeParsing)
	require.NotEmpty(t, start.ID)

	// A long operation cannot hold the dashboard POST open.
	status, err := GetMatchLabResetStatus()
	require.NoError(t, err)
	require.Equal(t, "started", status.Status)
	require.Equal(t, start.ID, status.ID)

	_, err = StartMatchLabReset(false)
	require.ErrorIs(t, err, ErrMatchLabResetInProgress)

	close(unblock)
	require.Eventually(t, func() bool {
		s, err := GetMatchLabResetStatus()
		return err == nil && s.Status == "done" &&
			s.Torrents == 17 && s.StreamFiles == 42 && s.IncludeParsing
	}, 5*time.Second, 20*time.Millisecond)

	require.Eventually(t, func() bool {
		matchLabResetState.Lock()
		defer matchLabResetState.Unlock()
		return !matchLabResetState.active
	}, 5*time.Second, 20*time.Millisecond)
}
