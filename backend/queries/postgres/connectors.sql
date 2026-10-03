-- name: ListConnectorSettings :many
SELECT type, enabled FROM connector_settings;

-- name: UpsertConnectorSetting :exec
INSERT INTO connector_settings (type, enabled, updated_by, updated_at) VALUES ($1, $2, $3, $4)
ON CONFLICT (type) DO UPDATE SET enabled = EXCLUDED.enabled, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at;
