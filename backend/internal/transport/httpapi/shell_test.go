package httpapi_test

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync/atomic"

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

// Shell implements [remotefs.Sheller]: a shell that says where it started and on what screen, answers each line typed with
// "echo: " and the line, tells each resize, ends with status 5 at the line "exit", and refuses the line "fail".
func (shellFS) Shell(_ context.Context, dir string, cols, rows int) (remotefs.Terminal, error) {
	out, in := io.Pipe()
	term := &echoTerminal{out: out, in: in}
	go func() { _, _ = fmt.Fprintf(in, "ready in %s %dx%d\n", dir, cols, rows) }()
	return term, nil
}

// echoTerminal is the shell of [shellFS].
type echoTerminal struct {
	out    *io.PipeReader
	in     *io.PipeWriter
	exited atomic.Bool
}

// Read implements io.Reader.
func (e *echoTerminal) Read(p []byte) (int, error) { return e.out.Read(p) }

// Write implements io.Writer.
func (e *echoTerminal) Write(p []byte) (int, error) {
	if string(p) == "fail\n" {
		return 0, io.ErrClosedPipe
	}
	if string(p) == "exit\n" {
		e.exited.Store(true)
		_ = e.in.Close()
		return len(p), nil
	}
	_, _ = io.WriteString(e.in, "echo: "+string(p))
	return len(p), nil
}

// Resize implements [remotefs.Terminal].
func (e *echoTerminal) Resize(cols, rows int) error {
	_, err := fmt.Fprintf(e.in, "resized %dx%d\n", cols, rows)
	return err
}

// Wait implements [remotefs.Terminal].
func (e *echoTerminal) Wait() (int, error) {
	if e.exited.Load() {
		return 5, nil
	}
	return -1, io.ErrClosedPipe
}

// Close implements io.Closer.
func (e *echoTerminal) Close() error { return e.in.CloseWithError(io.EOF) }
