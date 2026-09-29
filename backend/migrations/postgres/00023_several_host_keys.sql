-- +goose Up
ALTER TABLE known_hosts DROP CONSTRAINT known_hosts_pkey;
ALTER TABLE known_hosts ADD PRIMARY KEY (workspace_id, host, port, key_type, fingerprint_sha256);

-- +goose Down
DELETE FROM known_hosts newer USING known_hosts older
WHERE newer.workspace_id = older.workspace_id AND newer.host = older.host AND newer.port = older.port AND newer.key_type = older.key_type
  AND (newer.first_seen, newer.fingerprint_sha256) > (older.first_seen, older.fingerprint_sha256);
ALTER TABLE known_hosts DROP CONSTRAINT known_hosts_pkey;
ALTER TABLE known_hosts ADD PRIMARY KEY (workspace_id, host, port, key_type);
