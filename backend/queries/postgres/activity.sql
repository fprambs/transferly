-- name: InsertActivity :exec
INSERT INTO activity (id, workspace_id, user_id, site_id, site_name, kind, path, detail, error_code, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: ActivityByUserWorkspace :many
SELECT id, workspace_id, user_id, site_id, site_name, kind, path, detail, error_code, created_at
FROM activity
WHERE user_id = $1 AND workspace_id = $2 AND (sqlc.narg('site_id')::uuid IS NULL OR site_id = sqlc.narg('site_id')::uuid)
ORDER BY created_at DESC, id DESC
LIMIT $3;

-- name: PurgeActivity :execrows
DELETE FROM activity WHERE created_at < $1;
