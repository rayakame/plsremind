-- name: Notify :exec
SELECT pg_notify(@channel::text, @payload::text);

-- name: DeleteOldEvents :exec
DELETE FROM events WHERE created_at <= @before;
