# ADR-0011: Live connection log

- Status: accepted
- Date: 2026-09-28

## Context

FileZilla shows, above the file panes, a log of what each connection is doing: resolving, connecting, the server key, signing in, listing, and every error. When something fails, that log is where the user, or whoever helps them, learns why. Sentinel showed only the final error, and the steps that led to it were lost; ADR-0012 also promised that FTP going on without TLS under optional encryption would be told somewhere.

## Decision

**Steps are events with stable codes.** The `connlog` package publishes a `connection.log` event per step to the clients of the user, through the same WebSocket hub as transfers. An entry names its site and workspace, its time, a level (info, warn, error), a step code and fixed parameters. The client translates the step, as it does error codes; the server never sends prose.

**Steps and their parameters:**

| Step | Level | Parameters |
|---|---|---|
| `connecting` | info | host, port, via (`direct`, `proxy`, `jump`), proxy |
| `connected` | info | address actually reached (the first connection only: FTP's data connections are not told) |
| `host_key_verified` | info | keyType, fingerprint (`tls-certificate` for FTPS) |
| `host_key_unknown` | warn | keyType, fingerprint |
| `host_key_changed` | error | keyType, fingerprint |
| `plain_text` | warn | none; FTP with optional encryption went on without TLS |
| `authenticated` | info | method (the logon type), username |
| `listing` / `listed` | info | path, and count once listed |
| `failed` | error | path, code (the stable error code the API answers) |

Steps of a jump host carry `hop: jump`. The connector records the connection steps and the listing endpoint records listing, its result and failures, including connection failures, since listing is what opens a connection.

**Nothing secret, ever.** Parameters are chosen fields: host names, ports, addresses, fingerprints, user names, logon types, paths, counts and codes. There is no free-text field that could carry an error message with a secret in it. A test connects with passwords, keys and proxies and checks that no entry holds any secret.

**The server keeps nothing.** Entries go to the clients connected now and are not stored or replayed. The client keeps the last 500 lines per site in memory, shows them in a Log panel above the explorer that follows the site being browsed, and lets the user copy or clear them.

## Consequences

- Positive: failures come with the steps that led to them; FTP without TLS is visible; the log can be copied into a support request.
- Negative: a tab that was closed misses the steps taken meanwhile; only browsing tells listing steps (transfers tell their connection steps, not their progress, which the transfers panel shows); the log is per user, so two tabs of the same user see the same entries.
