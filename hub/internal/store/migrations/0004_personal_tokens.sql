-- +goose Up
CREATE TABLE personal_tokens (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    name text NOT NULL,
    token_hash bytea NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    last_used_at timestamptz
);
CREATE INDEX personal_tokens_user_id_idx ON personal_tokens (user_id);

-- +goose Down
DROP TABLE personal_tokens;
