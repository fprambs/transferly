-- +goose Up
CREATE TABLE invitations (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email text NOT NULL,
    role text NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
    token_hash bytea NOT NULL UNIQUE,
    invited_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX invitations_one_per_email ON invitations (workspace_id, email);
CREATE INDEX invitations_expires_at_idx ON invitations (expires_at);

-- +goose Down
DROP TABLE invitations;
