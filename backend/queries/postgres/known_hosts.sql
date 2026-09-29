-- name: InsertKnownHost :exec
INSERT INTO known_hosts (workspace_id, host, port, key_type, fingerprint_sha256, first_seen, accepted_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, host, port, key_type, fingerprint_sha256) DO NOTHING;

-- name: KnownHostsFor :many
SELECT host, port, key_type, fingerprint_sha256, first_seen, accepted_by
FROM known_hosts WHERE workspace_id = $1 AND host = $2 AND port = $3 ORDER BY key_type, first_seen, fingerprint_sha256;

-- name: KnownHosts :many
SELECT host, port, key_type, fingerprint_sha256, first_seen, accepted_by
FROM known_hosts WHERE workspace_id = $1 ORDER BY host, port, key_type, first_seen, fingerprint_sha256;

-- name: DeleteKnownHost :execrows
DELETE FROM known_hosts
WHERE workspace_id = $1 AND host = $2 AND port = $3 AND key_type = $4 AND (sqlc.arg(fingerprint)::text = '' OR fingerprint_sha256 = sqlc.arg(fingerprint)::text);
