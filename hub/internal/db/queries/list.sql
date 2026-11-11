-- name: CreateList :one
INSERT INTO lists (id, user_id, name, position, is_inbox, created_at, updated_at)
VALUES (@id, @user_id, @name, @position, @is_inbox, @now, @now)
RETURNING *;
