-- name: InsertKnownHost :exec
INSERT INTO known_hosts (workspace_id, host, port, key_type, fingerprint_sha256, first_seen, accepted_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, host, port, key_type) DO NOTHING;

-- name: KnownHostsFor :many
SELECT host, port, key_type, fingerprint_sha256, first_seen, accepted_by
FROM known_hosts WHERE workspace_id = $1 AND host = $2 AND port = $3 ORDER BY key_type;

-- name: KnownHosts :many
SELECT host, port, key_type, fingerprint_sha256, first_seen, accepted_by
FROM known_hosts WHERE workspace_id = $1 ORDER BY host, port, key_type;

-- name: DeleteKnownHost :execrows
DELETE FROM known_hosts WHERE workspace_id = $1 AND host = $2 AND port = $3 AND key_type = $4;
