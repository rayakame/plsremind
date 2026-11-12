-- name: CreateSubitem :one
INSERT INTO subitems (id, item_id, title, position, created_at, updated_at)
VALUES (@id, @item_id, @title, @position, @now, @now)
RETURNING *;

-- name: ListSubitems :many
SELECT * FROM subitems WHERE item_id = ANY(@item_ids::uuid[]) ORDER BY item_id, position, id;

-- name: GetSubitem :one
SELECT * FROM subitems WHERE id = @id AND item_id = @item_id;

-- name: UpdateSubitem :one
UPDATE subitems SET title = @title, done = @done, updated_at = @now WHERE id = @id
RETURNING *;

-- name: SetSubitemPosition :one
UPDATE subitems SET position = @position, updated_at = @now WHERE id = @id
RETURNING *;

-- name: DeleteSubitem :exec
DELETE FROM subitems WHERE id = @id;
