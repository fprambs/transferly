-- name: InsertWorkspace :exec
INSERT INTO workspaces (id, name, kind, owner_id, created_at) VALUES ($1, $2, $3, $4, $5);

-- name: InsertMembership :exec
INSERT INTO memberships (workspace_id, user_id, role) VALUES ($1, $2, $3);

-- name: PersonalWorkspace :one
SELECT id, name, kind, owner_id, created_at FROM workspaces WHERE owner_id = $1 AND kind = 'personal';

-- name: WorkspacesForUser :many
SELECT w.id, w.name, w.kind, w.owner_id, w.created_at, m.role
FROM workspaces w JOIN memberships m ON m.workspace_id = w.id
WHERE m.user_id = $1
ORDER BY w.kind, w.name, w.created_at;

-- name: MembershipRole :one
SELECT role FROM memberships WHERE workspace_id = $1 AND user_id = $2;
