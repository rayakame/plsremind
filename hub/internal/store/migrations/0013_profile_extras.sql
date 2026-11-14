-- +goose Up
-- locale: the language the user wants to be addressed in, NULL while they have not chosen.
-- preferences: settings of the user's own clients, which the hub stores without reading them.
ALTER TABLE users
    ADD COLUMN locale text,
    ADD COLUMN preferences jsonb NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE users
    DROP COLUMN locale,
    DROP COLUMN preferences;
