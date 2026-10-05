# SSH tools of SFTP sites

Sources: [RFC 4254, SSH connection protocol](https://www.rfc-editor.org/rfc/rfc4254) (session channels, `exec`, `exit-status`),
[golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh) (`Session.Run`, `Session.Signal`), [POSIX shell quoting](https://pubs.opengroup.org/onlinepubs/9799919799/utilities/V3_chap02.html#tag_19_02),
[GNU tar](https://www.gnu.org/software/tar/manual/), [Info-ZIP zip and unzip](https://infozip.sourceforge.net/). Decision:
[ADR-0027](../adr/0027-ssh-tools.md).

## What runs on the server

| Operation | Command | Notes |
|---|---|---|
| Probe | `sh -c 'for t in cp tar gzip zip unzip du md5sum sha1sum sha256sum sha512sum; do command -v "$t" …; done; echo transferly-probe'` | once per connection, 10 s at most |
| Copy | `cp -p SRC DST`, after a check that fails as `File exists` unless it may overwrite | modes and times kept |
| Checksum | `sha256sum PATH` (or md5sum, sha1sum, sha512sum) | a leading `\` of GNU output is dropped |
| Usage | `du -sk PATH` | kibibytes on the disk, times 1024 |
| Compress | `cd DIR && zip -q -r -y TARGET ./NAME…` or `tar -czf TARGET -C DIR ./NAME…` | refuses an existing target; zip keeps links as links |
| Extract | `unzip -q -n ARCHIVE -d DIR` or `tar -xzkf ARCHIVE -C DIR` | never replaces files; unzip exit 1 and GNU tar's kept files count as success |

Every path is quoted by `quote` (single quotes, `'` written as `'\''`) and is absolute or starts with `./`.

## Outcomes

A command that exits with a status other than zero is a `*sftpfs.CommandError`. Its standard error, read up to 4 KiB, decides the
error of the port:

- `No such file` is not found;
- `Permission denied`, `Operation not permitted` and `Read-only file system` are permission errors;
- `File exists` is exists;
- `No space left` and `quota exceeded` are quota errors;
- `Not a directory` is not a directory;
- exit 127 or 126 (missing program) is `remotefs.ErrUnsupported`, and anything else is invalid.

A server that refuses the `exec` request is `remotefs.ErrUnsupported`, and callers fall back to moving the data themselves. An
ended context sends `SIGKILL` and closes the session.

## In the product

- Copies within one site and checksums use the server automatically.
- The explorer offers **Compress…** and **Extract here** in the context menu and the details panel, and **Measure** for folders,
  only when `GET …/sites/{siteId}/tools` says the server can.
- Administrators turn the feature off in Settings → Administration → Limits.

## Tests

- `sftptest.Options.Exec` scripts the answers of `exec` requests, to check the command lines and the reading of outcomes.
- `TestQuote` runs the quoting through `sh` with hostile values.
- With `TRANSFERLY_TEST_SSH_ADDR` set to the `ssh` service of the test stacks, `TestCommands_OpenSSH` runs every operation with
  hostile names against OpenSSH with bash, coreutils, tar, zip and unzip. With `TRANSFERLY_TEST_SFTP_ADDR`, `TestCommands_SFTPOnly`
  checks that a server forced to `internal-sftp` offers nothing.
- `e2e/specs/ssh-tools.spec.ts` compresses, extracts and measures on `ssh`, and checks that `sftp` offers none of it.
