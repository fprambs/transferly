-- name: InsertSite :exec
INSERT INTO sites (id, workspace_id, name, connector, config, created_at, updated_at, folder_id, speed_limit, remote_dir, color)
VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8, $9, $10);

-- name: UpdateSite :execrows
UPDATE sites
SET name = $3, connector = $4, config = $5, updated_at = $6, folder_id = $7, speed_limit = $8, remote_dir = $9, color = $10
WHERE id = $1 AND workspace_id = $2;

-- name: SiteByID :one
SELECT id, workspace_id, name, connector, config, created_at, updated_at, folder_id, speed_limit, remote_dir, color
FROM sites WHERE id = $1 AND workspace_id = $2;

-- name: SitesByWorkspace :many
SELECT id, workspace_id, name, connector, config, created_at, updated_at, folder_id, speed_limit, remote_dir, color
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
