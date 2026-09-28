-- +goose Up
ALTER TABLE sites DROP CONSTRAINT sites_auth_method_check;
ALTER TABLE sites ADD CONSTRAINT sites_auth_method_check CHECK (auth_method IN ('password', 'key', 'ask', 'anonymous'));
UPDATE sites SET auth_method = 'anonymous'
WHERE protocol = 'ftp' AND username = 'anonymous' AND auth_method = 'password'
  AND NOT EXISTS (SELECT 1 FROM site_secrets WHERE site_secrets.site_id = sites.id AND site_secrets.kind = 'password');

-- +goose Down
UPDATE sites SET auth_method = 'password' WHERE auth_method = 'anonymous';
ALTER TABLE sites DROP CONSTRAINT sites_auth_method_check;
ALTER TABLE sites ADD CONSTRAINT sites_auth_method_check CHECK (auth_method IN ('password', 'key', 'ask'));
