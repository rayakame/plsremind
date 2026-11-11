-- name: NextEventSeq :one
UPDATE users SET event_seq = event_seq + 1 WHERE id = @id
RETURNING event_seq;

-- name: InsertEvent :exec
INSERT INTO events (user_id, seq, type, payload, actor_kind, actor_id, created_at)
VALUES (@user_id, @seq, @type, @payload, @actor_kind, @actor_id, @created_at);
