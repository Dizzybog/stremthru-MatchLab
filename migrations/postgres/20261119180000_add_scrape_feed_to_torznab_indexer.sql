-- +goose Up
-- +goose StatementBegin
ALTER TABLE "public"."torznab_indexer"
  ADD COLUMN "scrape_feed" boolean NOT NULL DEFAULT false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE "public"."torznab_indexer"
  DROP COLUMN IF EXISTS "scrape_feed";
-- +goose StatementEnd
