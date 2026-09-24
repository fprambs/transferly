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
