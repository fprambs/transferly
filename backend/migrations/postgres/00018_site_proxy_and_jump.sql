-- +goose Up
ALTER TABLE sites ADD COLUMN proxy_kind text NOT NULL DEFAULT '';
ALTER TABLE sites ADD COLUMN proxy_host text NOT NULL DEFAULT '';
ALTER TABLE sites ADD COLUMN proxy_port integer NOT NULL DEFAULT 0;
ALTER TABLE sites ADD COLUMN proxy_username text NOT NULL DEFAULT '';
ALTER TABLE sites ADD COLUMN jump_host text NOT NULL DEFAULT '';
ALTER TABLE sites ADD COLUMN jump_port integer NOT NULL DEFAULT 0;
ALTER TABLE sites ADD COLUMN jump_username text NOT NULL DEFAULT '';
ALTER TABLE sites ADD COLUMN jump_auth_method text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sites DROP COLUMN proxy_kind;
ALTER TABLE sites DROP COLUMN proxy_host;
ALTER TABLE sites DROP COLUMN proxy_port;
ALTER TABLE sites DROP COLUMN proxy_username;
ALTER TABLE sites DROP COLUMN jump_host;
ALTER TABLE sites DROP COLUMN jump_port;
ALTER TABLE sites DROP COLUMN jump_username;
ALTER TABLE sites DROP COLUMN jump_auth_method;
