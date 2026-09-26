-- +goose Up
ALTER TABLE user_vaults
    ADD COLUMN public_key bytea,
    ADD COLUMN sealed_private_key bytea,
    ADD CONSTRAINT user_vaults_keys_together CHECK ((public_key IS NULL) = (sealed_private_key IS NULL));

ALTER TABLE workspaces
    ADD COLUMN key_version integer NOT NULL DEFAULT 1,
    ADD COLUMN rekey_pending boolean NOT NULL DEFAULT false;

CREATE TABLE workspace_key_grants (
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    version integer NOT NULL,
    sealed bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id, version)
);
CREATE INDEX workspace_key_grants_user_idx ON workspace_key_grants (user_id);

-- +goose Down
DROP TABLE workspace_key_grants;
ALTER TABLE workspaces DROP COLUMN rekey_pending, DROP COLUMN key_version;
ALTER TABLE user_vaults DROP CONSTRAINT user_vaults_keys_together, DROP COLUMN sealed_private_key, DROP COLUMN public_key;
