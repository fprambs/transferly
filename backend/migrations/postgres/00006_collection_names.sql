-- +goose Up
CREATE UNIQUE INDEX site_folders_name_key
    ON site_folders (workspace_id, COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name));

-- +goose Down
DROP INDEX site_folders_name_key;
