-- +goose Up
-- The vault keys of sessions that remember their vault across restarts (ADR-0028), each sealed with a key derived from the token
-- of its session, which only the browser holds. Only development turns it on; a key goes away with its session.
CREATE TABLE vault_sessions (
    session_id uuid PRIMARY KEY REFERENCES user_sessions (id) ON DELETE CASCADE,
    sealed bytea NOT NULL
);

-- +goose Down
DROP TABLE vault_sessions;
