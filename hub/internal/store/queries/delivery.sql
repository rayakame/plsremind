-- name: LockDueReminders :many
-- Whole items at a time: which reminder of an item goes out depends on all of
-- its due reminders, so they must not be split across batches.
SELECT r.* FROM scheduled_reminders r
WHERE r.fire_at <= @now
  AND r.item_id IN (
      SELECT s.item_id FROM scheduled_reminders s
      WHERE s.fire_at <= @now
      GROUP BY s.item_id
      ORDER BY min(s.fire_at), s.item_id
      LIMIT @batch)
ORDER BY r.fire_at, r.id
FOR UPDATE OF r SKIP LOCKED;

-- name: DeleteScheduledReminders :exec
DELETE FROM scheduled_reminders WHERE id = ANY(@ids::uuid[]);

-- name: InsertReminderLog :exec
INSERT INTO reminder_log (id, user_id, item_id, kind, offset_seconds, scheduled_for, fire_at, fired_at, outcome)
VALUES (@id, @user_id, @item_id, @kind, @offset_seconds, @scheduled_for, @fire_at, @fired_at, @outcome);

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
WHERE id IN (
    SELECT d.id FROM deliveries d
    WHERE d.item_id = @item_id AND d.connection_id = @connection_id AND d.status = 'pending'
    ORDER BY d.id
    FOR UPDATE)
RETURNING *;

-- name: SupersedeTokenDeliveries :many
UPDATE deliveries SET status = 'superseded', closed_at = @now
WHERE id IN (
    SELECT d.id FROM deliveries d
    WHERE d.item_id = @item_id AND d.token_id = @token_id AND d.status = 'pending'
    ORDER BY d.id
    FOR UPDATE)
RETURNING *;

-- name: ExpireDeliveries :many
UPDATE deliveries SET status = 'expired', closed_at = @now
WHERE id IN (
    SELECT d.id FROM deliveries d
    WHERE d.status = 'pending' AND d.expires_at < @now
    ORDER BY d.id
    FOR UPDATE)
RETURNING *;

-- name: CancelItemDeliveries :many
UPDATE deliveries SET status = 'cancelled', closed_at = @now
WHERE id IN (
    SELECT d.id FROM deliveries d
    WHERE d.item_id = @item_id AND d.status = 'pending'
    ORDER BY d.id
    FOR UPDATE)
RETURNING *;

-- name: CancelConnectionDeliveries :many
UPDATE deliveries SET status = 'cancelled', closed_at = @now
WHERE id IN (
    SELECT d.id FROM deliveries d
    WHERE d.connection_id = @connection_id AND d.status = 'pending'
      AND (sqlc.narg('user_id')::uuid IS NULL OR d.user_id = sqlc.narg('user_id'))
    ORDER BY d.id
    FOR UPDATE)
RETURNING *;

-- name: CancelTokenDeliveries :many
UPDATE deliveries SET status = 'cancelled', closed_at = @now
WHERE id IN (
    SELECT d.id FROM deliveries d
    WHERE d.token_id = @token_id AND d.status = 'pending'
    ORDER BY d.id
    FOR UPDATE)
RETURNING *;

-- name: ListPendingConnectionDeliveries :many
SELECT sqlc.embed(d), sqlc.embed(r)
FROM deliveries d
JOIN reminder_log r ON r.id = d.reminder_id
WHERE d.connection_id = @connection_id AND d.status = 'pending'
  AND (NOT @only_unsent::boolean OR d.attempts = 0)
ORDER BY d.id
FOR UPDATE OF d;

-- name: ListPendingTokenDeliveries :many
SELECT sqlc.embed(d), sqlc.embed(r)
FROM deliveries d
JOIN reminder_log r ON r.id = d.reminder_id
WHERE d.token_id = @token_id AND d.status = 'pending'
  AND (NOT @only_unsent::boolean OR d.attempts = 0)
ORDER BY d.id
FOR UPDATE OF d;

-- name: GetPendingDelivery :one
SELECT sqlc.embed(d), sqlc.embed(r)
FROM deliveries d
JOIN reminder_log r ON r.id = d.reminder_id
WHERE d.id = @id AND d.status = 'pending'
FOR UPDATE OF d;

-- name: SendDelivery :one
UPDATE deliveries SET attempts = attempts + 1, held_until = NULL WHERE id = @id
RETURNING attempts;

-- name: HoldDelivery :exec
UPDATE deliveries SET held_until = @held_until WHERE id = @id;

-- name: LockDelivery :one
SELECT * FROM deliveries WHERE id = @id FOR UPDATE;

-- name: ListExternalIDs :many
SELECT external_id FROM identities WHERE user_id = @user_id AND provider = @provider ORDER BY created_at, id;

-- name: GetPersonalTokenByID :one
SELECT * FROM personal_tokens WHERE id = @id;

-- name: ListReminderLog :many
SELECT * FROM reminder_log
WHERE user_id = @user_id
  AND (sqlc.narg('item_id')::uuid IS NULL OR item_id = sqlc.narg('item_id'))
  AND (sqlc.narg('before_id')::uuid IS NULL OR id < sqlc.narg('before_id'))
ORDER BY id DESC
LIMIT @page_size;

-- name: ListLogDeliveries :many
SELECT d.id, d.reminder_id, d.status, d.reason, d.attempts, d.connection_id, d.token_id,
       COALESCE(c.name, t.name, '')::text AS channel_name
FROM deliveries d
LEFT JOIN connections c ON c.id = d.connection_id
LEFT JOIN personal_tokens t ON t.id = d.token_id
WHERE d.reminder_id = ANY(@reminder_ids::uuid[])
ORDER BY d.id;

-- name: DeleteOldReminderLog :exec
DELETE FROM reminder_log WHERE fired_at <= @before;

-- name: ReleaseHeldDeliveries :many
UPDATE deliveries SET held_until = NULL
WHERE status = 'pending' AND held_until <= @now
RETURNING id;

-- name: GetDeliveryChannel :one
SELECT connection_id, token_id FROM deliveries WHERE id = @id AND status = 'pending';

-- name: ReleaseUserHeldDeliveries :exec
UPDATE deliveries SET held_until = @now
WHERE user_id = @user_id AND status = 'pending' AND held_until IS NOT NULL;
