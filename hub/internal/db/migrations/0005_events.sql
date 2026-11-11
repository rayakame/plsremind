-- +goose Up
CREATE TABLE events (
    user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    seq bigint NOT NULL,
    type text NOT NULL,
    payload jsonb NOT NULL,
    actor_kind text NOT NULL CHECK (actor_kind IN ('session', 'personal_token', 'connection')),
    actor_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, seq)
);

-- +goose Down
DROP TABLE events;
