-- name: InsertAudit :exec
INSERT INTO audit_events (workspace_id, user_id, action, target, ip, meta, at) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: AuditByWorkspace :many
SELECT a.id, a.workspace_id, a.user_id, COALESCE(u.email, '')::text AS actor_email, a.action, a.target, a.ip, a.meta, a.at
FROM audit_events a LEFT JOIN users u ON u.id = a.user_id
WHERE a.workspace_id = @workspace_id
  AND (@prefix::text = '' OR starts_with(a.action, @prefix::text))
  AND (@before::bigint = 0 OR a.id < @before::bigint)
ORDER BY a.id DESC
LIMIT @row_limit;
