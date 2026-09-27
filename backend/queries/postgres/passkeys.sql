-- name: ListPasskeys :many
SELECT id, name, credential, created_at, last_used_at FROM user_passkeys WHERE user_id = $1 ORDER BY created_at, id;

-- name: InsertPasskey :exec
INSERT INTO user_passkeys (id, user_id, name, credential_id, credential, created_at) VALUES ($1, $2, $3, $4, $5, $6);

-- name: DeletePasskey :execrows
DELETE FROM user_passkeys WHERE user_id = $1 AND id = $2;

-- name: TouchPasskey :execrows
UPDATE user_passkeys SET credential = $3, last_used_at = $4 WHERE user_id = $1 AND id = $2;
