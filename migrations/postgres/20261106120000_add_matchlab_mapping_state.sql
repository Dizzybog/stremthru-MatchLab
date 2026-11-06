-- +goose Up
ALTER TABLE torrent_info ADD COLUMN matchlab_mapped_at timestamptz;
CREATE INDEX idx_torrent_info_matchlab_parser_version ON torrent_info (parser_version, hash);
CREATE INDEX idx_torrent_info_matchlab_pending ON torrent_info (hash)
  WHERE matchlab_mapped_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_torrent_info_matchlab_pending;
DROP INDEX IF EXISTS idx_torrent_info_matchlab_parser_version;
ALTER TABLE torrent_info DROP COLUMN matchlab_mapped_at;
