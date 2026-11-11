-- +goose Up
CREATE TABLE connections (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    provider text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    token_hash bytea NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    last_used_at timestamptz
);

-- Holds only explicit choices. Without a row, a connection may act for a user
-- exactly when the user has an identity at the connection's provider.
CREATE TABLE connection_grants (
    user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    connection_id uuid NOT NULL REFERENCES connections ON DELETE CASCADE,
    enabled boolean NOT NULL,
    PRIMARY KEY (user_id, connection_id)
);

-- +goose Down
DROP TABLE connection_grants;
DROP TABLE connections;
