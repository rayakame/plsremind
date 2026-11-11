-- name: CreateManualReminder :one
INSERT INTO manual_reminders (id, item_id, remind_at, created_at)
VALUES (@id, @item_id, @remind_at, @created_at)
RETURNING *;

-- name: ListManualReminders :many
SELECT * FROM manual_reminders WHERE item_id = @item_id ORDER BY remind_at, id;

-- name: DeleteManualReminder :execrows
DELETE FROM manual_reminders WHERE id = @id AND item_id = @item_id;
