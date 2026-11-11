-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= @now;

-- name: DeleteExpiredOAuthStates :exec
DELETE FROM oauth_states WHERE expires_at <= @now;

-- name: DeleteExpiredInvites :exec
DELETE FROM invites WHERE expires_at <= @now AND used_at IS NULL;
