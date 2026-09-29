-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: LockUserCreation :exec
SELECT pg_advisory_xact_lock(4097083627);

-- name: InsertUser :exec
INSERT INTO users (id, email, password_hash, role, status, locale, timezone, created_at, theme, file_double_click)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: UserByEmail :one
SELECT id, email, password_hash, role, status, locale, timezone, created_at, theme, file_double_click
FROM users WHERE email = $1;

-- name: UserByID :one
SELECT id, email, password_hash, role, status, locale, timezone, created_at, theme, file_double_click
FROM users WHERE id = $1;

-- name: UpdatePasswordHash :exec
UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1;

-- name: UpdatePreferences :exec
UPDATE users SET locale = $2, timezone = $3, theme = $4, file_double_click = $5, updated_at = now() WHERE id = $1;
