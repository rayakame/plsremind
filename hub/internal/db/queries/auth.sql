-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, token_hash, created_at, last_used_at, expires_at)
VALUES (@id, @user_id, @token_hash, @now, @now, @expires_at);

-- name: GetSessionUser :one
SELECT s.id AS session_id, s.last_used_at, u.id AS user_id, u.role, u.disabled
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = @token_hash AND s.expires_at > @now;

-- name: TouchSession :exec
UPDATE sessions SET last_used_at = @now, expires_at = @expires_at WHERE id = @id;

-- name: DeleteSessionByHash :exec
DELETE FROM sessions WHERE token_hash = @token_hash;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = @user_id;

-- name: ListLoginMethods :many
SELECT key, type, name FROM login_providers WHERE login_enabled ORDER BY type = 'password' DESC, key;

-- name: GetLoginProvider :one
SELECT * FROM login_providers WHERE key = @key;
