-- +goose Up
CREATE TABLE subitems (
    id uuid PRIMARY KEY,
    item_id uuid NOT NULL REFERENCES items ON DELETE CASCADE,
    title text NOT NULL,
    done boolean NOT NULL DEFAULT false,
    position integer NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX subitems_item_id_idx ON subitems (item_id);

CREATE TABLE manual_reminders (
    id uuid PRIMARY KEY,
    item_id uuid NOT NULL REFERENCES items ON DELETE CASCADE,
    remind_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL
);
CREATE INDEX manual_reminders_item_id_idx ON manual_reminders (item_id);

-- +goose Down
DROP TABLE manual_reminders;
DROP TABLE subitems;
