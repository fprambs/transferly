-- name: GetWorkspace :one
SELECT id, name, kind, owner_id, created_at FROM workspaces WHERE id = $1;

-- name: WorkspaceMembers :many
SELECT m.user_id, u.email, m.role, m.created_at
FROM memberships m JOIN users u ON u.id = m.user_id
WHERE m.workspace_id = $1
ORDER BY (m.role = 'owner') DESC, m.created_at, u.email;

-- name: UpdateMembershipRole :execrows
UPDATE memberships SET role = $3 WHERE workspace_id = $1 AND user_id = $2 AND role <> 'owner';

-- name: DeleteMembership :execrows
DELETE FROM memberships WHERE workspace_id = $1 AND user_id = $2 AND role <> 'owner';

-- name: SetMembershipRole :execrows
UPDATE memberships SET role = $3 WHERE workspace_id = $1 AND user_id = $2;

-- name: SetWorkspaceOwner :exec
UPDATE workspaces SET owner_id = $2 WHERE id = $1;
