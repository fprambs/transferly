-- +goose Up
ALTER TABLE users ADD COLUMN theme text NOT NULL DEFAULT 'transferly-dark' CHECK (theme ~ '^[a-z0-9][a-z0-9-]{0,39}$');

-- +goose Down
ALTER TABLE users DROP COLUMN theme;
