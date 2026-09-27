-- +goose Up
CREATE TABLE instance_limits (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    data jsonb NOT NULL,
    updated_by uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE instance_limits;
