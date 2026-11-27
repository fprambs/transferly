-- name: InsertSession :exec
INSERT INTO user_sessions (id, user_id, token_hash, user_agent, ip, created_at, last_seen_at, expires_at, kind, name, host)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: SessionByTokenHash :one
SELECT id, user_id, token_hash, user_agent, ip, created_at, last_seen_at, expires_at, kind, name, host
FROM user_sessions WHERE token_hash = $1;

-- name: ListUserSessions :many
SELECT id, user_id, token_hash, user_agent, ip, created_at, last_seen_at, expires_at, kind, name, host
FROM user_sessions WHERE user_id = $1 AND kind = $2
ORDER BY last_seen_at DESC, id;

-- name: TouchSession :exec
UPDATE user_sessions SET last_seen_at = $2 WHERE id = $1;

-- name: DeleteSession :exec
DELETE FROM user_sessions WHERE id = $1;

-- name: DeleteUserSession :execrows
DELETE FROM user_sessions WHERE id = $1 AND user_id = $2;

-- name: DeleteUserSessions :execrows
DELETE FROM user_sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM user_sessions
WHERE expires_at <= sqlc.arg(now)
   OR (kind = 'browser' AND last_seen_at < sqlc.arg(browser_idle_before))
   OR (kind = 'cli' AND last_seen_at < sqlc.arg(cli_idle_before));
