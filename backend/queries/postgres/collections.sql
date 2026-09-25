-- name: InsertCollection :exec
INSERT INTO site_folders (id, workspace_id, parent_id, name) VALUES ($1, $2, $3, $4);

-- name: UpdateCollection :execrows
UPDATE site_folders SET parent_id = $3, name = $4 WHERE id = $1 AND workspace_id = $2;

-- name: CollectionByID :one
SELECT id, workspace_id, parent_id, name FROM site_folders WHERE id = $1 AND workspace_id = $2;

-- name: CollectionsByWorkspace :many
SELECT id, workspace_id, parent_id, name FROM site_folders WHERE workspace_id = $1 ORDER BY lower(name), id;

-- name: CollectionHasContents :one
SELECT (EXISTS (SELECT 1 FROM site_folders f WHERE f.parent_id = $1 AND f.workspace_id = $2)
    OR EXISTS (SELECT 1 FROM sites s WHERE s.folder_id = $1 AND s.workspace_id = $2))::boolean AS has_contents;

-- name: DeleteCollection :execrows
DELETE FROM site_folders WHERE id = $1 AND workspace_id = $2;
