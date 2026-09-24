-- name: InsertSession :exec
INSERT INTO user_sessions (id, user_id, token_hash, user_agent, ip, created_at, last_seen_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: SessionByTokenHash :one
SELECT id, user_id, token_hash, user_agent, ip, created_at, last_seen_at, expires_at
FROM user_sessions WHERE token_hash = $1;

-- name: TouchSession :exec
UPDATE user_sessions SET last_seen_at = $2 WHERE id = $1;

-- name: DeleteSession :exec
DELETE FROM user_sessions WHERE id = $1;

-- name: DeleteUserSessions :execrows
DELETE FROM user_sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM user_sessions WHERE expires_at <= $1 OR last_seen_at < $2;
