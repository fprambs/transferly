# ADR-0027: SSH tools on SFTP sites

- Status: accepted
- Date: 2026-10-04

## Context

An SFTP site already holds an SSH connection, but Transferly uses only its SFTP subsystem. SFTP v3 has no copy, no checksum,
no archive and no disk usage. So today, inside one server:

- a copy downloads every byte to Transferly and uploads it back;
- a checksum reads the whole file through Transferly;
- compressing or extracting means downloading, working on it elsewhere and uploading again.

Most SFTP servers also let the account run commands over the same connection (an `exec` channel), as `ssh user@host command`
does. Running `cp`, `sha256sum` or `tar` there does the work where the data lives, without moving it. This is what users of
WinSCP and Termius rely on, and what a web client can offer with auditing on top.

Not every server allows it. Accounts chrooted to `internal-sftp`, managed SFTP gateways and appliances refuse `exec`, or have no
shell or tools. Running commands also widens what a site can do, so it must stay under control: by role, per site, and in the
audit log.

## Decision

The work is split in three steps, delivered in order:

1. **Operations on the server.**
2. **Sudo mode.**
3. **An integrated terminal.**

This ADR decides the first step and the rules all three share. The sudo mode and the terminal get their own sections when they
are built.

### Operations on the server (5.7a)

A new optional port interface, `remotefs.Commands`, that an adapter may implement:

- `ServerCopy(ctx, from, to, overwrite)`: copies a file on the server, replacing what is at the target only when asked;
- `ServerChecksum(ctx, path, algorithm)`: MD5, SHA-1, SHA-256 or SHA-512 of a file, computed on the server;
- `ServerArchive(ctx, dir, names, target, format)` and `ServerExtract(ctx, archive, format, dir)`: ZIP or tar.gz on the server;
- `ServerUsage(ctx, path)`: the bytes a folder takes, as `du` measures them;
- `Tools(ctx)`: what the server offers, from the probe.

The methods that do the work are prefixed with `Server`: `ServerCopy`, `ServerChecksum`, `ServerArchive`, `ServerExtract` and `ServerUsage`.

The SFTP adapter implements it over `exec` channels of the connection it already has. Callers keep their current path when the
interface is missing or a command is refused:

- copies within one SFTP site use `ServerCopy` file by file, keeping the folder walk and the policies for existing files, and stream
  through Transferly when it is unsupported; a verified copy still streams, since verifying needs the bytes;
- the checksum card uses `Checksum`, and reads the file as today when it fails;
- compress and extract appear only when the server offers them;
- folder sizes are shown only when the server offers them.

**Probing.** On first use, a connection runs one small probe to learn whether `exec` works and which tools exist: `cp`, `mv`,
`sha256sum` and the other checksum tools, `tar`, `zip` and `unzip`, and `du`. The result is kept for the life of the
connection. A server that refuses `exec` is never asked again.

**Commands are built, never interpolated.** Every path goes through one quoting function, which wraps it in single quotes and
escapes the single quotes inside it. Every path argument is absolute or starts with `./`, so no name can be read as an option,
with or without `--`. Nothing a user types reaches a command except as a quoted path. The tests cover hostile names:
quotes, spaces, `$()`, backticks, newlines, `;`, globs and leading dashes. They run against a real OpenSSH server with bash,
coreutils, tar, zip and unzip.

Commands assume a POSIX shell (sh, bash, dash, zsh), as Linux servers have. The probe runs under `sh -c` and ends with a marker,
so an account with another shell, or one forced to `internal-sftp`, simply offers nothing. Info-ZIP's `unzip` writes control
characters of names as `^J` and the like, so a name with a newline comes back renamed from a ZIP archive.

**Outcomes.** A command that exits with a status other than zero fails with its standard error, mapped to the errors of the port
where it can be: not found, permission denied, exists, no space. Output is read with a cap, and every command is bounded by its
context and a timeout.

### Rules shared by the three steps

- **Roles.** The operations on the server need what the matching file operations need: `editor` to copy, compress and extract,
  and `viewer` for checksums and sizes. The sudo mode and the terminal will need `admin` of the workspace and an opt-in on the
  site, off by default.
- **Instance.** An administrator can turn SSH commands off for the whole instance in Settings → Administration → Limits
  (`serverCommands` of the limits, on by default). Sites then behave as plain SFTP.
- **Activity.** Compressions and extractions are recorded in the activity log of the person, like the other file operations
  (kinds `compress` and `extract`). The terminal will record its opening and closing, but not what is typed.
- **Network guard.** No new connection is opened. Commands run over the SSH connection that already passed the guard and the
  check of the server key.

## Consequences

- Positive:
  - copies, checksums, archives and folder sizes inside one SFTP server take seconds, without moving data through Transferly
    or the browser;
  - servers that refuse commands keep working as before;
  - the same `exec` channel will carry the sudo mode and the terminal.
- Negative:
  - commands depend on the tools of the server: GNU and BSD versions differ, and the probe and the tests must cover both;
  - building shell commands is a classic injection risk, so quoting is centralized, reviewed and tested with hostile names;
  - a server-side copy is not resumable and shows no byte progress, only that it is running.
