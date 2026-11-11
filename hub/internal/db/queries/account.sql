-- name: SetupLock :exec
SELECT pg_advisory_xact_lock(1);

-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: CreateUser :one
INSERT INTO users (id, display_name, role, timezone, created_at)
VALUES (@id, @display_name, @role, @timezone, @created_at)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = @id;

-- name: UpdateUserProfile :one
UPDATE users
SET display_name = @display_name, timezone = @timezone, quiet_start = @quiet_start, quiet_end = @quiet_end
WHERE id = @id
RETURNING *;

-- name: CreateIdentity :one
INSERT INTO identities (id, user_id, provider, external_id, display_name, password_hash, provider_token, created_at)
VALUES (@id, @user_id, @provider, @external_id, @display_name, @password_hash, @provider_token, @created_at)
RETURNING *;

-- name: GetIdentity :one
SELECT * FROM identities WHERE provider = @provider AND external_id = @external_id;
