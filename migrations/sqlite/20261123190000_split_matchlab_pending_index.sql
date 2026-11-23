-- +goose Up
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_torrent_info_matchlab_pending;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_torrent_info_matchlab_pending_fresh ON torrent_info (parser_version, hash)
  WHERE matchlab_mapped_at IS NULL AND matchlab_retry_at IS NULL AND t_title = parser_input;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_torrent_info_matchlab_pending_retry ON torrent_info (parser_version, matchlab_retry_at)
  WHERE matchlab_mapped_at IS NULL AND matchlab_retry_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_torrent_info_matchlab_pending_retry;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_torrent_info_matchlab_pending_fresh;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_torrent_info_matchlab_pending ON torrent_info (hash)
  WHERE matchlab_mapped_at IS NULL;
-- +goose StatementEnd
