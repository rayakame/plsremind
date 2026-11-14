-- +goose Up
-- locale: the language the user wants to be addressed in, NULL while they have not chosen.
-- preferences: settings of the user's own clients, which the hub stores without reading them.
-- json rather than jsonb keeps the text as it was sent: jsonb writes a number like 1e100000
-- out digit by digit, which would let a small request grow into a huge value.
ALTER TABLE users
    ADD COLUMN locale text,
    ADD COLUMN preferences json NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE users
    DROP COLUMN locale,
    DROP COLUMN preferences;
