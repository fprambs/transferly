-- +goose Up
ALTER TABLE sites ADD COLUMN ftp_encryption text NOT NULL DEFAULT ''
  CHECK (ftp_encryption IN ('', 'optional', 'explicit', 'implicit', 'plain'));
UPDATE sites SET
  ftp_encryption = CASE
    WHEN protocol = 'ftps' AND port = 990 THEN 'implicit'
    WHEN protocol = 'ftps' THEN 'explicit'
    WHEN protocol = 'ftp' THEN 'plain'
    ELSE ''
  END,
  protocol = CASE WHEN protocol = 'ftps' THEN 'ftp' ELSE protocol END;

-- +goose Down
UPDATE sites SET protocol = 'ftps' WHERE ftp_encryption IN ('explicit', 'implicit');
ALTER TABLE sites DROP COLUMN ftp_encryption;
