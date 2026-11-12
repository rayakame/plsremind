-- name: CreateList :one
INSERT INTO lists (id, user_id, name, position, is_inbox, created_at, updated_at)
VALUES (@id, @user_id, @name, @position, @is_inbox, @now, @now)
RETURNING *;

-- name: ListLists :many
SELECT * FROM lists WHERE user_id = @user_id ORDER BY position, id;

-- name: GetList :one
SELECT * FROM lists WHERE id = @id AND user_id = @user_id;

-- name: GetInbox :one
SELECT * FROM lists WHERE user_id = @user_id AND is_inbox;

-- name: RenameList :one
UPDATE lists SET name = @name, updated_at = @now WHERE id = @id
RETURNING *;

-- name: SetListPosition :one
UPDATE lists SET position = @position, updated_at = @now WHERE id = @id
RETURNING *;

-- name: DeleteList :exec
DELETE FROM lists WHERE id = @id;

-- name: LockList :one
SELECT * FROM lists WHERE id = @id AND user_id = @user_id FOR NO KEY UPDATE;

-- name: HoldList :one
SELECT * FROM lists WHERE id = @id AND user_id = @user_id FOR SHARE;
