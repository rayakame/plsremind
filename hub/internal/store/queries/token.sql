-- name: CreatePersonalToken :one
INSERT INTO personal_tokens (id, user_id, name, remind, token_hash, created_at)
VALUES (@id, @user_id, @name, @remind, @token_hash, @created_at)
RETURNING *;

-- name: CountPersonalTokens :one
SELECT count(*) FROM personal_tokens WHERE user_id = @user_id;

-- name: ListPersonalTokens :many
SELECT * FROM personal_tokens WHERE user_id = @user_id ORDER BY id;

-- name: DeletePersonalToken :execrows
DELETE FROM personal_tokens WHERE id = @id AND user_id = @user_id;

-- name: GetPersonalTokenUser :one
SELECT t.id AS token_id, t.last_used_at, u.id AS user_id, u.role, u.disabled
FROM personal_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = @token_hash;

-- name: TouchPersonalToken :exec
UPDATE personal_tokens SET last_used_at = @now WHERE id = @id;

-- name: DeleteUserPersonalTokens :exec
DELETE FROM personal_tokens WHERE user_id = @user_id;

-- name: GetPersonalToken :one
SELECT * FROM personal_tokens WHERE id = @id AND user_id = @user_id FOR NO KEY UPDATE;

-- name: UpdatePersonalToken :one
UPDATE personal_tokens SET name = @name, remind = @remind WHERE id = @id
RETURNING *;
