-- +goose Up
ALTER TABLE sites DROP CONSTRAINT sites_auth_method_check;
ALTER TABLE sites ADD CONSTRAINT sites_auth_method_check CHECK (auth_method IN ('password', 'key', 'ask', 'anonymous', 'account'));
ALTER TABLE site_secrets DROP CONSTRAINT site_secrets_kind_check;
ALTER TABLE site_secrets ADD CONSTRAINT site_secrets_kind_check
  CHECK (kind IN ('password', 'private_key', 'passphrase', 'jump_password', 'jump_private_key', 'jump_passphrase', 'proxy_password', 'account'));

-- +goose Down
DELETE FROM site_secrets WHERE kind = 'account';
UPDATE sites SET auth_method = 'password' WHERE auth_method = 'account';
ALTER TABLE site_secrets DROP CONSTRAINT site_secrets_kind_check;
ALTER TABLE site_secrets ADD CONSTRAINT site_secrets_kind_check
  CHECK (kind IN ('password', 'private_key', 'passphrase', 'jump_password', 'jump_private_key', 'jump_passphrase', 'proxy_password'));
ALTER TABLE sites DROP CONSTRAINT sites_auth_method_check;
ALTER TABLE sites ADD CONSTRAINT sites_auth_method_check CHECK (auth_method IN ('password', 'key', 'ask', 'anonymous'));
