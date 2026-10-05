# Transferly patches to pkg/sftp

This is pkg/sftp v1.13.11 (BSD 2-Clause license, see LICENSE), used through a `replace` directive in `backend/go.mod`. Only the
library sources and its internal packages are kept; its tests, examples, fuzz harness and the in-memory example handler stay
upstream.

## Lengths given by the server (1d)

A reply to a read carries a length field written by the server. Upstream slices the reply with it unchecked (`data[:l]`) in
`File.readChunkAt` and in the worker goroutines of `File.ReadAt` and `File.WriteTo`. A malicious server that claims more bytes than it
sends makes the client panic; in the worker goroutines the panic cannot be recovered and brings the whole process down.

- The three places check the length against the bytes of the reply, and the buffer of `WriteTo` against its capacity, and fail the
  read with `errDataLength` instead.
- `unmarshalUint32` and `unmarshalString`, which every reply goes through, give zero values instead of panicking on a packet too short
  for what they read.

`TestFS_LyingServer` in `internal/adapters/remotefs/sftpfs` reads from a server that rewrites the length of every reply
(`sftptest.Options.LyingReads`), pipelined, strict, at an offset and in parallel; before these patches it crashed the test binary.
Drop this copy when upstream checks these lengths.
