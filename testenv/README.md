# Test Environment

Remote servers for integration and end-to-end tests. Nothing is published to the host.

Start: `docker compose -f compose.dev.yaml --profile testenv up -d`

| Service | Address in the Compose network | Notes |
|---|---|---|
| `sftp` | `sftp:22` | `atmoz/sftp`; upload directory `/upload` |
| `ftp` | `ftp:21` | plain FTP; `TZ=America/Santiago` so `LIST` shows local time without a zone |
| `ftps` | `ftps:21` | explicit FTPS (AUTH TLS), TLS session reuse required |
| `ftps-implicit` | `ftps-implicit:990` | implicit FTPS, TLS session reuse required |
| `toxiproxy` | `toxiproxy:2222` (sftp), `2121` (ftp), `2122` (ftps), `2990` (ftps-implicit); API on `8474` | fault injection |

Credentials for every server: `sentinel` / `sentinel`. The FTPS certificate is self-signed.

## Known Limits

- vsftpd 3.0.3 does not implement `MLSD`, so these servers exercise the `LIST` path (and `MDTM`, which vsftpd returns in local time with `use_localtime=YES`). A server with `MLSD` is still needed.
- FTP data connections use passive ports directly and bypass Toxiproxy; only the control channel is proxied. SFTP is fully proxied.
- vsftpd crashes as PID 1 when clients disconnect abruptly; the services run with `init: true`.
- Package downloads in the vsftpd image use HTTPS because plain HTTP mirrors returned altered files on the development network.
