-- name: GetItemByID :one
SELECT * FROM items WHERE id = @id;

-- name: ListOpenItemIDs :many
SELECT id FROM items WHERE user_id = @user_id AND status = 'open' ORDER BY id;

-- name: DeleteItemSchedule :exec
DELETE FROM scheduled_reminders WHERE item_id = @item_id;

-- name: DeleteFutureSchedule :exec
DELETE FROM scheduled_reminders WHERE item_id = @item_id AND fire_at > @now;

-- name: InsertScheduledReminder :exec
INSERT INTO scheduled_reminders (id, item_id, user_id, kind, offset_seconds, manual_id, fire_at, scheduled_for)
VALUES (@id, @item_id, @user_id, @kind, @offset_seconds, @manual_id, @fire_at, @scheduled_for);

-- name: ListHandledReminders :many
SELECT kind, scheduled_for FROM reminder_log WHERE item_id = @item_id;
