-- +goose Up
ALTER TABLE users ADD COLUMN file_double_click text NOT NULL DEFAULT 'download' CHECK (file_double_click IN ('download', 'edit'));

-- +goose Down
ALTER TABLE users DROP COLUMN file_double_click;
