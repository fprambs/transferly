# Test Environment

Remote servers for integration and end-to-end tests. Nothing is published to the host.

Start: `docker compose -f compose.dev.yaml --profile testenv up -d`

| Service | Address in the Compose network | Notes |
|---|---|---|
| `sftp` | `sftp:22` | `atmoz/sftp`; user `transferly` with upload directory `/upload`, and `anonymous` let in with the SSH "none" method (`testenv/sftp/anonymous.sh`), serving `/pub/readme.txt`; `/preview-samples` is `testenv/preview-samples`, mounted read-only, with a sample of every format the file viewers show |
| `ftp` | `ftp:21` | plain FTP; `TZ=America/Santiago` so `LIST` shows local time without a zone |
| `ftps` | `ftps:21` | explicit FTPS (AUTH TLS), TLS session reuse required |
| `ftps-implicit` | `ftps-implicit:990` | implicit FTPS, TLS session reuse required |
| `toxiproxy` | `toxiproxy:2222` (sftp), `2121` (ftp), `2122` (ftps), `2990` (ftps-implicit); API on `8474` | fault injection |

Credentials for every server: `transferly` / `transferly`. The FTPS certificate is self-signed.

## Test Data

`make seed` (or `scripts/seed-testenv.sh`) creates 50,000 small files in `seed/many` and files with unusual names (spaces, accents, CJK, emoji, apostrophes, tabs) in `seed/names`, on both the SFTP and FTP servers. Add `--large` for 1 GiB and 5 GiB files.

## Known Limits

- vsftpd 3.0.3 does not implement `MLSD`, so these servers exercise the `LIST` path (and `MDTM`, which vsftpd returns in local time with `use_localtime=YES`). A server with `MLSD` is still needed.
- FTP data connections use passive ports directly and bypass Toxiproxy; only the control channel is proxied. SFTP is fully proxied.
- vsftpd crashes as PID 1 when clients disconnect abruptly; the services run with `init: true`.
- Package downloads in the vsftpd image use HTTPS because plain HTTP mirrors returned altered files on the development network.
