-- +goose Up
CREATE TABLE user_totp (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    sealed bytea NOT NULL,
    confirmed boolean NOT NULL DEFAULT false,
    last_step bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_recovery_codes (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash bytea NOT NULL,
    used_at timestamptz,
    PRIMARY KEY (user_id, code_hash)
);

-- +goose Down
DROP TABLE user_recovery_codes;
DROP TABLE user_totp;
