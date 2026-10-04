-- +goose Up
ALTER TABLE sites ADD COLUMN connector text;
ALTER TABLE sites ADD COLUMN config jsonb NOT NULL DEFAULT '{}';
UPDATE sites SET
  connector = protocol,
  config = jsonb_strip_nulls(jsonb_build_object(
    'host', host,
    'port', port::text,
    'username', username,
    'auth', auth_method,
    'encryption', CASE WHEN protocol = 'ftp' THEN NULLIF(ftp_encryption, '') END,
    'list_timezone', CASE WHEN protocol = 'ftp' THEN list_timezone END,
    'charset', CASE WHEN protocol = 'ftp' THEN charset END,
    'proxy_kind', NULLIF(proxy_kind, ''),
    'proxy_host', CASE WHEN proxy_kind <> '' THEN proxy_host END,
    'proxy_port', CASE WHEN proxy_kind <> '' THEN proxy_port::text END,
    'proxy_username', CASE WHEN proxy_kind <> '' THEN NULLIF(proxy_username, '') END,
    'jump_host', CASE WHEN protocol = 'sftp' THEN NULLIF(jump_host, '') END,
    'jump_port', CASE WHEN protocol = 'sftp' AND jump_host <> '' THEN jump_port::text END,
    'jump_username', CASE WHEN protocol = 'sftp' AND jump_host <> '' THEN jump_username END,
    'jump_auth', CASE WHEN protocol = 'sftp' AND jump_host <> '' THEN jump_auth_method END
  ));
ALTER TABLE sites ALTER COLUMN connector SET NOT NULL;
ALTER TABLE sites
  DROP COLUMN protocol, DROP COLUMN host, DROP COLUMN port, DROP COLUMN username, DROP COLUMN auth_method,
  DROP COLUMN list_timezone, DROP COLUMN charset, DROP COLUMN options, DROP COLUMN ftp_encryption,
  DROP COLUMN proxy_kind, DROP COLUMN proxy_host, DROP COLUMN proxy_port, DROP COLUMN proxy_username,
  DROP COLUMN jump_host, DROP COLUMN jump_port, DROP COLUMN jump_username, DROP COLUMN jump_auth_method;
ALTER TABLE site_secrets DROP CONSTRAINT site_secrets_kind_check;
ALTER TABLE site_secrets ADD CONSTRAINT site_secrets_kind_check CHECK (kind ~ '^[a-z][a-z0-9_]{0,63}$');

-- +goose Down
DELETE FROM sites WHERE connector NOT IN ('sftp', 'ftp');
ALTER TABLE site_secrets DROP CONSTRAINT site_secrets_kind_check;
DELETE FROM site_secrets WHERE kind NOT IN ('password', 'private_key', 'passphrase', 'jump_password', 'jump_private_key', 'jump_passphrase', 'proxy_password', 'account');
ALTER TABLE site_secrets ADD CONSTRAINT site_secrets_kind_check
  CHECK (kind IN ('password', 'private_key', 'passphrase', 'jump_password', 'jump_private_key', 'jump_passphrase', 'proxy_password', 'account'));
ALTER TABLE sites
  ADD COLUMN protocol text NOT NULL DEFAULT 'sftp' CHECK (protocol IN ('sftp', 'ftp', 'ftps')),
  ADD COLUMN host text NOT NULL DEFAULT '',
  ADD COLUMN port integer NOT NULL DEFAULT 22,
  ADD COLUMN username text NOT NULL DEFAULT '',
  ADD COLUMN auth_method text NOT NULL DEFAULT 'password',
  ADD COLUMN list_timezone text NOT NULL DEFAULT 'UTC',
  ADD COLUMN charset text NOT NULL DEFAULT 'utf-8',
  ADD COLUMN options jsonb NOT NULL DEFAULT '{}',
  ADD COLUMN ftp_encryption text NOT NULL DEFAULT '' CHECK (ftp_encryption IN ('', 'optional', 'explicit', 'implicit', 'plain')),
  ADD COLUMN proxy_kind text NOT NULL DEFAULT '',
  ADD COLUMN proxy_host text NOT NULL DEFAULT '',
  ADD COLUMN proxy_port integer NOT NULL DEFAULT 0,
  ADD COLUMN proxy_username text NOT NULL DEFAULT '',
  ADD COLUMN jump_host text NOT NULL DEFAULT '',
  ADD COLUMN jump_port integer NOT NULL DEFAULT 0,
  ADD COLUMN jump_username text NOT NULL DEFAULT '',
  ADD COLUMN jump_auth_method text NOT NULL DEFAULT '';
UPDATE sites SET
  protocol = connector,
  host = config->>'host',
  port = (config->>'port')::integer,
  username = config->>'username',
  auth_method = config->>'auth',
  list_timezone = COALESCE(config->>'list_timezone', 'UTC'),
  charset = COALESCE(config->>'charset', 'utf-8'),
  ftp_encryption = COALESCE(config->>'encryption', ''),
  proxy_kind = COALESCE(config->>'proxy_kind', ''),
  proxy_host = COALESCE(config->>'proxy_host', ''),
  proxy_port = COALESCE((config->>'proxy_port')::integer, 0),
  proxy_username = COALESCE(config->>'proxy_username', ''),
  jump_host = COALESCE(config->>'jump_host', ''),
  jump_port = COALESCE((config->>'jump_port')::integer, 0),
  jump_username = COALESCE(config->>'jump_username', ''),
  jump_auth_method = COALESCE(config->>'jump_auth', '');
ALTER TABLE sites ADD CONSTRAINT sites_port_check CHECK (port BETWEEN 1 AND 65535);
ALTER TABLE sites ADD CONSTRAINT sites_auth_method_check CHECK (auth_method IN ('password', 'key', 'ask', 'anonymous', 'account', 'interactive'));
ALTER TABLE sites DROP COLUMN connector, DROP COLUMN config;
