-- name: InsertMCPLink :exec
INSERT INTO mcp_links (id, user_id, client_id) VALUES ($1, $2, $3);

-- name: InsertMCPLinkSite :exec
INSERT INTO mcp_link_sites (link_id, workspace_id, site_id) VALUES ($1, $2, $3);

-- name: ActiveMCPLinkSites :many
SELECT ms.workspace_id, ms.site_id
FROM mcp_links AS ml
JOIN mcp_link_sites AS ms ON ms.link_id = ml.id
WHERE ml.id = $1 AND ml.user_id = $2 AND ml.client_id = $3 AND ml.revoked_at IS NULL
ORDER BY ms.workspace_id, ms.site_id;

-- name: RevokeMCPLink :execrows
UPDATE mcp_links SET revoked_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;
