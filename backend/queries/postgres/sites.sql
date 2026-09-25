-- name: InsertSite :exec
INSERT INTO sites (id, workspace_id, name, protocol, host, port, username, auth_method, list_timezone, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10);

-- name: UpdateSite :execrows
UPDATE sites
SET name = $3, protocol = $4, host = $5, port = $6, username = $7, auth_method = $8, list_timezone = $9, updated_at = $10
WHERE id = $1 AND workspace_id = $2;

-- name: SiteByID :one
SELECT id, workspace_id, name, protocol, host, port, username, auth_method, list_timezone, created_at, updated_at
FROM sites WHERE id = $1 AND workspace_id = $2;

-- name: SitesByWorkspace :many
SELECT id, workspace_id, name, protocol, host, port, username, auth_method, list_timezone, created_at, updated_at
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
