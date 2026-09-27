-- name: GetInstanceLimits :one
SELECT data FROM instance_limits WHERE id;

-- name: UpsertInstanceLimits :exec
INSERT INTO instance_limits (id, data, updated_by, updated_at) VALUES (true, $1, $2, $3)
ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at;

-- name: CountWorkspaceSites :one
SELECT count(*) FROM sites WHERE workspace_id = $1;

-- name: CountOwnedTeams :one
SELECT count(*) FROM workspaces WHERE owner_id = $1 AND kind = 'team';

-- name: CountMembersAndInvitations :one
SELECT ((SELECT count(*) FROM memberships m WHERE m.workspace_id = @workspace_id) + (SELECT count(*) FROM invitations i WHERE i.workspace_id = @workspace_id AND i.expires_at > @now::timestamptz))::bigint AS total;

-- name: TransferBytesSince :one
SELECT COALESCE(sum(bytes_done), 0)::bigint AS total FROM transfers WHERE user_id = $1 AND updated_at >= $2;
