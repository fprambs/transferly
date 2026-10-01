-- +goose Up
ALTER TABLE users ALTER COLUMN theme SET DEFAULT 'transferly-dark';
UPDATE users SET theme = 'transferly-' || substr(theme, length('sentinel-') + 1) WHERE theme IN ('sentinel-dark', 'sentinel-light');

-- +goose Down
UPDATE users SET theme = 'sentinel-' || substr(theme, length('transferly-') + 1) WHERE theme IN ('transferly-dark', 'transferly-light');
ALTER TABLE users ALTER COLUMN theme SET DEFAULT 'sentinel-dark';
