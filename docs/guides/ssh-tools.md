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

## Sudo mode

`sudo -S -p 'transferly-sudo-password:' -u 'USER' -- sh -c 'for p in …sftp-server paths…; do if [ -x "$p" ]; then echo transferly-sudo-ready >&2; exec "$p"; fi; done; …'`
runs over an `exec` channel, and `sftp.NewClientPipe` speaks SFTP over its pipes once `transferly-sudo-ready` shows on standard
error. Sudo prints the prompt without a line break, so standard error is scanned as it comes.

| sudo does | Transferly does |
|---|---|
| asks once | writes the sudo password of the site, or the password of the account, and a line break |
| asks again | closes the channel: `sudo_refused` |
| asks with no password to give | closes the channel: `sudo_refused` |
| ends (`not in the sudoers file`, no `sftp-server`) | `sudo_refused` with what it wrote |
| says nothing within the timeout | `remote_unavailable` |

## Terminal

`FS.Shell(ctx, dir, cols, rows)` checks the probe found a shell, opens a session, asks for a PTY `xterm-256color` and starts
`cd 'DIR' 2>/dev/null; exec "${SHELL:-/bin/sh}" -l`. The `remotefs.Terminal` it returns reads and writes the PTY, resizes it with
`window-change`, and `Wait` gives the exit status (-1 when the shell ended without one, for example on a signal).

`files.Service.Terminal` needs the action `elevate`. `httpapi/terminal.go` checks the site has `terminal` on, opens the shell
before upgrading the request, so failures are plain problem responses, and relays it:

| Message | Direction | Meaning |
| --- | --- | --- |
| binary | browser → server | typed bytes |
| binary | server → browser | what the shell prints |
| text `{"type":"resize","cols":120,"rows":40}` | browser → server | resize; malformed or out of bounds is ignored |
| text `{"type":"exit","code":0}` | server → browser | the shell ended; a normal closure follows |

When the browser goes away the shell is closed. The activity entry has kind `terminal`, the folder and the duration.

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
- **Terminal** appears in the explorer toolbar when the site has Allow the terminal on and the server has a shell; the panel below
  the files holds one tab per shell (`features/terminal`).
- Administrators turn the feature off in Settings → Administration → Limits.

## Tests

- `sftptest.Options.Exec` scripts the answers of `exec` requests, to check the command lines and the reading of outcomes.
- `TestQuote` runs the quoting through `sh` with hostile values.
- With `TRANSFERLY_TEST_SSH_ADDR` set to the `ssh` service of the test stacks, `TestCommands_OpenSSH` runs every operation with
  hostile names against OpenSSH with bash, coreutils, tar, zip and unzip. With `TRANSFERLY_TEST_SFTP_ADDR`, `TestCommands_SFTPOnly`
  checks that a server forced to `internal-sftp` offers nothing.
- `sftptest.Options.Sudo` plays sudo (password, no password, refusal, silence, garbage) and serves SFTP on the channel.
- `TestSudo_OpenSSH` reads `/root/only-root.txt` against the `ssh` service as `transferly` (sudo with its password) and `deployer`
  (sudo without one), and checks wrong and missing passwords.
- `e2e/specs/ssh-tools.spec.ts` compresses, extracts and measures on `ssh`, checks that `sftp` offers none of it, and lists `/root`
  in sudo mode.
- `sftptest.Options.Shell` plays a PTY shell; `TestShell_OpenSSH` opens a real one on `ssh` and checks the folder, the resize and
  the exit status. `TestTerminal` drives the WebSocket end to end against a fake shell.
- The e2e spec also opens a terminal on `ssh`, runs a command in `/home/transferly` and waits for the exit status.
