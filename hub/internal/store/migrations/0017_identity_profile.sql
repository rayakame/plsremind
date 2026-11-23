-- +goose Up
ALTER TABLE identities
    ADD COLUMN username text NOT NULL DEFAULT '',
    ADD COLUMN avatar bytea,
    ADD COLUMN avatar_type text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE identities
    DROP COLUMN avatar_type,
    DROP COLUMN avatar,
    DROP COLUMN username;
