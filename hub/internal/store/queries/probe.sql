-- name: ConnectionTestAccess :one
-- Whether a user may test a connection: it is on, may deliver, and acts for them.
SELECT (c.enabled AND 'deliveries' = ANY(c.allowed_intents))::boolean AS deliverable,
       EXISTS (SELECT 1 FROM identities i WHERE i.user_id = @user_id AND i.provider = c.provider) AS linked,
       COALESCE(
           (SELECT g.enabled FROM connection_grants g WHERE g.user_id = @user_id AND g.connection_id = c.id),
           true)::boolean AS granted
FROM connections c
WHERE c.id = @connection_id;

-- name: CreateConnectionTest :one
INSERT INTO connection_tests (id, user_id, connection_id, created_at)
VALUES (@id, @user_id, @connection_id, @created_at)
RETURNING *;

-- name: GetConnectionTest :one
SELECT * FROM connection_tests WHERE id = @id;

-- name: GetUserConnectionTest :one
SELECT * FROM connection_tests WHERE id = @id AND user_id = @user_id AND connection_id = @connection_id;

-- name: LockConnectionTest :one
SELECT * FROM connection_tests WHERE id = @id FOR UPDATE;

-- name: CloseConnectionTest :one
UPDATE connection_tests SET status = @status, reason = @reason, closed_at = @now
WHERE id = @id AND status = 'pending'
RETURNING *;

-- name: ExpireConnectionTests :execrows
UPDATE connection_tests SET status = 'unanswered', reason = 'no_answer', closed_at = @now
WHERE status = 'pending' AND created_at <= @answer_by;

-- name: DeleteOldConnectionTests :exec
DELETE FROM connection_tests WHERE created_at < @before;

-- name: LastConnectionTests :many
SELECT DISTINCT ON (t.connection_id)
       t.connection_id, t.status, t.reason, t.closed_at::timestamptz AS at
FROM connection_tests t
WHERE t.user_id = @user_id AND t.status IN ('delivered', 'failed')
ORDER BY t.connection_id, t.closed_at DESC, t.id DESC;
