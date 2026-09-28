-- +goose Up
ALTER TABLE sites DROP CONSTRAINT sites_auth_method_check;
ALTER TABLE sites ADD CONSTRAINT sites_auth_method_check CHECK (auth_method IN ('password', 'key', 'ask'));

-- +goose Down
UPDATE sites SET auth_method = 'password' WHERE auth_method = 'ask';
ALTER TABLE sites DROP CONSTRAINT sites_auth_method_check;
ALTER TABLE sites ADD CONSTRAINT sites_auth_method_check CHECK (auth_method IN ('password', 'key'));
