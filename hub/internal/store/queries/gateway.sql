-- name: Notify :exec
SELECT pg_notify(@channel::text, @payload::text);

-- name: DeleteOldEvents :exec
DELETE FROM events WHERE created_at <= @before;

-- name: GetEventSeq :one
SELECT event_seq FROM users WHERE id = @id;

-- name: ListEventsAfter :many
SELECT * FROM events WHERE user_id = @user_id AND seq > @after ORDER BY seq LIMIT @batch;

-- name: ListGrantedUsers :many
SELECT u.id
FROM users u
JOIN connections c ON c.id = @connection_id
WHERE NOT u.disabled
  AND COALESCE(
        (SELECT g.enabled FROM connection_grants g WHERE g.user_id = u.id AND g.connection_id = c.id),
        EXISTS (SELECT 1 FROM identities i WHERE i.user_id = u.id AND i.provider = c.provider))
ORDER BY u.id;

-- name: GetGrantRemind :one
SELECT COALESCE(
    (SELECT g.remind FROM connection_grants g WHERE g.user_id = @user_id AND g.connection_id = @connection_id),
    true)::boolean;
