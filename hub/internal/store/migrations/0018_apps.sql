-- +goose Up
ALTER TABLE connections ADD COLUMN install_url text NOT NULL DEFAULT '';
CREATE INDEX deliveries_user_connection_idx ON deliveries (user_id, connection_id, created_at DESC);

-- +goose Down
DROP INDEX deliveries_user_connection_idx;
ALTER TABLE connections DROP COLUMN install_url;
