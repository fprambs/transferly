-- +goose Up
CREATE TABLE connector_settings (
    type text PRIMARY KEY,
    enabled boolean NOT NULL,
    updated_by uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE connector_settings;
