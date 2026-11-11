-- +goose Up
CREATE TABLE users (
    id uuid PRIMARY KEY,
    display_name text NOT NULL,
    role text NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    timezone text NOT NULL DEFAULT 'UTC',
    quiet_start time NOT NULL DEFAULT '22:00',
    quiet_end time NOT NULL DEFAULT '08:00',
    disabled boolean NOT NULL DEFAULT false,
    event_seq bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL
);

CREATE TABLE identities (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    provider text NOT NULL,
    external_id text NOT NULL,
    display_name text NOT NULL,
    password_hash text,
    provider_token bytea,
    created_at timestamptz NOT NULL,
    UNIQUE (provider, external_id)
);
CREATE INDEX identities_user_id_idx ON identities (user_id);

CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    last_used_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);

CREATE TABLE login_providers (
    key text PRIMARY KEY,
    type text NOT NULL CHECK (type IN ('password', 'github', 'discord', 'oidc')),
    name text NOT NULL,
    login_enabled boolean NOT NULL,
    link_enabled boolean NOT NULL,
    client_id text NOT NULL DEFAULT '',
    client_secret bytea,
    issuer_url text NOT NULL DEFAULT ''
);

INSERT INTO login_providers (key, type, name, login_enabled, link_enabled)
VALUES ('password', 'password', 'Password', true, true);

CREATE TABLE lists (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    name text NOT NULL,
    position integer NOT NULL,
    is_inbox boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX lists_user_id_idx ON lists (user_id);
CREATE UNIQUE INDEX lists_one_inbox_idx ON lists (user_id) WHERE is_inbox;

-- +goose Down
DROP TABLE lists;
DROP TABLE login_providers;
DROP TABLE sessions;
DROP TABLE identities;
DROP TABLE users;
