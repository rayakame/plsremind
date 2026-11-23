-- name: CreateConnection :one
INSERT INTO connections (id, name, description, provider, allowed_intents, install_url, token_hash, created_at)
VALUES (@id, @name, @description, @provider, @allowed_intents, @install_url, @token_hash, @created_at)
RETURNING *;

-- name: ListConnections :many
SELECT * FROM connections ORDER BY id;

-- name: GetConnection :one
SELECT * FROM connections WHERE id = @id;

-- name: UpdateConnection :one
UPDATE connections
SET name = @name, description = @description, enabled = @enabled, allowed_intents = @allowed_intents,
    install_url = @install_url
WHERE id = @id
RETURNING *;

-- name: SetConnectionToken :one
UPDATE connections SET token_hash = @token_hash WHERE id = @id
RETURNING *;

-- name: DeleteConnection :execrows
DELETE FROM connections WHERE id = @id;

-- name: GetConnectionByToken :one
SELECT * FROM connections WHERE token_hash = @token_hash AND enabled;

-- name: TouchConnection :exec
UPDATE connections SET last_used_at = @now WHERE id = @id;

-- name: GetGrantedUser :one
SELECT u.*
FROM users u
JOIN connections c ON c.id = @connection_id
WHERE u.id = @user_id
  AND NOT u.disabled
  AND COALESCE(
        (SELECT g.enabled FROM connection_grants g WHERE g.user_id = u.id AND g.connection_id = c.id),
        EXISTS (SELECT 1 FROM identities i WHERE i.user_id = u.id AND i.provider = c.provider));

-- name: GetGrantedUserByIdentity :one
SELECT u.*
FROM users u
JOIN connections c ON c.id = @connection_id
JOIN identities i ON i.user_id = u.id AND i.provider = c.provider
WHERE i.external_id = @external_id
  AND NOT u.disabled
  AND COALESCE(
        (SELECT g.enabled FROM connection_grants g WHERE g.user_id = u.id AND g.connection_id = c.id),
        true);

-- name: ListUserConnections :many
SELECT c.id, c.name, c.description, c.provider, c.allowed_intents, c.install_url,
       EXISTS (SELECT 1 FROM identities i WHERE i.user_id = @user_id AND i.provider = c.provider) AS linked,
       g.enabled AS granted, g.remind AS remind_choice
FROM connections c
LEFT JOIN connection_grants g ON g.connection_id = c.id AND g.user_id = @user_id
WHERE c.enabled
ORDER BY c.id;

-- name: SetGrantEnabled :exec
INSERT INTO connection_grants (user_id, connection_id, enabled)
VALUES (@user_id, @connection_id, @enabled)
ON CONFLICT (user_id, connection_id) DO UPDATE SET enabled = EXCLUDED.enabled;

-- name: SetGrantRemind :exec
INSERT INTO connection_grants (user_id, connection_id, remind)
VALUES (@user_id, @connection_id, @remind)
ON CONFLICT (user_id, connection_id) DO UPDATE SET remind = EXCLUDED.remind;

-- name: ConnectionReach :many
-- Whom each connection can remind now: users it reminds who have an account it can write to.
SELECT c.id, count(u.id)::integer AS reach
FROM connections c
LEFT JOIN users u ON c.enabled
    AND NOT u.disabled
    AND 'deliveries' = ANY(c.allowed_intents)
    AND EXISTS (SELECT 1 FROM identities i WHERE i.user_id = u.id AND i.provider = c.provider)
    AND COALESCE((SELECT g.remind FROM connection_grants g WHERE g.user_id = u.id AND g.connection_id = c.id), true)
    AND COALESCE((SELECT g.enabled FROM connection_grants g WHERE g.user_id = u.id AND g.connection_id = c.id), true)
WHERE sqlc.narg(connection_id)::uuid IS NULL OR c.id = sqlc.narg(connection_id)::uuid
GROUP BY c.id;

-- name: LastDeliveries :many
SELECT DISTINCT ON (d.connection_id)
       d.connection_id::uuid AS connection_id, d.status, d.reason,
       COALESCE(d.closed_at, d.created_at)::timestamptz AS at
FROM deliveries d
WHERE d.user_id = @user_id
  AND d.connection_id IS NOT NULL
  AND d.status IN ('pending', 'delivered', 'failed', 'expired')
ORDER BY d.connection_id, d.created_at DESC, d.id DESC;
