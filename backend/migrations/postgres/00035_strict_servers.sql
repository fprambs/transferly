-- +goose Up
-- The SFTP servers that proved to report sizes other than the bytes they send, or to drop pipelined reads, by address, so what
-- Transferly learns about them outlives its restarts.
CREATE TABLE strict_servers (
    address text PRIMARY KEY,
    learned_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE strict_servers;
