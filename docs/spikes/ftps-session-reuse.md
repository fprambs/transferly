# Spike: FTPS With TLS Session Reuse

Program: `backend/spikes/ftpstls`. Servers: vsftpd 3.0.3 with `require_ssl_reuse=YES`, explicit (`ftps:21`) and implicit (`ftps-implicit:990`). Client: `jlaffaye/ftp` v0.2.4.

## Question

Can `jlaffaye/ftp` talk to FTPS servers that require the data connection to reuse the control connection's TLS session, or is a fork needed?

## Results

Each variant runs login, upload, listing, download and delete.

| Mode | TLS | Client session cache | Result |
|---|---|---|---|
| explicit | 1.3 | yes | ok |
| explicit | 1.3 | no | `522 SSL connection failed: session reuse required` |
| explicit | 1.2 | yes | ok |
| explicit | 1.2 | no | `522 session reuse required` |
| implicit | 1.3 | yes | ok |
| implicit | 1.3 | no | `522 session reuse required` |
| implicit | 1.2 | yes | ok |
| explicit | 1.3 | yes, empty `ServerName` (host given as an IP) | `522 session reuse required` |

## Findings

- No fork is needed: setting `tls.Config.ClientSessionCache` makes the data connection resume the control session.
- The cache key is the `ServerName`. With an empty `ServerName` Go keys the session by remote address, which differs between control and data connections, so reuse fails. Always set `ServerName`, even when connecting to an IP.
- `jlaffaye/ftp` requires closing the `Retr` response before issuing the next command; leaving it open blocks the control connection until the server times out.
- The client has no built-in per-operation timeout on data connections; wrap operations with deadlines.

## Decisions

- The FTPS adapter always builds a `tls.Config` with a shared `ClientSessionCache` and a non-empty `ServerName`.
- Every adapter operation runs under a context deadline.
- Add a conformance test that connects to the reuse-requiring servers of the test environment.
