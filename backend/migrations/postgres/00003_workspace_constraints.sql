-- +goose Up
CREATE UNIQUE INDEX workspaces_one_personal_per_user ON workspaces (owner_id) WHERE kind = 'personal';

-- +goose Down
DROP INDEX workspaces_one_personal_per_user;
