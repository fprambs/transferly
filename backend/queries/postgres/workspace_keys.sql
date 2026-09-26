-- name: UserKeys :one
SELECT public_key, sealed_private_key FROM user_vaults WHERE user_id = $1;

-- name: SetUserKeys :execrows
UPDATE user_vaults SET public_key = $2, sealed_private_key = $3 WHERE user_id = $1 AND public_key IS NULL;

-- name: WorkspaceKeyState :one
SELECT key_version, rekey_pending FROM workspaces WHERE id = $1;

-- name: KeyGrant :one
SELECT sealed FROM workspace_key_grants WHERE workspace_id = $1 AND user_id = $2 AND version = $3;

-- name: InsertKeyGrant :exec
INSERT INTO workspace_key_grants (workspace_id, user_id, version, sealed) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING;

-- name: UngrantedMembers :many
SELECT m.user_id, v.public_key
FROM memberships m
JOIN user_vaults v ON v.user_id = m.user_id AND v.public_key IS NOT NULL
WHERE m.workspace_id = $1
  AND NOT EXISTS (SELECT 1 FROM workspace_key_grants g WHERE g.workspace_id = m.workspace_id AND g.user_id = m.user_id AND g.version = $2)
ORDER BY m.created_at;

-- name: GrantedMembers :many
SELECT user_id FROM workspace_key_grants WHERE workspace_id = $1 AND version = $2;

-- name: LockWorkspaceKey :one
SELECT key_version FROM workspaces WHERE id = $1 FOR UPDATE;

-- name: BumpWorkspaceKey :exec
UPDATE workspaces SET key_version = $2, rekey_pending = false WHERE id = $1;

-- name: StaleSecrets :many
SELECT s.site_id, s.kind, s.dek_version, s.sealed
FROM site_secrets s JOIN sites st ON st.id = s.site_id
WHERE st.workspace_id = $1 AND s.dek_version < $2
ORDER BY s.site_id, s.kind;

-- name: ResealSecret :execrows
UPDATE site_secrets SET sealed = $4, dek_version = $5 WHERE site_id = $1 AND kind = $2 AND dek_version = $3;

-- name: DeleteStaleGrants :exec
DELETE FROM workspace_key_grants g USING workspaces w
WHERE g.workspace_id = $1 AND w.id = g.workspace_id AND g.version < w.key_version
  AND NOT EXISTS (
      SELECT 1 FROM site_secrets s JOIN sites st ON st.id = s.site_id
      WHERE st.workspace_id = g.workspace_id AND s.dek_version = g.version
  );

-- name: MarkRekeyPending :exec
UPDATE workspaces SET rekey_pending = true WHERE id = $1 AND kind = 'team';

-- name: DeleteMemberGrants :exec
DELETE FROM workspace_key_grants WHERE workspace_id = $1 AND user_id = $2;
