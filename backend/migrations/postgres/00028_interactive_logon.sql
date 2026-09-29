-- +goose Up
ALTER TABLE sites DROP CONSTRAINT sites_auth_method_check;
ALTER TABLE sites ADD CONSTRAINT sites_auth_method_check CHECK (auth_method IN ('password', 'key', 'ask', 'anonymous', 'account', 'interactive'));

-- +goose Down
UPDATE sites SET auth_method = 'ask' WHERE auth_method = 'interactive';
ALTER TABLE sites DROP CONSTRAINT sites_auth_method_check;
ALTER TABLE sites ADD CONSTRAINT sites_auth_method_check CHECK (auth_method IN ('password', 'key', 'ask', 'anonymous', 'account'));
