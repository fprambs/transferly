package sftpfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

var _ remotefs.Commands = (*FS)(nil)

// probeTimeout bounds the probe of what a server can do.
const probeTimeout = 10 * time.Second

// Caps on what a command may write before the rest is dropped.
const (
	maxStdout = 64 << 10
	maxStderr = 4 << 10
)

// probeMarker ends the output of the probe, so a server that runs something else instead of the command is told apart.
const probeMarker = "transferly-probe"

// probedTools are the programs the probe looks for.
var probedTools = []string{"cp", "tar", "gzip", "zip", "unzip", "du", "md5sum", "sha1sum", "sha256sum", "sha512sum"}

// probeScript prints the name of each probed program the server has, then the marker. It runs under sh -c, so it works whatever
// the login shell of the account is.
var probeScript = "for t in " + strings.Join(probedTools, " ") + `; do command -v "$t" >/dev/null 2>&1 && echo "$t"; done; echo ` + probeMarker

// sums maps the checksum algorithms to the programs that compute them.
var sums = map[string]string{"md5": "md5sum", "sha1": "sha1sum", "sha256": "sha256sum", "sha512": "sha512sum"}

// hexDigest matches a digest in hexadecimal.
var hexDigest = regexp.MustCompile(`^[0-9a-fA-F]{32,128}$`)

// commands holds what the probe learned about the server of a connection, or why it could not learn it.
type commands struct {
	once  sync.Once
	tools remotefs.Tools
	err   error
}

// quote wraps s in single quotes, with the single quotes inside it escaped, so a POSIX shell reads it as one word, as it is.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// arg renders a path as one argument of a command, quoted. Paths must be absolute or start with ./, so no path can be read as
// an option, and cannot hold a NUL byte, which no command line can carry.
func arg(p string) (string, error) {
	if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "./") || strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("%w: %q cannot be an argument of a command", remotefs.ErrInvalid, p)
	}
	return quote(p), nil
}

// args renders paths as arguments, separated by spaces.
func args(paths ...string) (string, error) {
	out := make([]string, len(paths))
	for i, p := range paths {
		quoted, err := arg(p)
		if err != nil {
			return "", err
		}
		out[i] = quoted
	}
	return strings.Join(out, " "), nil
}

// capped keeps the first bytes written to it, up to a limit, and drops the rest.
type capped struct {
	buf   bytes.Buffer
	limit int
}

// Write implements io.Writer.
func (c *capped) Write(p []byte) (int, error) {
	if room := c.limit - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// CommandError is a command that ran on the server and exited with a status other than zero.
type CommandError struct {
	// Status is the exit status.
	Status int
	// Stderr is the start of what the command wrote to its standard error.
	Stderr string
}

// Error implements error.
func (e *CommandError) Error() string {
	return fmt.Sprintf("the command exited with status %d: %s", e.Status, strings.TrimSpace(e.Stderr))
}

// run executes a command on the server and returns its standard output; callers check the connection first, through Tools. A server that refuses to run commands fails with
// [remotefs.ErrUnsupported], a command that fails with a [*CommandError], and an ended context kills the command.
func (f *FS) run(ctx context.Context, command string) ([]byte, error) {
	session, err := f.ssh.NewSession()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", remotefs.ErrUnsupported, err)
	}
	defer func() { _ = session.Close() }()
	stdout, stderr := &capped{limit: maxStdout}, &capped{limit: maxStderr}
	session.Stdout, session.Stderr = stdout, stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		<-done
		return nil, ctx.Err()
	}
	var exit *ssh.ExitError
	switch {
	case errors.As(err, &exit):
		return nil, &CommandError{Status: exit.ExitStatus(), Stderr: stderr.buf.String()}
	case err != nil && f.closed.Load():
		return nil, fmt.Errorf("%w: %w", remotefs.ErrConnectionLost, err)
	case err != nil:
		return nil, fmt.Errorf("%w: %w", remotefs.ErrUnsupported, err)
	}
	return stdout.buf.Bytes(), nil
}

// commandError turns the failure of a command into the errors of the port, by what it wrote to its standard error.
func commandError(op, p string, err error) error {
	var failed *CommandError
	if !errors.As(err, &failed) {
		return fmt.Errorf("%s %q: %w", op, p, err)
	}
	text := strings.ToLower(failed.Stderr)
	kind := remotefs.ErrInvalid
	switch {
	case failed.Status == 127 || failed.Status == 126:
		kind = remotefs.ErrUnsupported
	case strings.Contains(text, "no such file"):
		kind = remotefs.ErrNotFound
	case strings.Contains(text, "permission denied") || strings.Contains(text, "operation not permitted") || strings.Contains(text, "read-only file system"):
		kind = remotefs.ErrPermission
	case strings.Contains(text, "file exists"):
		kind = remotefs.ErrExists
	case strings.Contains(text, "no space left") || strings.Contains(text, "quota exceeded"):
		kind = remotefs.ErrQuota
	case strings.Contains(text, "not a directory"):
		kind = remotefs.ErrNotDirectory
	}
	return fmt.Errorf("%s %q: %w: %w", op, p, kind, err)
}

// Tools implements [remotefs.Commands]: one probe per connection lists the programs the server has. A server that does not run
// commands, or runs something else in their place as servers restricted to SFTP do, offers nothing.
func (f *FS) Tools(ctx context.Context) (remotefs.Tools, error) {
	if err := f.guard(ctx); err != nil {
		return remotefs.Tools{}, err
	}
	f.cmds.once.Do(func() {
		probe, cancel := context.WithTimeout(context.WithoutCancel(ctx), probeTimeout)
		defer cancel()
		out, err := f.run(probe, "sh -c "+quote(probeScript))
		if err != nil && !errors.Is(err, remotefs.ErrUnsupported) && !isCommandError(err) {
			f.cmds.err = err
			return
		}
		f.cmds.tools = toolsOf(string(out))
	})
	return f.cmds.tools, f.cmds.err
}

// isCommandError reports whether a failure is a command that ran and failed.
func isCommandError(err error) bool {
	var failed *CommandError
	return errors.As(err, &failed)
}

// toolsOf reads the output of the probe; output without the marker offers nothing.
func toolsOf(out string) remotefs.Tools {
	lines := strings.Fields(out)
	if !slices.Contains(lines, probeMarker) {
		return remotefs.Tools{}
	}
	has := func(name string) bool { return slices.Contains(lines, name) }
	tools := remotefs.Tools{Copy: has("cp"), Usage: has("du")}
	for _, algorithm := range []string{"md5", "sha1", "sha256", "sha512"} {
		if has(sums[algorithm]) {
			tools.Checksums = append(tools.Checksums, algorithm)
		}
	}
	if has("zip") {
		tools.Archive = append(tools.Archive, remotefs.FormatZip)
	}
	if has("unzip") {
		tools.Extract = append(tools.Extract, remotefs.FormatZip)
	}
	if has("tar") && has("gzip") {
		tools.Archive = append(tools.Archive, remotefs.FormatTarGz)
		tools.Extract = append(tools.Extract, remotefs.FormatTarGz)
	}
	return tools
}

// need fails with [remotefs.ErrUnsupported] unless the server offers what ok checks.
func (f *FS) need(ctx context.Context, op, p string, ok func(remotefs.Tools) bool) error {
	tools, err := f.Tools(ctx)
	if err != nil {
		return err
	}
	if !ok(tools) {
		return fmt.Errorf("%s %q: %w", op, p, remotefs.ErrUnsupported)
	}
	return nil
}

// absent is the start of a command that fails as "File exists" when the quoted path names anything.
func absent(quoted string) string {
	return "if [ -e " + quoted + " ] || [ -L " + quoted + " ]; then echo 'File exists' >&2; exit 1; fi; "
}

// ServerCopy implements [remotefs.Commands] with cp, keeping the modes and times of the file.
func (f *FS) ServerCopy(ctx context.Context, from, to string, overwrite bool) error {
	if err := f.need(ctx, "copy", from, func(t remotefs.Tools) bool { return t.Copy }); err != nil {
		return err
	}
	paths, err := args(path.Clean(from), path.Clean(to))
	if err != nil {
		return err
	}
	command := "cp -p " + paths
	if !overwrite {
		target, _ := arg(path.Clean(to))
		command = absent(target) + command
	}
	if _, err := f.run(ctx, command); err != nil {
		return commandError("copy", from, err)
	}
	return nil
}

// ServerChecksum implements [remotefs.Commands] with the sum programs of coreutils or BusyBox.
func (f *FS) ServerChecksum(ctx context.Context, p, algorithm string) (string, error) {
	program, ok := sums[algorithm]
	if !ok {
		return "", fmt.Errorf("checksum %q: %w: unknown algorithm %q", p, remotefs.ErrInvalid, algorithm)
	}
	if err := f.need(ctx, "checksum", p, func(t remotefs.Tools) bool { return slices.Contains(t.Checksums, algorithm) }); err != nil {
		return "", err
	}
	target, err := arg(path.Clean(p))
	if err != nil {
		return "", err
	}
	out, err := f.run(ctx, program+" "+target)
	if err != nil {
		return "", commandError("checksum", p, err)
	}
	fields := strings.Fields(strings.TrimPrefix(string(out), `\`))
	if len(fields) == 0 || !hexDigest.MatchString(fields[0]) {
		return "", fmt.Errorf("checksum %q: %w: unreadable output", p, remotefs.ErrUnsupported)
	}
	return strings.ToLower(fields[0]), nil
}

// ServerUsage implements [remotefs.Commands] with du, which counts kibibytes on the disk.
func (f *FS) ServerUsage(ctx context.Context, p string) (int64, error) {
	if err := f.need(ctx, "usage", p, func(t remotefs.Tools) bool { return t.Usage }); err != nil {
		return 0, err
	}
	target, err := arg(path.Clean(p))
	if err != nil {
		return 0, err
	}
	out, err := f.run(ctx, "du -sk "+target)
	if err != nil {
		return 0, commandError("usage", p, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, fmt.Errorf("usage %q: %w: unreadable output", p, remotefs.ErrUnsupported)
	}
	kib, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("usage %q: %w: unreadable output", p, remotefs.ErrUnsupported)
	}
	return kib * 1024, nil
}

// ServerArchive implements [remotefs.Commands] with zip, which stores symbolic links as links, or tar with gzip. The entries are
// named relative to dir, so the archive holds them without the folders above.
func (f *FS) ServerArchive(ctx context.Context, dir string, names []string, target, format string) error {
	if err := f.need(ctx, "archive", target, func(t remotefs.Tools) bool { return slices.Contains(t.Archive, format) }); err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("archive %q: %w: nothing to archive", target, remotefs.ErrInvalid)
	}
	entries := make([]string, len(names))
	for i, name := range names {
		if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
			return fmt.Errorf("archive %q: %w: %q is not a name in the folder", target, remotefs.ErrInvalid, name)
		}
		entries[i] = "./" + name
	}
	quoted, err := args(entries...)
	if err != nil {
		return err
	}
	folder, err := arg(path.Clean(dir))
	if err != nil {
		return err
	}
	archive, err := arg(path.Clean(target))
	if err != nil {
		return err
	}
	command := absent(archive) + "cd " + folder + " && zip -q -r -y " + archive + " " + quoted
	if format == remotefs.FormatTarGz {
		command = absent(archive) + "tar -czf " + archive + " -C " + folder + " " + quoted
	}
	if _, err := f.run(ctx, command); err != nil {
		return commandError("archive", target, err)
	}
	return nil
}

// onlyKept reports whether everything tar complained about is files it kept because they existed.
func onlyKept(stderr string) bool {
	kept := false
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		switch {
		case strings.Contains(line, "File exists"):
			kept = true
		case strings.Contains(line, "Exiting with failure status"):
		default:
			return false
		}
	}
	return kept
}

// ServerExtract implements [remotefs.Commands] with unzip or tar, which keep the files already in dir and, by default, skip
// members with absolute paths or that climb out of it. unzip exits with 1 for warnings, such as a member it skipped, and GNU tar
// fails when it kept a file, which is what was asked.
func (f *FS) ServerExtract(ctx context.Context, archive, format, dir string) error {
	if err := f.need(ctx, "extract", archive, func(t remotefs.Tools) bool { return slices.Contains(t.Extract, format) }); err != nil {
		return err
	}
	source, err := arg(path.Clean(archive))
	if err != nil {
		return err
	}
	target, err := arg(path.Clean(dir))
	if err != nil {
		return err
	}
	command := "unzip -q -n " + source + " -d " + target
	if format == remotefs.FormatTarGz {
		command = "tar -xzkf " + source + " -C " + target
	}
	_, err = f.run(ctx, command)
	var failed *CommandError
	if errors.As(err, &failed) && (format == remotefs.FormatZip && failed.Status == 1 || format == remotefs.FormatTarGz && onlyKept(failed.Stderr)) {
		return nil
	}
	if err != nil {
		return commandError("extract", archive, err)
	}
	return nil
}
