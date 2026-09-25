-- name: InsertTransfer :exec
INSERT INTO transfers (id, workspace_id, user_id, site_id, site_name, direction, path, name, status, total_bytes, bytes_done,
                       error_code, attempts, created_at, started_at, updated_at, finished_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17);

-- name: UpdateTransfer :execrows
UPDATE transfers
SET status = $3, total_bytes = $4, bytes_done = $5, error_code = $6, attempts = $7, started_at = $8, updated_at = $9, finished_at = $10
WHERE id = $1 AND user_id = $2;

-- name: TransferByID :one
SELECT id, workspace_id, user_id, site_id, site_name, direction, path, name, status, total_bytes, bytes_done, error_code,
       attempts, created_at, started_at, updated_at, finished_at
FROM transfers WHERE id = $1 AND user_id = $2;

-- name: TransfersByUser :many
SELECT id, workspace_id, user_id, site_id, site_name, direction, path, name, status, total_bytes, bytes_done, error_code,
       attempts, created_at, started_at, updated_at, finished_at
FROM transfers WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2;

-- name: TransfersByUserWorkspace :many
SELECT id, workspace_id, user_id, site_id, site_name, direction, path, name, status, total_bytes, bytes_done, error_code,
       attempts, created_at, started_at, updated_at, finished_at
FROM transfers WHERE user_id = $1 AND workspace_id = $2 ORDER BY created_at DESC, id DESC LIMIT $3;

-- name: DeleteTransfer :execrows
DELETE FROM transfers WHERE id = $1 AND user_id = $2;

-- name: InterruptActiveTransfers :execrows
UPDATE transfers SET status = 'interrupted', error_code = 'server_restarted', finished_at = $1, updated_at = $1
WHERE status IN ('queued', 'running');
