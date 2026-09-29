-- +goose Up
CREATE TABLE site_bookmarks (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    site_id uuid NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    path text NOT NULL CHECK (left(path, 1) = '/' AND octet_length(path) <= 4096),
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX site_bookmarks_name_key ON site_bookmarks (site_id, lower(name));

-- +goose Down
DROP TABLE site_bookmarks;
