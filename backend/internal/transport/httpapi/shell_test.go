package httpapi_test

import (
	"context"
	"io"
	"path"
	"strings"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// shellSite is the name of a site whose server runs commands (ADR-0027), as an SFTP server with a shell does.
const shellSite = "Shell server"

// shellFS is a connection to a server that runs commands, in memory: copies and archives are made with the file system, and a
// file named refuse.zip cannot be unpacked.
type shellFS struct{ *sharedFS }

// Tools implements [remotefs.Commands].
func (shellFS) Tools(context.Context) (remotefs.Tools, error) {
	return remotefs.Tools{Copy: true, Usage: true, Checksums: []string{"sha256"}, Archive: []string{remotefs.FormatZip}, Extract: []string{remotefs.FormatZip}}, nil
}

// ServerCopy implements [remotefs.Commands].
func (f shellFS) ServerCopy(ctx context.Context, from, to string, _ bool) error {
	return f.write(ctx, to, "copied from "+from)
}

// write stores content in a file.
func (f shellFS) write(ctx context.Context, p, content string) error {
	w, err := f.OpenWrite(ctx, p, 0, remotefs.WriteOverwrite)
	if err != nil {
		return err
	}
	_, _ = io.WriteString(w, content)
	return w.Close()
}

// ServerChecksum implements [remotefs.Commands].
func (shellFS) ServerChecksum(context.Context, string, string) (string, error) {
	return strings.Repeat("0", 64), nil
}

// ServerUsage implements [remotefs.Commands].
func (shellFS) ServerUsage(context.Context, string) (int64, error) { return 8192, nil }

// ServerArchive implements [remotefs.Commands].
func (f shellFS) ServerArchive(ctx context.Context, _ string, names []string, target, _ string) error {
	return f.write(ctx, target, strings.Join(names, ","))
}

// ServerExtract implements [remotefs.Commands].
func (f shellFS) ServerExtract(ctx context.Context, archive, _, dir string) error {
	if path.Base(archive) == "refuse.zip" {
		return remotefs.ErrPermission
	}
	return f.write(ctx, path.Join(dir, "inside.txt"), "x")
}
