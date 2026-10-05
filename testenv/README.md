# Test Environment

Test servers for every connector, to try each feature by hand on the development stack and for integration tests. Only the sign-in page of the fake of Google is published to the host (`127.0.0.1:8090`), because the browser opens it. The isolated end-to-end stack (`compose.test.yaml`) runs its own copies of these servers.

Start: `make testenv` (`docker compose -f compose.dev.yaml --profile testenv up -d --build`), next to `make up`.

| Service | Address in the Compose network | Notes |
|---|---|---|
| `sftp` | `sftp:22` | `atmoz/sftp`; user `transferly` with upload directory `/upload`, and `anonymous` let in with the SSH "none" method (`testenv/sftp/anonymous.sh`), serving `/pub/readme.txt`; `/preview-samples` is `testenv/preview-samples`, mounted read-only, with a sample of every format the file viewers show |
| `ssh` | `ssh:22` | OpenSSH on Debian with bash, coreutils, tar, zip, unzip and sudo (`testenv/ssh`); `transferly` / `transferly` may run commands, so its SFTP sites copy, compress, extract, sum and measure on the server, open a terminal, and may use sudo with its password; `deployer` / `deployer` may use sudo without one; `/root/only-root.txt` only root may read; the home folder holds a copy of `preview-samples` |
| `strict-sftp` | `strict-sftp:22` | an SFTP server that reports every file 300 bytes larger than it is, as some enterprise gateways do (`cmd/strictsftp`); `transferly` / `transferly`; holds `export.dat` (1.1 MB), `bundle.zip` (a ZIP of 1 MB) and `notes.txt` |
| `ftp` | `ftp:21` | plain FTP; `TZ=America/Santiago` so `LIST` shows local time without a zone |
| `ftps` | `ftps:21` | explicit FTPS (AUTH TLS), TLS session reuse required |
| `ftps-implicit` | `ftps-implicit:990` | implicit FTPS, TLS session reuse required |
| `s3` | `s3:7070` | Versity S3 Gateway over plain HTTP; access key `transferly`, secret `transferly-secret`, bucket `transferly` with a copy of `preview-samples` |
| `webdav` | `webdav:80` | Apache httpd with `mod_dav` over plain HTTP (`testenv/webdav`); `/webdav/` with `transferly` / `transferly`, and `/public/`, an anonymous read-only share; both hold a copy of `preview-samples` |
| `azurite` | `azurite:10000` | the official emulator of Azure Blob Storage, with its well-known account `devstoreaccount1`; `fakeazure` creates the container `transferly` with a copy of `preview-samples` |
| `fakeazure` | `fakeazure:10002` | a fake of Azure Files in memory: account `transferly`, share `share` with `Reports/q3.csv` and a copy of `preview-samples` |
| `fakegoogle` | `fakegoogle:8090`, and `127.0.0.1:8090` on the host | a fake of Google in memory: its sign-in page agrees at once, and My Drive holds a Google document, `Plan`, and a folder, `Shared notes`; the development backend uses it unless a real OAuth client is configured |
| `toxiproxy` | `toxiproxy:2222` (sftp), `2121` (ftp), `2122` (ftps), `2990` (ftps-implicit); API on `8474` | fault injection |

Credentials for the SFTP, SSH, FTP and WebDAV servers: `transferly` / `transferly`. The FTPS certificate is self-signed. Everything but the SFTP and FTP servers lives in memory and starts anew when its container restarts.

## Trying each connector

S3, Google, Azure and WebDAV are in beta: offer them first in Settings → Administration → Connectors. Then create a site with **New site**:

| Connector | Settings |
|---|---|
| SFTP | Host `sftp`, port 22, username `transferly`, password `transferly` |
| SFTP with SSH tools | Host `ssh`, port 22, username `transferly`, password `transferly`; it opens in `/home/transferly`; trust its key when asked |
| SFTP with a terminal | As SFTP with SSH tools, with Allow the terminal on in the Advanced tab; Terminal in the toolbar opens a shell in the folder in view |
| SFTP in sudo mode | As above, with Use sudo on in the Advanced tab and default remote folder `/root`; the sudo password may stay empty, since it is the password of the account |
| SFTP with inexact sizes | Host `strict-sftp`, port 22, username `transferly`, password `transferly`; download `export.dat` and preview `bundle.zip`; trust its key when asked, again after each restart of the container |
| FTP / FTPS | Host `ftp` with plain FTP, or `ftps` with explicit TLS, or `ftps-implicit` with implicit TLS on port 990; username `transferly`, password `transferly`; trust the certificate when asked |
| S3 | Endpoint `s3`, HTTPS off, port 7070, bucket `transferly`, access key ID `transferly`, secret access key `transferly-secret` |
| Google Drive | Connect with Google: the page of the fake agrees at once |
| Azure Blob Storage | Storage account `devstoreaccount1`, container `transferly`, account key `Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==`; on the Advanced tab, HTTPS off, endpoint `azurite`, port 10000 |
| Azure Files | Storage account `transferly`, share `share`, account key `dHJhbnNmZXJseS1henVyZS10ZXN0LWFjY291bnQta2V5`; on the Advanced tab, HTTPS off, endpoint `fakeazure`, port 10002 |
| WebDAV | Server `webdav`, HTTPS off, port 80, path `/webdav/`, username `transferly`, password `transferly`; or path `/public/` with the anonymous logon |

To use a real Google OAuth client on the development stack instead of the fake, set `TRANSFERLY_GOOGLE_CLIENT_ID`, `TRANSFERLY_GOOGLE_CLIENT_SECRET` and the three endpoints `TRANSFERLY_GOOGLE_AUTH_URL`, `TRANSFERLY_GOOGLE_TOKEN_URL` and `TRANSFERLY_GOOGLE_API_URL` to Google's ([guide](../docs/guides/google.md)).

## Test Data

`make seed` (or `scripts/seed-testenv.sh`) creates 50,000 small files in `seed/many` and files with unusual names (spaces, accents, CJK, emoji, apostrophes, tabs) in `seed/names`, on both the SFTP and FTP servers. Add `--large` for 1 GiB and 5 GiB files.

## Known Limits

- vsftpd 3.0.3 does not implement `MLSD`, so these servers exercise the `LIST` path (and `MDTM`, which vsftpd returns in local time with `use_localtime=YES`). A server with `MLSD` is still needed.
- FTP data connections use passive ports directly and bypass Toxiproxy; only the control channel is proxied. SFTP is fully proxied.
- vsftpd crashes as PID 1 when clients disconnect abruptly; the services run with `init: true`.
- Package downloads in the vsftpd image use HTTPS because plain HTTP mirrors returned altered files on the development network.
- The WebDAV server runs on the Debian image of Apache: the Alpine image lacks the DBM driver `mod_dav_fs` needs for its lock database.
