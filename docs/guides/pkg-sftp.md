# pkg/sftp v1.13.11

Transferly builds a patched copy in `backend/third_party/sftp` (see its PATCHES.md): upstream trusts the length a server gives in its replies to reads, so a malicious server could make it panic inside its own goroutines and bring the process down.

Source: [pkg.go.dev/github.com/pkg/sftp](https://pkg.go.dev/github.com/pkg/sftp).

Create clients with `sftp.NewClient(sshConn, opts...)`.

| Option | Upstream note | Decision |
|---|---|---|
| `UseConcurrentReads(true)` | generally safe; omitting it degrades performance | on, except for servers that proved strict (see below) |
| `UseConcurrentWrites(true)` | errors mid-write can leave holes in the file | on; on error discard the target, or write `.part` and rename |
| `MaxConcurrentRequestsPerFile(n)` | default 64 | 64, per-site override |
| `MaxPacketChecked(n)` | up to 32768 bytes | 32768 |
| `MaxPacketUnchecked(n)` | larger sizes for specific servers | only for tested profiles |

## Practices

- Prefer `File.WriteTo` and `File.ReadFrom` over read or write loops.
- `Close()` ends the session; `Wait()` blocks until the connection ends and drives reconnection.
- A client is safe for concurrent use.

## Tests

`remotefstest` conformance against the `testenv` server; drops simulated with toxiproxy.

## SSH (golang.org/x/crypto/ssh)

Source: [pkg.go.dev/golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh).

- `ClientConfig.HostKeyCallback` is mandatory and never `InsecureIgnoreHostKey`. The adapter calls `Config.VerifyHostKey(keyType, fingerprint)` and refuses to connect without it.
- `ssh.FingerprintSHA256(key)` renders the `SHA256:` fingerprint that Transferly stores and shows.
- `ClientConfig.HostKeyAlgorithms` restricts the accepted key types to the trusted ones, so a server cannot switch to a type that was never trusted.
- `Config.Ciphers` follows the order OpenSSH prefers, ChaCha20-Poly1305 and CTR before AES-GCM. The library puts AES-GCM first, and some enterprise servers that pick it then close the connection during authentication.
- Password authentication is offered through `ssh.Password` and `ssh.KeyboardInteractive`, because many servers only accept the latter.
- Keys are read with `ssh.ParsePrivateKey` or `ParsePrivateKeyWithPassphrase`; `*ssh.PassphraseMissingError` and `x509.IncorrectPasswordError` are reported as authentication errors.
- `NewClientConn` blocks on the handshake: the adapter closes the connection when the context ends (`context.AfterFunc`).

## Server statuses

SFTP version 3 has few status codes, so servers answer one generic failure for "already exists", "directory not empty" or "invalid rename". OpenSSH also answers "no such file" when a component of a path is a file. After such an answer the adapter looks at the path (`state`) to return the right sentinel. Only failures are probed, so the normal path costs nothing.

Server errors reach the client as `*sftp.StatusError`; `normaliseError` turns "no such file" into `os.ErrNotExist` and "permission denied" into `os.ErrPermission`. An error that is not a status is a transport failure and becomes `remotefs.ErrConnectionLost`.

## Tests

- `sftptest` runs an SFTP server in the test process over a temporary directory, answers like OpenSSH, and injects faults, so the unit tests need no Docker.
- The same conformance suite runs against the real OpenSSH of the `testenv` profile when `TRANSFERLY_TEST_SFTP_ADDR` is set:

```
docker compose -f compose.dev.yaml --profile testenv up -d sftp
docker compose -f compose.dev.yaml run --rm -e TRANSFERLY_TEST_SFTP_ADDR=sftp:22 tools sh -c 'cd backend && go test -run OpenSSH ./internal/adapters/remotefs/sftpfs/'
```

- The in-process server is not a security boundary and only follows the parts of the protocol the adapter uses. Anything that differs on a real server is a bug of `sftptest`: fix it there and keep the real-server run green.

## Download speed (4.13)

`File.Read` sends one request per call, so a copy through `Read` moves one 32 KB chunk per round trip: over a link with 20 ms of latency that caps a download at about 1.6 MB/s whatever the bandwidth. `File.WriteTo` keeps several requests in flight (`UseConcurrentReads`, `MaxConcurrentRequestsPerFile`) and is what makes SFTP fast. Every wrapper between the file and the HTTP response must therefore keep `WriteTo`, or `io.Copy` falls back to `Read`: `io.LimitReader` does not have it, which is how downloads ended up twenty times slower than other clients.

- `files.body` hands the destination to the file when the stream runs to the end of the file, reporting progress, bandwidth caps and cancellation on each chunk written; a range that stops earlier copies chunk by chunk.
- The transfer tracker's download wrapper keeps `WriteTo` too, and tells a failing destination (a browser that left, which ends the transfer as interrupted) from a failing file (failed, with its code).
- `TestRemoteFiles_DownloadReadsAhead` fails if a download over HTTP stops reaching the file's `WriteTo`.

Measured with `scripts/bench-sftp-download.sh` against the isolated test stack, 64 MiB, 2026-09-29:

| | local network | 20 ms added |
|---|---|---|
| OpenSSH `sftp` | 33–55 MB/s | 28–31 MB/s |
| Transferly before | 20–27 MB/s | 1.5 MB/s |
| Transferly after | — | 39–57 MB/s |

## Strict servers

Some enterprise SFTP gateways report sizes that do not match the bytes they send (a file listed as 5.7 KB sends 4.2 KB), or drop the session as soon as a client keeps several reads in flight. Announcing the reported size as `Content-Length` then leaves the browser waiting for bytes that never come, and a ZIP index is looked for where the file does not end. `File.WriteTo` also assumes every read of a regular file returns a full chunk, which such servers do not promise.

- `sftpfs.StrictServers` remembers, by address, the servers that proved strict. The connector keeps one for every connection, because a dropped session is discarded with what it learned, and keeps it in the table `strict_servers` through its `StrictStore`, read once on first use, so it outlives restarts. A site can also say so itself with **Sizes do not match the bytes** (`inexact_sizes`) on its Advanced tab, which makes every connection to it strict from the start. A server proves strict when a whole file comes with another size than it reported (`remotefs.SizeChecker.NoteWholeRead`), or when a pipelined read loses the connection in its first MiB. Later failures are the link's, not the server's, and do not count. The memory forgets everything once it holds 1024 servers.
- A connection to a strict server reads one request at a time (`UseConcurrentReads(false)`, and `Read` for a connection that learned it while open) and reports `SizesTrusted() == false`.
- `files.Download` reads a whole file of up to 512 KiB into memory, up to its end, before answering. It announces the bytes that came and gives the connection back at once, on every server. Reading it whole is also how a connection learns whether its sizes hold. A larger whole file streams with the reported size as `Content-Length` while the sizes are trusted, and to its end with no `Content-Length` once they are not; the transfer then ends at the end of the stream. Ranges always use the reported size.
- The archive viewer reads an archive of up to 512 KiB, or any archive on a strict server, whole into memory up to its end (capped like nested archives, 64 MiB), so a ZIP or 7z index is found where the bytes end.
- A download whose every expected byte moved ends as done even if the read failed afterwards, as when a server drops the session once the file was sent.
- A strict server is read one request at a time, so a link with a long round trip makes it slow: at 32 KiB per request and 250 ms per round trip, about 130 KB/s. Its files are therefore read through a second SFTP channel of the same connection, opened on first use, whose requests ask for 255 KiB, the most a reply of pkg/sftp can carry; the server sends what it allows per reply and the client asks for the rest. Writes keep the session's 32 KiB packets, which every server takes. A server that refuses the second channel is read through the session; sudo mode never opens it. `TestStrictBulkReads_OpenSSH` measures it against OpenSSH through a link that adds 40 ms to every message: 1.1 MB in about 1.1 s instead of 2.3 s.
- A whole file larger than 512 KiB, on a server not known as strict, is checked at its end before it is announced: the last byte its reported size names is read, and must be the only one left. A file that ends sooner or goes on marks the server strict and streams to its real end with no `Content-Length`. The archive viewer does the same check before it reads an archive by ranges. So even the first large download from a strict server comes whole, at the cost of one extra open of the file.
- `cmd/strictsftp` is such a server for the test stacks (`strict-sftp`, sizes 300 bytes larger): `e2e/specs/strict-sizes.spec.ts` downloads a file of 1.1 MB from it in the browser and lists a ZIP of 1 MB, and `TestStrictGateway_EndToEnd` checks the same against `sftptest.Options.SizeOff`, plus a restart over the same store.
