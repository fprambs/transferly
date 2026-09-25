-- +goose Up
CREATE UNIQUE INDEX sites_workspace_name_key ON sites (workspace_id, lower(name));

-- +goose Down
DROP INDEX sites_workspace_name_key;
