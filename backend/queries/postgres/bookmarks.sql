-- name: InsertBookmark :exec
INSERT INTO site_bookmarks (id, workspace_id, site_id, name, path, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: UpdateBookmark :execrows
UPDATE site_bookmarks SET name = $4, path = $5 WHERE id = $1 AND workspace_id = $2 AND site_id = $3;

-- name: BookmarkByID :one
SELECT id, workspace_id, site_id, name, path, created_by, created_at FROM site_bookmarks WHERE id = $1 AND workspace_id = $2 AND site_id = $3;

-- name: BookmarksBySite :many
SELECT id, workspace_id, site_id, name, path, created_by, created_at FROM site_bookmarks WHERE workspace_id = $1 AND site_id = $2 ORDER BY lower(name), id;

-- name: DeleteBookmark :execrows
DELETE FROM site_bookmarks WHERE id = $1 AND workspace_id = $2 AND site_id = $3;
