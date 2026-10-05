-- name: SaveVaultSession :exec
INSERT INTO vault_sessions (session_id, sealed) VALUES ($1, $2)
ON CONFLICT (session_id) DO UPDATE SET sealed = EXCLUDED.sealed;

-- name: GetVaultSession :one
SELECT sealed FROM vault_sessions WHERE session_id = $1;

-- name: DeleteVaultSession :exec
DELETE FROM vault_sessions WHERE session_id = $1;
