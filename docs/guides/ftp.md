# FTP and FTPS: jlaffaye/ftp and ftpserverlib

Sources: [jlaffaye/ftp](https://github.com/jlaffaye/ftp) v0.2.4 (`go doc github.com/jlaffaye/ftp`) and the spikes in [ftps-session-reuse](../spikes/ftps-session-reuse.md) and [ftp-list-timezone](../spikes/ftp-list-timezone.md). Test server: [fclairamb/ftpserverlib](https://github.com/fclairamb/ftpserverlib) v0.32.4 over an `afero` in-memory file system. Adapter: `backend/internal/adapters/remotefs/ftpfs`.

## How the client library behaves

- One `ServerConn` is one control connection that carries one command at a time. It is not safe for concurrent use.
- Passive mode only: EPSV first, then PASV. With `DialWithTrustPasvIP(false)`, the default, **the address a server announces is ignored** and data connections go to the address of the control connection. Never turn `trustPasvIP` on: it is the FTP passive hijack (a server can send the client to a third party).
- `Retr` and `List` hold the control connection until the response is closed; issue no other command before `Close`. `Response.Close` reads the final reply (226): a transfer the server cut short is only detected there, so a read that ended with `io.EOF` is complete only when `Close` returned nil.
- `MLSD` and `MLST` are used only when the server advertises `MLST` in `FEAT`: exact UTC times to the second, and `GetEntry` works. Without them `LIST` is parsed with the location of `DialWithLocation` and has minute precision (dates only for old files).
- `List` of a missing path on a server with only `LIST` may answer with nothing, and `List` of a file answers with the file itself. The adapter checks both.
- `StorFrom` truncates on some servers; `Append` (APPE) is universal. The adapter therefore resumes only at the end of the file (`Capabilities.Rewrite` is false).
- `DialWithDialFunc` replaces the dialer for the control and the data connections and then **the library no longer wraps TLS**. The adapter wraps every connection itself, with `tls.Client` and no explicit handshake: the library opens a data connection before sending the command that makes the server start its side of the handshake, so an early handshake blocks.

## Rules of the adapter

- Every connection, control and data, comes from the dialer of the configuration (the network guard's dialer in production).
- FTPS: one `tls.Config` per session with a `ClientSessionCache` and a `ServerName` that is never empty (with an IP as host the reuse fails). Certificates are not checked against authorities; the SHA-256 fingerprint goes to `VerifyHostKey` under the key type `tls-certificate` and is trusted like an SSH host key. Session reuse is required by real servers (vsftpd `require_ssl_reuse=YES`); the conformance test against vsftpd covers it. FTPS is capped at TLS 1.2: vsftpd 3.0.3 with `require_ssl_reuse=YES` fails every upload of more than one TLS record (16 KB) under TLS 1.3 with `426 Failure reading network stream`, and uploads of 1 KB work, which hides it in small tests. Downloads and listings work either way.
- Paths with `\r`, `\n` or NUL, and relative paths, are refused before anything is sent: a line break would let a name inject a second command.
- Operations take turns (a channel, so waiting honors the context). A transfer stream keeps its turn until it is closed; callers must close streams. Canceling a context cuts the connections, because an FTP command cannot be interrupted any other way, and the session is unusable afterwards.
- Reply classification: 421, 425, 426 lost connection; 530, 532 authentication; 452, 552 quota; 450, 451, 550, 553 refined by the text of the reply (missing, exists, not empty, not a directory) and by looking at the path, otherwise a permission problem.

## Tests

- `ftptest` starts a server on a free port: plain, explicit or implicit FTPS, with or without MLSD and MFMT, announcing another public host, with faults injected by operation and path, and a `Stop` that cuts every connection. Its file system is made strict, since the in-memory one creates missing parents.
- The conformance suite (`remotefstest`) runs against all of them, and opt-in against real vsftpd servers (`TestConformance_Vsftpd`, variables `SENTINEL_TEST_FTP_ADDR`, `SENTINEL_TEST_FTPS_ADDR`, `SENTINEL_TEST_FTPS_IMPLICIT_ADDR`) of the `ftp` profile of `compose.test.yaml`:

```
docker compose -f compose.test.yaml --profile ftp up -d --wait ftp ftps ftps-implicit
docker compose -f compose.test.yaml --profile ftp --profile unit run --rm unit sh -c 'cd backend && \
  SENTINEL_TEST_FTP_ADDR=ftp:21 SENTINEL_TEST_FTPS_ADDR=ftps:21 SENTINEL_TEST_FTPS_IMPLICIT_ADDR=ftps-implicit:990 \
  go test ./internal/adapters/remotefs/ftpfs/ -run Vsftpd -v'
```
