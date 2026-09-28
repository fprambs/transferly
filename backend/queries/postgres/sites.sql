-- name: InsertSite :exec
INSERT INTO sites (
  id, workspace_id, name, protocol, host, port, username, auth_method, list_timezone, charset, created_at, updated_at, folder_id, speed_limit,
  proxy_kind, proxy_host, proxy_port, proxy_username, jump_host, jump_port, jump_username, jump_auth_method, ftp_encryption
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $12, $10, $10, $11, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22);

-- name: UpdateSite :execrows
UPDATE sites
SET name = $3, protocol = $4, host = $5, port = $6, username = $7, auth_method = $8, list_timezone = $9, charset = $12, updated_at = $10, folder_id = $11, speed_limit = $13,
  proxy_kind = $14, proxy_host = $15, proxy_port = $16, proxy_username = $17, jump_host = $18, jump_port = $19, jump_username = $20, jump_auth_method = $21,
  ftp_encryption = $22
WHERE id = $1 AND workspace_id = $2;

-- name: SiteByID :one
SELECT id, workspace_id, name, protocol, host, port, username, auth_method, list_timezone, charset, created_at, updated_at, folder_id, speed_limit,
  proxy_kind, proxy_host, proxy_port, proxy_username, jump_host, jump_port, jump_username, jump_auth_method, ftp_encryption
FROM sites WHERE id = $1 AND workspace_id = $2;

-- name: SitesByWorkspace :many
SELECT id, workspace_id, name, protocol, host, port, username, auth_method, list_timezone, charset, created_at, updated_at, folder_id, speed_limit,
  proxy_kind, proxy_host, proxy_port, proxy_username, jump_host, jump_port, jump_username, jump_auth_method, ftp_encryption
FROM sites WHERE workspace_id = $1 ORDER BY lower(name), id;

-- name: DeleteSite :execrows
DELETE FROM sites WHERE id = $1 AND workspace_id = $2;

-- name: UpsertSecret :exec
INSERT INTO site_secrets (site_id, kind, dek_version, sealed) VALUES ($1, $2, $3, $4)
ON CONFLICT (site_id, kind) DO UPDATE SET dek_version = EXCLUDED.dek_version, sealed = EXCLUDED.sealed;

-- name: DeleteSecret :exec
DELETE FROM site_secrets WHERE site_id = $1 AND kind = $2;

-- name: SecretKindsByWorkspace :many
SELECT s.site_id, s.kind FROM site_secrets s JOIN sites ON sites.id = s.site_id
WHERE sites.workspace_id = $1 ORDER BY s.site_id, s.kind;

-- name: SecretBySiteKind :one
SELECT s.dek_version, s.sealed FROM site_secrets s JOIN sites ON sites.id = s.site_id
WHERE s.site_id = $1 AND sites.workspace_id = $2 AND s.kind = $3;
