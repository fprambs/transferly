-- +goose Up
ALTER TABLE transfers DROP CONSTRAINT transfers_direction_check;
ALTER TABLE transfers ADD CONSTRAINT transfers_direction_check CHECK (direction IN ('upload', 'download', 'copy'));

-- +goose Down
DELETE FROM transfers WHERE direction = 'copy';
ALTER TABLE transfers DROP CONSTRAINT transfers_direction_check;
ALTER TABLE transfers ADD CONSTRAINT transfers_direction_check CHECK (direction IN ('upload', 'download'));
