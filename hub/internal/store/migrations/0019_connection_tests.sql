-- +goose Up
CREATE TABLE connection_tests (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    connection_id uuid NOT NULL REFERENCES connections ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'delivered', 'failed', 'unanswered')),
    reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    sent_at timestamptz,
    closed_at timestamptz
);
CREATE INDEX connection_tests_user_idx ON connection_tests (user_id, connection_id, created_at DESC);
CREATE INDEX connection_tests_pending_idx ON connection_tests (created_at) WHERE status = 'pending';

-- +goose Down
DROP TABLE connection_tests;
