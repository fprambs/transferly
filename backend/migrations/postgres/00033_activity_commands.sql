-- +goose Up
ALTER TABLE activity DROP CONSTRAINT activity_kind_check;
ALTER TABLE activity ADD CONSTRAINT activity_kind_check CHECK (kind IN ('mkdir', 'rename', 'delete', 'checksum', 'transfer', 'compress', 'extract'));

-- +goose Down
DELETE FROM activity WHERE kind IN ('compress', 'extract');
ALTER TABLE activity DROP CONSTRAINT activity_kind_check;
ALTER TABLE activity ADD CONSTRAINT activity_kind_check CHECK (kind IN ('mkdir', 'rename', 'delete', 'checksum', 'transfer'));
