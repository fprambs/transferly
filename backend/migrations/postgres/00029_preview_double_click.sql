-- +goose Up
ALTER TABLE users DROP CONSTRAINT users_file_double_click_check;
ALTER TABLE users ADD CONSTRAINT users_file_double_click_check CHECK (file_double_click IN ('download', 'edit', 'preview'));

-- +goose Down
UPDATE users SET file_double_click = 'download' WHERE file_double_click = 'preview';
ALTER TABLE users DROP CONSTRAINT users_file_double_click_check;
ALTER TABLE users ADD CONSTRAINT users_file_double_click_check CHECK (file_double_click IN ('download', 'edit'));
