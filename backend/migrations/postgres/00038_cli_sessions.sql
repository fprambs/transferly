-- +goose Up
-- Sessions of the terminal client and the approvals that start them (ADR-0029). A session is of the browser, with the cookie, or of
-- the terminal client, with a bearer token and longer lifetimes; the terminal client declares its name and the host it runs on.
ALTER TABLE user_sessions
    ADD COLUMN kind text NOT NULL DEFAULT 'browser' CHECK (kind IN ('browser', 'cli')),
    ADD COLUMN name text NOT NULL DEFAULT '',
    ADD COLUMN host text NOT NULL DEFAULT '';
CREATE INDEX user_sessions_user_id_kind_idx ON user_sessions (user_id, kind);

-- An approval in the browser waiting to be exchanged for a session: the SHA-256 of its single-use code, the PKCE S256 challenge, the
-- loopback address to send the code to or empty when the person pastes it, and what the client declared. Used approvals keep
-- used_at until they expire, so a second exchange finds nothing.
CREATE TABLE cli_authorizations (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash bytea NOT NULL UNIQUE,
    code_challenge text NOT NULL,
    redirect_uri text NOT NULL DEFAULT '',
    client_name text NOT NULL,
    client_host text NOT NULL,
    ip text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz
);
CREATE INDEX cli_authorizations_expires_at_idx ON cli_authorizations (expires_at);

-- +goose Down
-- Terminal sessions end first: without their kind they would become browser sessions that last 30 days.
DELETE FROM user_sessions WHERE kind = 'cli';
DROP TABLE cli_authorizations;
DROP INDEX user_sessions_user_id_kind_idx;
ALTER TABLE user_sessions DROP COLUMN host, DROP COLUMN name, DROP COLUMN kind;
