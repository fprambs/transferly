-- +goose Up
CREATE TABLE user_passkeys (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name text NOT NULL,
    credential_id bytea NOT NULL UNIQUE,
    credential jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    last_used_at timestamptz
);
CREATE INDEX user_passkeys_user_idx ON user_passkeys (user_id, created_at);

-- +goose Down
DROP TABLE user_passkeys;
