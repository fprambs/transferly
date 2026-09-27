-- name: ListAccounts :many
SELECT u.id, u.email, u.role, u.status, u.created_at,
       (EXISTS (SELECT 1 FROM user_totp t WHERE t.user_id = u.id AND t.confirmed)
        OR EXISTS (SELECT 1 FROM user_passkeys p WHERE p.user_id = u.id))::boolean AS second_factor
FROM users u ORDER BY u.created_at, u.id;

-- name: SetUserStatus :execrows
UPDATE users SET status = $2, updated_at = now() WHERE id = $1;

-- name: SetUserRole :execrows
UPDATE users SET role = $2, updated_at = now() WHERE id = $1;

-- name: CountActiveAdmins :one
SELECT count(*) FROM users WHERE role = 'admin' AND status = 'active';

-- name: ListNetworkAllowances :many
SELECT cidr FROM network_allowances ORDER BY cidr;

-- name: DeleteNetworkAllowances :exec
DELETE FROM network_allowances;

-- name: InsertNetworkAllowance :exec
INSERT INTO network_allowances (cidr, created_by, created_at) VALUES ($1, $2, $3);

-- name: DeleteUserPasskeys :exec
DELETE FROM user_passkeys WHERE user_id = $1;

-- name: AuditInstance :many
SELECT a.id, a.workspace_id, a.user_id, COALESCE(u.email, '')::text AS actor_email, a.action, a.target, a.ip, a.meta, a.at
FROM audit_events a LEFT JOIN users u ON u.id = a.user_id
WHERE a.workspace_id IS NULL AND starts_with(a.action, 'admin.')
  AND (@prefix::text = '' OR starts_with(a.action, @prefix::text))
  AND (@before::bigint = 0 OR a.id < @before::bigint)
ORDER BY a.id DESC
LIMIT @row_limit;
