-- +goose Up
-- Site names are unique within each collection, not within the whole workspace: two folders may each hold a site called "Internal".
DROP INDEX sites_workspace_name_key;
CREATE UNIQUE INDEX sites_folder_name_key
    ON sites (workspace_id, COALESCE(folder_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name));

-- +goose Down
-- Going back fails while two sites of a workspace share a name; rename one of them first.
DROP INDEX sites_folder_name_key;
CREATE UNIQUE INDEX sites_workspace_name_key ON sites (workspace_id, lower(name));
