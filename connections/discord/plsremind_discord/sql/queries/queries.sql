-- name: AddDelivery :exec
INSERT OR IGNORE INTO deliveries (delivery_id, sent_at) VALUES (?, ?);

-- name: FindDelivery :one
SELECT delivery_id FROM deliveries WHERE delivery_id = ?;

-- name: PurgeDeliveries :execrows
DELETE FROM deliveries WHERE sent_at < ?;

-- name: AddMessage :exec
INSERT INTO messages (message_id, channel_id, account_id, user_id, item_id, item_updated_at, sent_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: MessagesForItem :many
SELECT * FROM messages WHERE item_id = ? ORDER BY message_id;

-- name: MessagesForUser :many
SELECT * FROM messages WHERE user_id = ? ORDER BY message_id;

-- name: StampMessage :exec
UPDATE messages SET item_updated_at = ? WHERE message_id = ?;

-- name: ForgetMessage :execrows
DELETE FROM messages WHERE message_id = ?;

-- name: ForgetUser :execrows
DELETE FROM messages WHERE user_id = ?;

-- name: PurgeMessages :execrows
DELETE FROM messages WHERE sent_at < ?;
