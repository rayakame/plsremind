-- name: CreateOAuthState :exec
INSERT INTO oauth_states (state_hash, provider, intent, user_id, invite_code, verifier, expires_at)
VALUES (@state_hash, @provider, @intent, @user_id, @invite_code, @verifier, @expires_at);

-- name: ConsumeOAuthState :one
DELETE FROM oauth_states WHERE state_hash = @state_hash
RETURNING *;

-- name: ListLoginProviders :many
SELECT * FROM login_providers ORDER BY type = 'password' DESC, key;

-- name: UpsertLoginProvider :one
INSERT INTO login_providers (key, type, name, login_enabled, link_enabled, client_id, client_secret, issuer_url)
VALUES (@key, @type, @name, @login_enabled, @link_enabled, @client_id, @client_secret, @issuer_url)
ON CONFLICT (key) DO UPDATE
SET name = EXCLUDED.name, login_enabled = EXCLUDED.login_enabled, link_enabled = EXCLUDED.link_enabled,
    client_id = EXCLUDED.client_id, client_secret = EXCLUDED.client_secret, issuer_url = EXCLUDED.issuer_url
RETURNING *;

-- name: DeleteLoginProvider :exec
DELETE FROM login_providers WHERE key = @key;

-- name: CountLoginMethods :one
SELECT count(*) FROM login_providers WHERE login_enabled;

-- name: CountProviderIdentities :one
SELECT count(*) FROM identities WHERE provider = @provider;

-- name: RefreshIdentity :exec
UPDATE identities SET display_name = @display_name, provider_token = @provider_token WHERE id = @id;

-- name: GetUserIdentity :one
SELECT * FROM identities WHERE id = @id AND user_id = @user_id;

-- name: DeleteIdentity :exec
DELETE FROM identities WHERE id = @id;

-- name: CountUsableLogins :one
SELECT count(*)
FROM identities i
JOIN login_providers p ON p.key = i.provider
WHERE i.user_id = @user_id AND p.login_enabled AND i.id <> @except_id;
