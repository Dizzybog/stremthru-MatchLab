package torrent_info

import (
	"database/sql"
	"os"
	"testing"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/stretchr/testify/require"
)

// The real PostgreSQL DDL path is exercised in CI against an isolated database.
// Local SQLite-only test runs skip it when the connection is not configured.
func TestPostgresFastMatchLabResetIntegration(t *testing.T) {
	dsn := os.Getenv("STREMTHRU_TEST_POSTGRES_RESET_DSN")
	if dsn == "" {
		t.Skip("set STREMTHRU_TEST_POSTGRES_RESET_DSN to run the PostgreSQL reset integration test")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(5)
	require.NoError(t, sqlDB.Ping())

	originalDialect := db.Dialect
	originalTimestamp := db.CurrentTimestamp
	t.Cleanup(func() {
		db.Dialect = originalDialect
		db.CurrentTimestamp = originalTimestamp
	})
	db.Dialect = db.DBDialectPostgres
	db.CurrentTimestamp = "current_timestamp"
	db.SetForTesting(sqlDB, t.Cleanup)

	// Dedicated test database only. No application/user data may exist here.
	// pgx extended query protocol needs one statement per Exec call.
	for _, statement := range []string{
		`CREATE TABLE public.torrent_info (
			hash text PRIMARY KEY,
			t_title text NOT NULL,
			parser_version integer NOT NULL DEFAULT 0,
			parser_input text NOT NULL DEFAULT '',
			parsed_at timestamptz,
			matchlab_mapped_at timestamptz,
			matchlab_retry_at timestamptz,
			matchlab_parse_retry_at timestamptz,
			matchlab_file_revision bigint NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX idx_test_info_pending ON public.torrent_info (hash)
			WHERE matchlab_mapped_at IS NULL`,
		`CREATE INDEX idx_test_info_parser ON public.torrent_info (parser_version)`,
		`CREATE INDEX idx_test_info_parser_hash ON public.torrent_info (parser_version, hash)`,
		`CREATE TABLE public.torrent_stream (
			h text NOT NULL,
			p text NOT NULL,
			src text NOT NULL DEFAULT '',
			sid character varying NOT NULL DEFAULT '',
			asid text NOT NULL DEFAULT '',
			PRIMARY KEY (h, p)
		)`,
		`CREATE INDEX idx_test_sid ON public.torrent_stream (sid text_pattern_ops)`,
		`CREATE INDEX idx_test_asid ON public.torrent_stream (asid text_pattern_ops)`,
		`CREATE INDEX idx_test_src_sid ON public.torrent_stream (src, sid)`,
		`CREATE INDEX idx_test_sid_expression ON public.torrent_stream (lower(sid))`,
		`CREATE INDEX idx_test_sid_predicate ON public.torrent_stream (h) WHERE sid <> '*'`,
		`CREATE TABLE public.imdb_torrent (hash text NOT NULL, tid text NOT NULL)`,
		`CREATE TABLE public.anidb_torrent (hash text NOT NULL, tid text NOT NULL)`,
		`INSERT INTO public.torrent_info
			(hash, t_title, parser_version, parser_input, parsed_at,
			 matchlab_mapped_at, matchlab_retry_at, matchlab_parse_retry_at, matchlab_file_revision)
		VALUES ('hash-a', 'Example S01E01', 10000, 'Example S01E01', now(), now(), now(), now(), 14),
		       ('hash-b', 'Example S01E02', 10000, 'Example S01E02', now(), now(), now(), now(), 8)`,
		`INSERT INTO public.torrent_stream(h, p, src, sid, asid)
		VALUES ('hash-a', '/S01E01.mkv', 'dht', 'tt1234567:1:1', '42:1'),
		       ('hash-b', '/S01E02.mkv', 'dht', 'tt1234567:1:2', '42:2')`,
		`INSERT INTO public.imdb_torrent VALUES ('hash-a', 'tt1234567')`,
		`INSERT INTO public.anidb_torrent VALUES ('hash-b', '42')`,
	} {
		_, err = sqlDB.Exec(statement)
		require.NoError(t, err, "SQL: %s", statement)
	}

	// An external view is an unsupported dependency. DROP COLUMN RESTRICT
	// must fail atomically without leaving any column/index half-reset.
	_, err = sqlDB.Exec("CREATE VIEW public.test_reset_view AS SELECT sid FROM public.torrent_stream")
	require.NoError(t, err)
	_, err = ResetAllMatchLabMappingsWithStats(false)
	require.Error(t, err)
	var originalSID string
	require.NoError(t, sqlDB.QueryRow("SELECT sid FROM torrent_stream WHERE h = 'hash-a'").Scan(&originalSID))
	require.Equal(t, "tt1234567:1:1", originalSID)
	_, err = sqlDB.Exec("DROP VIEW public.test_reset_view")
	require.NoError(t, err)

	stats, err := ResetAllMatchLabMappingsWithStats(false)
	require.NoError(t, err)
	require.True(t, stats.FastReset)

	for _, hash := range []string{"hash-a", "hash-b"} {
		var sid, asid, path, src string
		require.NoError(t, sqlDB.QueryRow(
			"SELECT sid, asid, p, src FROM torrent_stream WHERE h = $1", hash,
		).Scan(&sid, &asid, &path, &src))
		require.Equal(t, "*", sid)
		require.Empty(t, asid)
		require.NotEmpty(t, path)
		require.Equal(t, "dht", src)

		var version int
		var revision int64
		var parsed, mapped, retry sql.NullTime
		var parserInput string
		require.NoError(t, sqlDB.QueryRow(`
			SELECT parser_version, parser_input, parsed_at, matchlab_mapped_at,
			       matchlab_retry_at, matchlab_file_revision FROM torrent_info WHERE hash = $1`, hash,
		).Scan(&version, &parserInput, &parsed, &mapped, &retry, &revision))
		require.Equal(t, 10000, version)
		require.NotEmpty(t, parserInput)
		require.True(t, parsed.Valid)
		require.False(t, mapped.Valid)
		require.False(t, retry.Valid)
		require.Equal(t, int64(15), revision)
	}

	for _, table := range []string{"imdb_torrent", "anidb_torrent"} {
		var count int
		require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count))
		require.Zero(t, count)
	}
	for _, index := range []string{
		"idx_test_sid", "idx_test_asid", "idx_test_src_sid",
		"idx_test_sid_expression", "idx_test_sid_predicate",
		"idx_test_info_pending", "idx_test_info_parser", "idx_test_info_parser_hash",
	} {
		var exists bool
		require.NoError(t, sqlDB.QueryRow(
			"SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname='public' AND indexname=$1)", index,
		).Scan(&exists))
		require.True(t, exists, "missing restored index %s", index)
	}
	// Rebuilt columns preserve original defaults for newly inserted torrents.
	_, err = sqlDB.Exec("INSERT INTO torrent_stream(h,p) VALUES ('hash-new', '/new.mkv')")
	require.NoError(t, err)
	var newSID, newASID string
	require.NoError(t, sqlDB.QueryRow("SELECT sid, asid FROM torrent_stream WHERE h='hash-new'").Scan(&newSID, &newASID))
	require.Empty(t, newSID)
	require.Empty(t, newASID)

	stats, err = ResetAllMatchLabMappingsWithStats(true)
	require.NoError(t, err)
	require.True(t, stats.FastReset)
	var version int
	var parserInput string
	var parsed sql.NullTime
	var revision int64
	require.NoError(t, sqlDB.QueryRow(`
		SELECT parser_version, parser_input, parsed_at, matchlab_file_revision
		FROM torrent_info WHERE hash='hash-a'`).Scan(&version, &parserInput, &parsed, &revision))
	require.Zero(t, version)
	require.Empty(t, parserInput)
	require.False(t, parsed.Valid)
	require.Equal(t, int64(16), revision)

	// This is a dedicated ephemeral CI database; clean up to support reruns.
	_, err = sqlDB.Exec(`DROP TABLE public.anidb_torrent, public.imdb_torrent,
		public.torrent_stream, public.torrent_info CASCADE`)
	require.NoError(t, err)
}
