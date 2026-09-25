-- +goose Up
CREATE TABLE transfers (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    site_id uuid REFERENCES sites (id) ON DELETE SET NULL,
    site_name text NOT NULL,
    direction text NOT NULL CHECK (direction IN ('upload', 'download')),
    path text NOT NULL,
    name text NOT NULL,
    status text NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed', 'canceled', 'interrupted')),
    total_bytes bigint CHECK (total_bytes >= 0),
    bytes_done bigint NOT NULL DEFAULT 0 CHECK (bytes_done >= 0),
    error_code text NOT NULL DEFAULT '',
    attempts integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    started_at timestamptz,
    updated_at timestamptz NOT NULL,
    finished_at timestamptz
);
CREATE INDEX transfers_user_created_idx ON transfers (user_id, created_at DESC);
CREATE INDEX transfers_workspace_idx ON transfers (workspace_id);

-- +goose Down
DROP TABLE transfers;
