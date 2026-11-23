-- name: AddDelivery :exec
INSERT OR IGNORE INTO deliveries (delivery_id, sent_at) VALUES (?, ?);

-- name: FindDelivery :one
SELECT delivery_id FROM deliveries WHERE delivery_id = ?;

-- name: PurgeDeliveries :execrows
DELETE FROM deliveries WHERE sent_at < ?;

-- name: AddMessage :exec
INSERT INTO messages (message_id, channel_id, account_id, user_id, item_id, shown_at, sent_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: FindMessage :one
SELECT message_id FROM messages WHERE message_id = ?;

-- name: MessagesForItem :many
SELECT * FROM messages WHERE item_id = ? ORDER BY message_id;

-- name: MessagesForUser :many
SELECT * FROM messages WHERE user_id = ? ORDER BY message_id;

-- name: StampMessage :exec
UPDATE messages SET shown_at = ? WHERE message_id = ?;

-- name: ForgetMessage :execrows
DELETE FROM messages WHERE message_id = ?;

-- name: ForgetUser :execrows
DELETE FROM messages WHERE user_id = ?;

-- name: PurgeMessages :execrows
DELETE FROM messages WHERE sent_at < ?;

-- name: FindDmChannel :one
SELECT channel_id FROM dm_channels WHERE bot_id = ? AND account_id = ?;

-- name: KeepDmChannel :exec
INSERT INTO dm_channels (bot_id, account_id, channel_id) VALUES (?, ?, ?)
ON CONFLICT (bot_id, account_id) DO UPDATE SET channel_id = excluded.channel_id;

-- name: ForgetDmChannel :exec
DELETE FROM dm_channels WHERE bot_id = ? AND account_id = ? AND channel_id = ?;

-- name: ForgetAccountDmChannels :exec
DELETE FROM dm_channels WHERE account_id = ?;

-- name: ForgetAccountMessages :exec
DELETE FROM messages WHERE user_id = ? AND account_id = ?;

-- name: ForgetUserDmChannels :exec
DELETE FROM dm_channels WHERE account_id IN (SELECT account_id FROM messages WHERE user_id = ?);

-- name: PurgeDmChannels :execrows
DELETE FROM dm_channels WHERE NOT EXISTS (SELECT 1 FROM messages WHERE messages.account_id = dm_channels.account_id);
