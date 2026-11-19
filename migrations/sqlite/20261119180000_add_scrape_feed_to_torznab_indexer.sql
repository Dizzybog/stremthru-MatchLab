-- +goose Up
-- +goose StatementBegin
ALTER TABLE `torznab_indexer`
  ADD COLUMN `scrape_feed` bool NOT NULL DEFAULT false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE `torznab_indexer`
  DROP COLUMN `scrape_feed`;
-- +goose StatementEnd
