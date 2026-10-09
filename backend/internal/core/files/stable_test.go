package files_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/google/uuid"
)

// TestStableFileGuarantees verifies preservation, staging identity, path aliases and bounded copy admission.
func TestStableFileGuarantees(t *testing.T) {
	f := newFixture(t)
	f.put(t, "/original", "original bytes")
	f.fs.failRemove = true
	if _, err := f.svc.Upload(t.Context(), f.editor, "/original", remotefs.WriteOverwrite, 0, strings.NewReader("new")); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Fatal(err)
	}
	f.fs.failRemove = false
	if got := content(t, f, "/original"); got != "original bytes" {
		t.Fatalf("original changed: %q", got)
	}
	for _, from := range []string{"/src/../src", "/src/"} {
		if err := f.svc.CheckCopy(t.Context(), files.CopyLocation{Source: f.editor, SourcePath: from, Destination: f.editor, DestinationPath: "/src/child"}); !errors.Is(err, remotefs.ErrInvalid) {
			t.Fatalf("alias admitted: %s, %v", from, err)
		}
	}
	owned := f.editor
	owned.UploadID = uuid.New()
	broken := ioErrorReader{}
	_, _ = f.svc.Upload(t.Context(), owned, "/owned", remotefs.WriteCreate, 0, broken)
	if _, err := f.svc.Stat(t.Context(), owned, "/transferly-"+owned.UploadID.String()+".part"); err != nil {
		t.Fatalf("owned staging missing: %v", err)
	}
	f.put(t, "/source", "large stream")
	pool := connections.New(connector{f.fs}, f.clock, connections.Config{MaxPerSite: 1, Attempts: 1})
	defer pool.Close()
	svc := files.NewService(roles{f.editor.Actor: workspace.RoleEditor}, pool).WithBufferedLimit(0)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/source", Destination: f.editor, DestinationPath: "/dest"}, files.PolicyFail)
	if !errors.Is(err, remotefs.ErrUnsupported) || !errors.Is(err, files.ErrCopyConnectionLimit) {
		t.Fatalf("single-connection copy = %v", err)
	}
}

// TestArchiveMemberValidation rejects invalid virtual names before opening a remote archive while accepting their relative namespace.
func TestArchiveMemberValidation(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.ArchiveEntries(t.Context(), f.editor, "/absent.zip", []string{"bad\x00name"}); !errors.Is(err, remotefs.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := f.svc.OpenArchiveEntry(t.Context(), f.editor, "/absent.zip", nil, ""); !errors.Is(err, remotefs.ErrInvalid) {
		t.Fatal(err)
	}
	svc := files.NewService(roles{f.editor.Actor: workspace.RoleEditor}, unavailablePool{})
	if _, err := svc.ArchiveEntries(t.Context(), f.editor, "/archive.zip", nil); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Fatal(err)
	}
}

// unavailablePool models a remote connection that fails after names and permissions have been validated.
type unavailablePool struct{}

// Acquire refuses the connection without lending a lease.
func (unavailablePool) Acquire(context.Context, connections.Key, string) (*connections.Lease, error) {
	return nil, remotefs.ErrConnectionLost
}

// ioErrorReader simulates a client that disconnects before its upload body completes.
type ioErrorReader struct{}

// Read fails without producing bytes, leaving an identifiable staging file for later recovery.
func (ioErrorReader) Read([]byte) (int, error) { return 0, errors.New("interrupted") }

// noReplacement hides an atomic extension to model a protocol without safe overwrite.
type noReplacement struct{ remotefs.RemoteFS }

// TestUnsupportedOverwrite preserves existing files on protocols without replacement and admits only missing targets.
func TestUnsupportedOverwrite(t *testing.T) {
	f := newFixture(t)
	f.put(t, "/original", "keep")
	pool := connections.New(connector{noReplacement{f.fs}}, f.clock, connections.Config{Attempts: 1})
	defer pool.Close()
	svc := files.NewService(roles{f.editor.Actor: workspace.RoleEditor}, pool)
	if _, err := svc.Upload(t.Context(), f.editor, "/original", remotefs.WriteOverwrite, 0, strings.NewReader("discard")); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Fatal(err)
	}
	if got := content(t, f, "/original"); got != "keep" {
		t.Fatal(got)
	}
	if _, err := svc.Upload(t.Context(), f.editor, "/new", remotefs.WriteOverwrite, 0, strings.NewReader("new")); err != nil {
		t.Fatal(err)
	}

	raced := &raceReader{fs: f.fs.shared, path: "/raced", content: "original from another client", src: strings.NewReader("new")}
	if _, err := svc.Upload(t.Context(), f.editor, "/raced", remotefs.WriteOverwrite, 0, raced); !errors.Is(err, remotefs.ErrUnsafeReplace) {
		t.Fatal(err)
	}
	if got := content(t, f, "/raced"); got != "original from another client" {
		t.Fatal(got)
	}
	if _, err := svc.Upload(t.Context(), f.editor, "/stat-fails-at-commit", remotefs.WriteOverwrite, 0, &failStatReader{Reader: strings.NewReader("new"), fs: f.fs}); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Fatal(err)
	}
	f.fs.failStat = true
	if _, err := svc.Upload(t.Context(), f.editor, "/failure", remotefs.WriteOverwrite, 0, strings.NewReader("new")); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Fatal(err)
	}
}

// failStatReader models a server that becomes unavailable after accepting the body.
type failStatReader struct {
	io.Reader
	fs *flakyFS
}

// Read changes the fault only after the preflight and write handle have succeeded.
func (r *failStatReader) Read(p []byte) (int, error) {
	r.fs.failStat = true
	return r.Reader.Read(p)
}

// gatedUpload pauses a body while another request attempts to write the same destination.
type gatedUpload struct {
	ready, release chan struct{}
	once           sync.Once
	source         io.Reader
}

// Read exposes an in-progress writer before forwarding its bounded test payload.
func (g *gatedUpload) Read(p []byte) (int, error) {
	g.once.Do(func() { close(g.ready); <-g.release })
	return g.source.Read(p)
}

// TestConcurrentUploadAdmission prevents alias paths from writing the same staging file concurrently.
func TestConcurrentUploadAdmission(t *testing.T) {
	f := newFixture(t)
	for _, invalid := range []string{"", "relative", "dir/../relative"} {
		if _, err := f.svc.Upload(t.Context(), f.editor, invalid, remotefs.WriteCreate, 0, strings.NewReader("x")); !errors.Is(err, remotefs.ErrInvalid) {
			t.Fatal(err)
		}
	}
	body := &gatedUpload{ready: make(chan struct{}), release: make(chan struct{}), source: strings.NewReader("first")}
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.Upload(t.Context(), f.editor, "/dir/../same", remotefs.WriteCreate, 0, body)
		done <- err
	}()
	<-body.ready
	_, err := f.svc.Upload(t.Context(), f.editor, "/same", remotefs.WriteCreate, 0, strings.NewReader("second"))
	close(body.release)
	if !errors.Is(err, remotefs.ErrExists) {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := content(t, f, "/same"); got != "first" {
		t.Fatal(got)
	}
}

// unsupportedReplacer models a negotiated extension that is unavailable on this particular server.
type unsupportedReplacer struct{ *flakyFS }

// Replace refuses substitution before changing either file.
func (unsupportedReplacer) Replace(context.Context, string, string) error {
	return remotefs.ErrUnsupported
}

// TestNegotiatedReplacementRefusal preserves the destination when an optional extension is not supported.
func TestNegotiatedReplacementRefusal(t *testing.T) {
	f := newFixture(t)
	f.put(t, "/original", "keep")
	pool := connections.New(connector{unsupportedReplacer{f.fs}}, f.clock, connections.Config{Attempts: 1})
	defer pool.Close()
	svc := files.NewService(roles{f.editor.Actor: workspace.RoleEditor}, pool)
	_, err := svc.Upload(t.Context(), f.editor, "/original", remotefs.WriteOverwrite, 0, strings.NewReader("new"))
	if !errors.Is(err, remotefs.ErrUnsafeReplace) {
		t.Fatal(err)
	}
	if got := content(t, f, "/original"); got != "keep" {
		t.Fatal(got)
	}
}
