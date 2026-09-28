# coder/websocket v1.8.15

Source: [pkg.go.dev/github.com/coder/websocket](https://pkg.go.dev/github.com/coder/websocket).

## What the library requires

- `websocket.Accept(w, r, opts)` upgrades the request; on failure it has already answered, so the handler just returns.
- **Always read from the connection.** Control frames (pings, pongs, close) are only handled while someone reads. Sentinel's stream only sends, so it calls `conn.CloseRead(ctx)`: a goroutine reads for it, `Ping` and `Close` keep working, and a client that sends a data message is disconnected with a policy violation.
- After any error from a method the library closes the connection, context expirations included. A read with a timeout therefore closes the connection when it times out; tests read in a goroutine instead.
- `Close` waits up to five seconds for the peer's close frame. When the client stopped answering there is nobody to shake hands with, so the handler ends with `CloseNow` (deferred) and skips `Close`.
- `wsjson.Write(ctx, conn, v)` writes a JSON message; every write gets its own deadline so a stalled client cannot hold the handler.

## Origin checks (cross-site WebSocket hijacking)

A browser sends the user's cookie on a WebSocket opened from any site, so the upgrade must check `Origin`.

- `AcceptOptions.OriginPatterns` are host patterns matched with `path.Match`; a pattern with `://` matches `scheme://host`. Sentinel passes its allowed origins, escaping `\ * ? [`.
- Sentinel also checks `Origin` itself before the upgrade, against `SENTINEL_BASE_URL` and `SENTINEL_ALLOWED_ORIGINS`, and answers a `cross_site_request` problem with 403. The library's check stays as defense in depth.
- A request without `Origin` comes from a non-browser client, which has no ambient cookie, and is allowed.
- `InsecureSkipVerify` is never used.

## The stream

`GET /api/v1/ws` (not in `openapi.yaml`, since an upgrade has no request/response shape) needs a session cookie. Messages are JSON `{"type": ..., "data": ...}`.

| Type | Data |
|---|---|
| `snapshot` | `{"transfers": [...]}` the transfers running right now; always the first message |
| `transfer.state` | the transfer, whenever its state changes |
| `transfer.progress` | `{"id", "bytesDone", "totalBytes"}`, at most every 250 ms per transfer |
| `vault.locked` | `null`, when the user locks the vault |
| `connection.log` | `{"workspaceId", "siteId", "time", "level", "step", "params"}`, one step of a connection to a site (ADR-0011); not replayed on reconnect |

A client that reconnects gets a new snapshot and reloads the history with `GET /api/v1/transfers`.

## Lifecycle

- The hub (`adapters/events`) drops a client that cannot keep up (buffer of 256 events); the client reconnects and reloads. Publishing never blocks the core.
- The server pings every 30 s and drops clients that do not answer within 10 s.
- The session is checked every 30 s with `auth.Service.Verify`, which does **not** record use: an open tab must not keep an unattended session alive. Signing out closes the session's streams at once.

## Tests

Tests use the library's own client against `httptest`, read in a background goroutine, and cover origins, snapshot, events per user, lock, sign-out, session expiry, data sent by the client, unresponsive clients and dropped subscriptions. A mutation that used `Authenticate` instead of `Verify` fails the keep-alive test.
