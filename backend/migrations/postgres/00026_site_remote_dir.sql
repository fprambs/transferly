-- +goose Up
ALTER TABLE sites ADD COLUMN remote_dir text NOT NULL DEFAULT '' CHECK (remote_dir = '' OR (left(remote_dir, 1) = '/' AND octet_length(remote_dir) <= 4096));

-- +goose Down
ALTER TABLE sites DROP COLUMN remote_dir;
