-- name: ListStrictServers :many
SELECT address FROM strict_servers ORDER BY address;

-- name: RememberStrictServer :exec
INSERT INTO strict_servers (address) VALUES ($1) ON CONFLICT (address) DO NOTHING;
