# pkg/sftp v1.13.11

Source: [pkg.go.dev/github.com/pkg/sftp](https://pkg.go.dev/github.com/pkg/sftp).

Create clients with `sftp.NewClient(sshConn, opts...)`.

| Option | Upstream note | Decision |
|---|---|---|
| `UseConcurrentReads(true)` | generally safe; omitting it degrades performance | on |
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
- `ssh.FingerprintSHA256(key)` renders the `SHA256:` fingerprint that Sentinel stores and shows.
- `ClientConfig.HostKeyAlgorithms` restricts the accepted key types to the trusted ones, so a server cannot switch to a type that was never trusted.
- Password authentication is offered through `ssh.Password` and `ssh.KeyboardInteractive`, because many servers only accept the latter.
- Keys are read with `ssh.ParsePrivateKey` or `ParsePrivateKeyWithPassphrase`; `*ssh.PassphraseMissingError` and `x509.IncorrectPasswordError` are reported as authentication errors.
- `NewClientConn` blocks on the handshake: the adapter closes the connection when the context ends (`context.AfterFunc`).

## Server statuses

SFTP version 3 has few status codes, so servers answer one generic failure for "already exists", "directory not empty" or "invalid rename". OpenSSH also answers "no such file" when a component of a path is a file. After such an answer the adapter looks at the path (`state`) to return the right sentinel. Only failures are probed, so the normal path costs nothing.

Server errors reach the client as `*sftp.StatusError`; `normaliseError` turns "no such file" into `os.ErrNotExist` and "permission denied" into `os.ErrPermission`. An error that is not a status is a transport failure and becomes `remotefs.ErrConnectionLost`.

## Tests

- `sftptest` runs an SFTP server in the test process over a temporary directory, answers like OpenSSH, and injects faults, so the unit tests need no Docker.
- The same conformance suite runs against the real OpenSSH of the `testenv` profile when `SENTINEL_TEST_SFTP_ADDR` is set:

```
docker compose -f compose.dev.yaml --profile testenv up -d sftp
docker compose -f compose.dev.yaml run --rm -e SENTINEL_TEST_SFTP_ADDR=sftp:22 tools sh -c 'cd backend && go test -run OpenSSH ./internal/adapters/remotefs/sftpfs/'
```

- The in-process server is not a security boundary and only follows the parts of the protocol the adapter uses. Anything that differs on a real server is a bug of `sftptest`: fix it there and keep the real-server run green.
