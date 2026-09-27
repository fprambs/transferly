-- +goose Up
ALTER TABLE sites ADD COLUMN speed_limit bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE sites DROP COLUMN speed_limit;
