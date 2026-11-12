-- name: Notify :exec
SELECT pg_notify(@channel::text, @payload::text);

-- name: DeleteOldEvents :exec
DELETE FROM events WHERE created_at <= @before;

-- name: GetEventSeq :one
SELECT event_seq FROM users WHERE id = @id;

-- name: ListEventsAfter :many
SELECT * FROM events WHERE user_id = @user_id AND seq > @after ORDER BY seq LIMIT @batch;
