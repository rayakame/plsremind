-- +goose Up
CREATE TABLE reminder_plans (
    user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    priority text NOT NULL CHECK (priority IN ('low', 'normal', 'high')),
    before_seconds integer[] NOT NULL,
    follow_up text NOT NULL CHECK (follow_up IN ('never', 'daily', 'twice_daily')),
    PRIMARY KEY (user_id, priority)
);

-- +goose Down
DROP TABLE reminder_plans;
