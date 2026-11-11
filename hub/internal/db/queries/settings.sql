-- name: GetSettings :one
SELECT name, registration FROM instance_settings;

-- name: UpdateSettings :one
UPDATE instance_settings SET name = @name, registration = @registration
RETURNING name, registration;
