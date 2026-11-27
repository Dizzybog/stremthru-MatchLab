package znabsearch

import (
	"database/sql"
	"testing"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/MunifTanjim/stremthru/internal/logger"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

// A title-level AniDB id (no episode, as the feed scraper queues) gets the
// work's titles, so the whole work is searched by title.
func TestTitleLevelAniDBIdGetsTitles(t *testing.T) {
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(sqlDB, "../../../migrations/sqlite"))
	db.SetForTesting(sqlDB, t.Cleanup)
	_, err = db.Exec(`INSERT INTO anidb_title (tid, ttype, tlang, value, season, year) VALUES
		('99901', 'main', 'x-jat', 'Sousou no Frieren', '1', '2023'),
		('99901', 'official', 'en', 'Frieren: Beyond Journey''s End', '1', '2023')`)
	require.NoError(t, err)

	meta, nsid, err := GetQueryMeta(logger.Scoped("test"), "anidb:99901")
	require.NoError(t, err)
	require.True(t, nsid.IsAnime)
	require.True(t, meta.IsSeries)
	require.Len(t, meta.Titles, 2)
	require.Zero(t, meta.Season)
	require.Zero(t, meta.Ep)
}
