-- name: CreateItem :one
INSERT INTO items (id, user_id, list_id, title, note, priority, due_at, due_date, status, completed_at, reminder_override, created_at, updated_at)
VALUES (@id, @user_id, @list_id, @title, @note, @priority, @due_at, @due_date, @status, @completed_at, @reminder_override, @now, @now)
RETURNING *;

-- name: GetItem :one
SELECT * FROM items WHERE id = @id AND user_id = @user_id;

-- name: UpdateItem :one
UPDATE items
SET list_id = @list_id, title = @title, note = @note, priority = @priority, due_at = @due_at, due_date = @due_date,
    status = @status, completed_at = @completed_at, reminder_override = @reminder_override, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: DeleteItem :exec
DELETE FROM items WHERE id = @id;

-- name: TouchItem :exec
UPDATE items SET updated_at = @now WHERE id = @id;

-- name: MoveItemsToList :many
UPDATE items SET list_id = @target_list_id, updated_at = @now WHERE list_id = @list_id
RETURNING *;

-- name: ListItemsByCreated :many
SELECT i.*
FROM items i
JOIN users u ON u.id = i.user_id
WHERE i.user_id = @user_id
  AND (sqlc.narg('list_id')::uuid IS NULL OR i.list_id = sqlc.narg('list_id'))
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('has_due')::boolean IS NULL OR (item_due(i.due_at, i.due_date, u.timezone) IS NOT NULL) = sqlc.narg('has_due'))
  AND (sqlc.narg('due_before')::timestamptz IS NULL OR item_due(i.due_at, i.due_date, u.timezone) < sqlc.narg('due_before'))
  AND (sqlc.narg('due_after')::timestamptz IS NULL OR item_due(i.due_at, i.due_date, u.timezone) > sqlc.narg('due_after'))
  AND (sqlc.narg('cursor_id')::uuid IS NULL OR CASE
        WHEN @descending::boolean THEN i.id < sqlc.narg('cursor_id')
        ELSE i.id > sqlc.narg('cursor_id')
      END)
ORDER BY
  CASE WHEN NOT @descending::boolean THEN i.id END ASC,
  CASE WHEN @descending::boolean THEN i.id END DESC
LIMIT @page_size;

-- name: ListItemsByDue :many
SELECT i.*
FROM items i
JOIN users u ON u.id = i.user_id
WHERE i.user_id = @user_id
  AND (sqlc.narg('list_id')::uuid IS NULL OR i.list_id = sqlc.narg('list_id'))
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('has_due')::boolean IS NULL OR (item_due(i.due_at, i.due_date, u.timezone) IS NOT NULL) = sqlc.narg('has_due'))
  AND (sqlc.narg('due_before')::timestamptz IS NULL OR item_due(i.due_at, i.due_date, u.timezone) < sqlc.narg('due_before'))
  AND (sqlc.narg('due_after')::timestamptz IS NULL OR item_due(i.due_at, i.due_date, u.timezone) > sqlc.narg('due_after'))
  AND (sqlc.narg('cursor_id')::uuid IS NULL OR CASE
        WHEN sqlc.narg('cursor_due')::timestamptz IS NULL THEN
          item_due(i.due_at, i.due_date, u.timezone) IS NULL AND CASE
            WHEN @descending::boolean THEN i.id < sqlc.narg('cursor_id')
            ELSE i.id > sqlc.narg('cursor_id')
          END
        ELSE
          item_due(i.due_at, i.due_date, u.timezone) IS NULL OR CASE
            WHEN @descending::boolean THEN (item_due(i.due_at, i.due_date, u.timezone), i.id) < (sqlc.narg('cursor_due'), sqlc.narg('cursor_id'))
            ELSE (item_due(i.due_at, i.due_date, u.timezone), i.id) > (sqlc.narg('cursor_due'), sqlc.narg('cursor_id'))
          END
      END)
ORDER BY
  (item_due(i.due_at, i.due_date, u.timezone) IS NULL),
  CASE WHEN NOT @descending::boolean THEN item_due(i.due_at, i.due_date, u.timezone) END ASC,
  CASE WHEN @descending::boolean THEN item_due(i.due_at, i.due_date, u.timezone) END DESC,
  CASE WHEN NOT @descending::boolean THEN i.id END ASC,
  CASE WHEN @descending::boolean THEN i.id END DESC
LIMIT @page_size;

-- name: ListItemsByPriority :many
SELECT i.*
FROM items i
JOIN users u ON u.id = i.user_id
WHERE i.user_id = @user_id
  AND (sqlc.narg('list_id')::uuid IS NULL OR i.list_id = sqlc.narg('list_id'))
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('has_due')::boolean IS NULL OR (item_due(i.due_at, i.due_date, u.timezone) IS NOT NULL) = sqlc.narg('has_due'))
  AND (sqlc.narg('due_before')::timestamptz IS NULL OR item_due(i.due_at, i.due_date, u.timezone) < sqlc.narg('due_before'))
  AND (sqlc.narg('due_after')::timestamptz IS NULL OR item_due(i.due_at, i.due_date, u.timezone) > sqlc.narg('due_after'))
  AND (sqlc.narg('cursor_id')::uuid IS NULL OR CASE
        WHEN @descending::boolean THEN (array_position(ARRAY['low', 'normal', 'high'], i.priority), i.id) < (sqlc.narg('cursor_rank')::integer, sqlc.narg('cursor_id'))
        ELSE (array_position(ARRAY['low', 'normal', 'high'], i.priority), i.id) > (sqlc.narg('cursor_rank')::integer, sqlc.narg('cursor_id'))
      END)
ORDER BY
  CASE WHEN NOT @descending::boolean THEN array_position(ARRAY['low', 'normal', 'high'], i.priority) END ASC,
  CASE WHEN @descending::boolean THEN array_position(ARRAY['low', 'normal', 'high'], i.priority) END DESC,
  CASE WHEN NOT @descending::boolean THEN i.id END ASC,
  CASE WHEN @descending::boolean THEN i.id END DESC
LIMIT @page_size;

-- name: LockItem :one
SELECT * FROM items WHERE id = @id AND user_id = @user_id FOR UPDATE;

-- name: ListAllDayDue :many
SELECT i.id, item_due(i.due_at, i.due_date, u.timezone)::timestamptz AS due
FROM items i
JOIN users u ON u.id = i.user_id
WHERE i.id = ANY(@item_ids::uuid[]) AND i.due_date IS NOT NULL;

-- name: ListOpenAllDayItems :many
SELECT * FROM items WHERE user_id = @user_id AND status = 'open' AND due_date IS NOT NULL ORDER BY id;
