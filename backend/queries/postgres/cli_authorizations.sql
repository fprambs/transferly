-- name: InsertCLIAuthorization :exec
INSERT INTO cli_authorizations (id, user_id, code_hash, code_challenge, redirect_uri, client_name, client_host, ip, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: UseCLIAuthorization :one
UPDATE cli_authorizations SET used_at = sqlc.arg(used_at)
WHERE code_hash = sqlc.arg(code_hash) AND used_at IS NULL
RETURNING id, user_id, code_hash, code_challenge, redirect_uri, client_name, client_host, ip, created_at, expires_at;

-- name: DeleteExpiredCLIAuthorizations :execrows
DELETE FROM cli_authorizations WHERE expires_at <= $1;
