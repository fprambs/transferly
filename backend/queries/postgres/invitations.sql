-- name: DeleteInvitationForEmail :exec
DELETE FROM invitations WHERE workspace_id = $1 AND email = $2;

-- name: InsertInvitation :exec
INSERT INTO invitations (id, workspace_id, email, role, token_hash, invited_by, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: InvitationByTokenHash :one
SELECT i.id, i.workspace_id, w.name AS workspace_name, i.email, i.role, i.invited_by, COALESCE(u.email, '') AS invited_by_email, i.created_at, i.expires_at
FROM invitations i
JOIN workspaces w ON w.id = i.workspace_id
LEFT JOIN users u ON u.id = i.invited_by
WHERE i.token_hash = $1 AND i.expires_at > $2;

-- name: PendingInvitations :many
SELECT i.id, i.workspace_id, w.name AS workspace_name, i.email, i.role, i.invited_by, COALESCE(u.email, '') AS invited_by_email, i.created_at, i.expires_at
FROM invitations i
JOIN workspaces w ON w.id = i.workspace_id
LEFT JOIN users u ON u.id = i.invited_by
WHERE i.workspace_id = $1 AND i.expires_at > $2
ORDER BY i.created_at DESC;

-- name: CountPendingInvitations :one
SELECT count(*) FROM invitations WHERE workspace_id = $1 AND expires_at > $2;

-- name: DeleteInvitation :execrows
DELETE FROM invitations WHERE workspace_id = $1 AND id = $2;

-- name: ConsumeInvitation :one
DELETE FROM invitations WHERE id = $1 AND expires_at > $2 RETURNING workspace_id, role;

-- name: PurgeExpiredInvitations :execrows
DELETE FROM invitations WHERE expires_at <= $1;

-- name: InsertMembershipIfMissing :exec
INSERT INTO memberships (workspace_id, user_id, role) VALUES ($1, $2, $3) ON CONFLICT (workspace_id, user_id) DO NOTHING;
