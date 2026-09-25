# Connection pool

`core/connections` keeps the connections to remote servers. The composition root wires it to `adapters/remotefs/connect`, which reads the site, opens its credentials with the vault key of the session, checks the server key against the trusted ones and dials through the network guard.

## Rules

- One pool per user and site (`Key{UserID, WorkspaceID, SiteID}`), at most 4 connections each. Connections are never shared between users, so nobody reuses a login made with credentials they cannot open.
- `Acquire` reuses an idle connection, opens a new one while there is room, or waits. A borrowed connection goes back with `Lease.Release(err)`: a lost connection (`remotefs.Retryable`) is closed instead of pooled.
- Connecting retries only network failures, with doubling waits (200 ms to 5 s) and jitter, up to 4 attempts. Bad credentials, a changed or unknown host key, a forbidden destination and a locked vault are returned at once.
- `With` runs work on a pooled connection and repeats it once on a fresh one when the connection was lost, so the work must be safe to repeat. Transfers use their own resume logic instead.
- Idle connections are pinged every 30 s (`Sweep`, driven by `Run`), closed after 5 minutes idle, and validated with a ping before use when idle for more than 10 s.
- Locking the vault or signing out calls `CloseUser`: idle connections close now, borrowed ones when released.

## Tests

The pool is tested with a fake connector and a fake clock; the connector with a real database and the in-process SFTP server. Concurrency is checked with the race detector, and the suite was mutated on purpose (no backoff growth, retrying permanent errors, pooling broken connections, ignoring pings, closing other users, not waking waiters): every mutation fails a test.
