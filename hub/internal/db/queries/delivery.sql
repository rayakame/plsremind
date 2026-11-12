-- name: LockDueReminders :many
SELECT * FROM scheduled_reminders
WHERE fire_at <= @now
ORDER BY fire_at, id
LIMIT @batch
FOR UPDATE SKIP LOCKED;

-- name: DeleteScheduledReminders :exec
DELETE FROM scheduled_reminders WHERE id = ANY(@ids::uuid[]);

-- name: InsertReminderLog :exec
INSERT INTO reminder_log (id, user_id, item_id, kind, offset_seconds, scheduled_for, fired_at, outcome)
VALUES (@id, @user_id, @item_id, @kind, @offset_seconds, @scheduled_for, @fired_at, @outcome);

-- name: ListReminderConnections :many
SELECT c.id
FROM connections c
JOIN users u ON u.id = @user_id AND NOT u.disabled
LEFT JOIN connection_grants g ON g.connection_id = c.id AND g.user_id = u.id
WHERE c.enabled
  AND 'deliveries' = ANY(c.allowed_intents)
  AND COALESCE(g.remind, true)
  AND COALESCE(g.enabled, EXISTS (SELECT 1 FROM identities i WHERE i.user_id = u.id AND i.provider = c.provider))
ORDER BY c.id;

-- name: ListReminderTokens :many
SELECT t.id
FROM personal_tokens t
JOIN users u ON u.id = t.user_id AND NOT u.disabled
WHERE t.user_id = @user_id AND t.remind
ORDER BY t.id;

-- name: InsertDelivery :exec
INSERT INTO deliveries (id, reminder_id, user_id, item_id, connection_id, token_id, created_at, expires_at)
VALUES (@id, @reminder_id, @user_id, @item_id, @connection_id, @token_id, @created_at, @expires_at);

-- name: CloseDelivery :one
UPDATE deliveries SET status = @status, reason = @reason, closed_at = @now
WHERE id = @id AND status = 'pending'
RETURNING *;

-- name: SupersedeConnectionDeliveries :many
UPDATE deliveries SET status = 'superseded', closed_at = @now
WHERE item_id = @item_id AND connection_id = @connection_id AND status = 'pending'
RETURNING id;

-- name: SupersedeTokenDeliveries :many
UPDATE deliveries SET status = 'superseded', closed_at = @now
WHERE item_id = @item_id AND token_id = @token_id AND status = 'pending'
RETURNING id;

-- name: ExpireDeliveries :many
UPDATE deliveries SET status = 'expired', closed_at = @now
WHERE status = 'pending' AND expires_at < @now
RETURNING id;

-- name: CancelItemDeliveries :many
UPDATE deliveries SET status = 'cancelled', closed_at = @now
WHERE item_id = @item_id AND status = 'pending'
RETURNING id;

-- name: CancelConnectionDeliveries :many
UPDATE deliveries SET status = 'cancelled', closed_at = @now
WHERE connection_id = @connection_id AND status = 'pending'
  AND (sqlc.narg('user_id')::uuid IS NULL OR user_id = sqlc.narg('user_id'))
RETURNING id;

-- name: CancelTokenDeliveries :many
UPDATE deliveries SET status = 'cancelled', closed_at = @now
WHERE token_id = @token_id AND status = 'pending'
RETURNING id;
