-- name: CreateManualReminder :one
INSERT INTO manual_reminders (id, item_id, remind_at, created_at)
VALUES (@id, @item_id, @remind_at, @created_at)
RETURNING *;

-- name: ListManualReminders :many
SELECT * FROM manual_reminders WHERE item_id = @item_id ORDER BY remind_at, id;

-- name: DeleteManualReminder :execrows
DELETE FROM manual_reminders WHERE id = @id AND item_id = @item_id;

-- name: ListManualRemindersForItems :many
SELECT * FROM manual_reminders WHERE item_id = ANY(@item_ids::uuid[]) ORDER BY item_id, remind_at, id;

-- name: ListSpentManualReminders :many
SELECT m.id, m.item_id, m.remind_at, i.user_id
FROM manual_reminders m JOIN items i ON i.id = m.item_id
WHERE m.remind_at <= @now
  AND (m.remind_at, m.id) > (@after_at::timestamptz, @after_id::uuid)
  AND NOT EXISTS (SELECT 1 FROM scheduled_reminders s WHERE s.manual_id = m.id)
ORDER BY m.remind_at, m.id
LIMIT @batch;

-- name: CountManualRemindersAhead :one
SELECT count(*) FROM manual_reminders m
WHERE m.item_id = @item_id
  AND (m.remind_at > @now OR EXISTS (SELECT 1 FROM scheduled_reminders s WHERE s.manual_id = m.id));

-- name: DeleteSpentManualReminder :execrows
DELETE FROM manual_reminders m
WHERE m.id = @id AND m.remind_at <= @now
  AND NOT EXISTS (SELECT 1 FROM scheduled_reminders s WHERE s.manual_id = m.id);
