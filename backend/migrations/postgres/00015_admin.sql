-- +goose Up
CREATE TABLE network_allowances (
    cidr text PRIMARY KEY,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE network_allowances;
