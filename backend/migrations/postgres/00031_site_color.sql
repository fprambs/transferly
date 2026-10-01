-- +goose Up
ALTER TABLE sites ADD COLUMN color text NOT NULL DEFAULT '' CHECK (color IN ('', 'red', 'orange', 'yellow', 'green', 'teal', 'blue', 'violet', 'pink'));

-- +goose Down
ALTER TABLE sites DROP COLUMN color;
