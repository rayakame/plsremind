-- +goose Up
CREATE TABLE oauth_states (
    state_hash bytea PRIMARY KEY,
    provider text NOT NULL,
    intent text NOT NULL CHECK (intent IN ('login', 'link')),
    user_id uuid REFERENCES users ON DELETE CASCADE,
    invite_code text NOT NULL DEFAULT '',
    verifier text NOT NULL,
    expires_at timestamptz NOT NULL
);

INSERT INTO login_providers (key, type, name, login_enabled, link_enabled)
VALUES ('github', 'github', 'GitHub', false, false),
       ('discord', 'discord', 'Discord', false, false);

-- +goose Down
DELETE FROM login_providers WHERE key IN ('github', 'discord');
DROP TABLE oauth_states;
