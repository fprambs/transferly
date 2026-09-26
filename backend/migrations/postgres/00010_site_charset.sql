-- +goose Up
ALTER TABLE sites ADD COLUMN charset text NOT NULL DEFAULT 'utf-8';

-- +goose Down
ALTER TABLE sites DROP COLUMN charset;
