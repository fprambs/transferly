-- +goose Up
ALTER TABLE site_secrets DROP CONSTRAINT site_secrets_kind_check;
ALTER TABLE site_secrets ADD CONSTRAINT site_secrets_kind_check
  CHECK (kind IN ('password', 'private_key', 'passphrase', 'jump_password', 'jump_private_key', 'jump_passphrase', 'proxy_password'));

-- +goose Down
ALTER TABLE site_secrets DROP CONSTRAINT site_secrets_kind_check;
ALTER TABLE site_secrets ADD CONSTRAINT site_secrets_kind_check
  CHECK (kind IN ('password', 'private_key', 'passphrase'));
