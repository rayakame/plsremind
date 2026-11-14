-- +goose Up
-- What the client sent along with the request that caused the event, so it can tell which of its calls this was.
ALTER TABLE events ADD COLUMN nonce text;

-- +goose Down
ALTER TABLE events DROP COLUMN nonce;
