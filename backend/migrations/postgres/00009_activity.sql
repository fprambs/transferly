-- +goose Up
CREATE TABLE activity (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    site_id uuid REFERENCES sites (id) ON DELETE SET NULL,
    site_name text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('mkdir', 'rename', 'delete', 'checksum', 'transfer')),
    path text NOT NULL,
    detail text NOT NULL DEFAULT '',
    error_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL
);
CREATE INDEX activity_user_workspace_idx ON activity (user_id, workspace_id, created_at DESC);
CREATE INDEX activity_created_idx ON activity (created_at);

-- +goose Down
DROP TABLE activity;
