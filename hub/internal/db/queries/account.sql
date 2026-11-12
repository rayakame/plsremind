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

-- name: AdminLock :exec
SELECT pg_advisory_xact_lock(2);

-- name: ListUsers :many
SELECT * FROM users
WHERE id > @after
ORDER BY id
LIMIT @page_size;

-- name: UpdateUserAccess :one
UPDATE users SET role = @role, disabled = @disabled WHERE id = @id
RETURNING *;

-- name: CountActiveAdmins :one
SELECT count(*) FROM users WHERE role = 'admin' AND NOT disabled;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = @id;

-- name: ListIdentities :many
SELECT * FROM identities WHERE user_id = @user_id ORDER BY created_at, id;

-- name: GetPasswordIdentity :one
SELECT * FROM identities WHERE user_id = @user_id AND provider = 'password';

-- name: UpdatePasswordHash :exec
UPDATE identities SET password_hash = @password_hash WHERE id = @id;

-- name: CreateInvite :one
INSERT INTO invites (id, code_hash, created_by, created_at, expires_at)
VALUES (@id, @code_hash, @created_by, @created_at, @expires_at)
RETURNING *;

-- name: ListInvites :many
SELECT * FROM invites ORDER BY id DESC;

-- name: DeleteInvite :execrows
DELETE FROM invites WHERE id = @id;

-- name: GetUsableInvite :one
SELECT * FROM invites
WHERE code_hash = @code_hash AND used_at IS NULL AND expires_at > @now
FOR UPDATE;

-- name: UseInvite :exec
UPDATE invites SET used_at = @now, used_by = @used_by WHERE id = @id;

-- name: LockUser :one
SELECT id FROM users WHERE id = @id FOR NO KEY UPDATE;
