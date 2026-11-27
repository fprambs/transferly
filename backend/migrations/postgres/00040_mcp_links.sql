-- +goose Up
-- A link persists the user's consent. Unlock generations and vault keys live only in memory.
CREATE TABLE mcp_links (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_id text NOT NULL CHECK (client_id <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz CHECK (revoked_at >= created_at)
);
CREATE INDEX mcp_links_user_id_idx ON mcp_links (user_id);

ALTER TABLE sites ADD CONSTRAINT sites_workspace_id_id_key UNIQUE (workspace_id, id);
CREATE TABLE mcp_link_sites (
    link_id uuid NOT NULL REFERENCES mcp_links (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL,
    site_id uuid NOT NULL,
    PRIMARY KEY (link_id, workspace_id, site_id),
    FOREIGN KEY (workspace_id, site_id) REFERENCES sites (workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX mcp_link_sites_target_idx ON mcp_link_sites (workspace_id, site_id);

-- +goose Down
DROP TABLE mcp_link_sites;
ALTER TABLE sites DROP CONSTRAINT sites_workspace_id_id_key;
DROP TABLE mcp_links;
