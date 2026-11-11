-- +goose Up
CREATE TABLE invites (
    id uuid PRIMARY KEY,
    code_hash bytea NOT NULL UNIQUE,
    created_by uuid REFERENCES users ON DELETE SET NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    used_by uuid REFERENCES users ON DELETE SET NULL
);

-- +goose Down
DROP TABLE invites;
