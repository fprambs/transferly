package files_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/remotefs/memfs"
	"github.com/fprambs/transferly/internal/core/archives"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/testutil"
)

// strictFS is a connection to a server that reports every file as off bytes larger than it is, as some enterprise SFTP gateways
// do, and that learns whether to trust its sizes from the whole reads it is told about.
type strictFS struct {
	*flakyFS
	off       int64
	untrusted bool
	noted     [][2]int64
}

// Stat reports the size of a file off by s.off bytes.
func (s *strictFS) Stat(ctx context.Context, p string) (remotefs.Entry, error) {
	entry, err := s.flakyFS.Stat(ctx, p)
	if err == nil && !entry.IsDir() {
		entry.Size += s.off
	}
	return entry, err
}

// OpenRead opens a file at an offset the misreported size allows, past its real end if need be.
func (s *strictFS) OpenRead(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	entry, err := s.flakyFS.Stat(ctx, p)
	if err != nil {
		return nil, err
	}
	return s.flakyFS.OpenRead(ctx, p, min(offset, entry.Size))
}

// SizesTrusted implements [remotefs.SizeChecker].
func (s *strictFS) SizesTrusted() bool { return !s.untrusted }

// NoteWholeRead implements [remotefs.SizeChecker], recording what it was told.
func (s *strictFS) NoteWholeRead(reported, read int64) {
	s.noted = append(s.noted, [2]int64{reported, read})
}

// strictConnector hands out connections to one strict file system.
type strictConnector struct{ fsys *strictFS }

// Connect returns the strict file system.
func (c strictConnector) Connect(context.Context, connections.Key, string) (remotefs.RemoteFS, error) {
	return c.fsys, nil
}

// newStrictFixture returns a fixture whose site is a strict server, holding /f with ten bytes.
func newStrictFixture(t *testing.T, off int64) (*fixture, *strictFS) {
	t.Helper()
	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	strict := &strictFS{flakyFS: &flakyFS{shared: shared{memfs.New(clock)}}, off: off}
	pool := connections.New(strictConnector{strict}, clock, connections.Config{Attempts: 1})
	t.Cleanup(pool.Close)
	ws, site, viewer := uuid.New(), uuid.New(), uuid.New()
	f := &fixture{
		clock: clock, fs: strict.flakyFS, pool: pool,
		svc:    files.NewService(roles{viewer: workspace.RoleViewer}, pool),
		viewer: files.Target{Actor: viewer, Workspace: ws, Site: site, Session: "s"},
	}
	f.put(t, "/f", "0123456789")
	return f, strict
}

// TestService_Download_Buffered verifies that small whole downloads are read before answering, so they announce the bytes sent.
//
// Use cases:
//   - a file the server reports larger than it is downloads with the length of its real bytes, the connection goes back before the
//     body is read, and the connection hears how many bytes came
//   - a file whose size holds is told so too
//   - a file reported small enough to buffer that runs past the cap streams to its end with no length announced
//   - a file that fails or a request canceled while it is read fails the download and the failing connection is discarded
func TestService_Download_Buffered(t *testing.T) {
	ctx := context.Background()
	f, strict := newStrictFixture(t, 4)
	obs := &recorder{}
	target := f.viewer
	target.Observer = obs

	d, err := f.svc.Download(ctx, target, "/f", files.Range{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Length() != 10 || d.Unsized || d.Partial || f.idle(f.viewer) != 1 {
		t.Errorf("Download() = length %d, unsized %v, partial %v with %d idle; want 10 bytes, sized, connection back", d.Length(), d.Unsized, d.Partial, f.idle(f.viewer))
	}
	if got := readAll(t, d); got != "0123456789" || obs.bytes != 10 || len(obs.started) != 1 || obs.started[0] != 10 {
		t.Errorf("content %q with %d bytes counted, started %v", got, obs.bytes, obs.started)
	}
	if len(strict.noted) != 1 || strict.noted[0] != [2]int64{14, 10} {
		t.Errorf("noted %v; want [[14 10]]", strict.noted)
	}

	strict.off, strict.noted = 0, nil
	if d, err = f.svc.Download(ctx, target, "/f", files.Range{}); err != nil || readAll(t, d) != "0123456789" {
		t.Fatalf("Download() of an honest file = %v", err)
	}
	if len(strict.noted) != 1 || strict.noted[0] != [2]int64{10, 10} {
		t.Errorf("noted %v; want [[10 10]]", strict.noted)
	}

	strict.off, strict.noted, obs.started, obs.bytes = -7, nil, nil, 0
	if d, err = f.svc.WithBufferedLimit(4).Download(ctx, target, "/f", files.Range{}); err != nil {
		t.Fatal(err)
	}
	if !d.Unsized || d.Length() != -1 || readAll(t, d) != "0123456789" || obs.bytes != 10 || obs.started[0] != -1 {
		t.Errorf("overflowing download = unsized %v, length %d, %d bytes counted, started %v", d.Unsized, d.Length(), obs.bytes, obs.started)
	}
	if len(strict.noted) != 1 || strict.noted[0] != [2]int64{3, 5} || f.idle(f.viewer) != 1 {
		t.Errorf("noted %v with %d idle; want [[3 5]] and the connection back", strict.noted, f.idle(f.viewer))
	}

	strict.off = 0
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.svc.WithBufferedLimit(64).Download(canceled, target, "/f", files.Range{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Download() canceled while buffering = %v; want context.Canceled", err)
	}
	f.fs.failRead = true
	if _, err := f.svc.Download(ctx, target, "/f", files.Range{}); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Download() of a file that fails = %v; want ErrConnectionLost", err)
	}
	if f.idle(f.viewer) != 0 {
		t.Error("a connection that dropped while buffering was pooled")
	}
}

// TestService_Download_Unsized verifies downloads from a server whose sizes are not trusted.
//
// Use cases:
//   - a whole file too large to buffer streams to its real end with no length announced, and is not checked again
//   - a range is still served by the sizes the server reports
func TestService_Download_Unsized(t *testing.T) {
	ctx := context.Background()
	f, strict := newStrictFixture(t, 5)
	strict.untrusted = true
	f.svc.WithBufferedLimit(4)
	obs := &recorder{}
	target := f.viewer
	target.Observer = obs

	d, err := f.svc.Download(ctx, target, "/f", files.Range{})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Unsized || d.Length() != -1 || readAll(t, d) != "0123456789" || obs.bytes != 10 || obs.started[0] != -1 {
		t.Errorf("Download() = unsized %v, length %d, %d bytes counted, started %v", d.Unsized, d.Length(), obs.bytes, obs.started)
	}
	if len(strict.noted) != 0 || f.idle(f.viewer) != 1 {
		t.Errorf("noted %v with %d idle; want nothing noted and the connection back", strict.noted, f.idle(f.viewer))
	}

	if d, err = f.svc.Download(ctx, target, "/f", files.Range{Set: true, Start: 2, End: 4}); err != nil || d.Unsized || readAll(t, d) != "234" {
		t.Errorf("range on an untrusted server = %+v, %v; want bytes 2 to 4", d, err)
	}
}

// TestService_Download_ChecksStreams verifies that a whole file streamed to its end tells the connection how many bytes came.
//
// Use cases:
//   - a file that ends before its reported size is reported, read chunk by chunk or written by itself
//   - a range, copied chunk by chunk, and a download abandoned before its end and closed twice, report nothing
func TestService_Download_ChecksStreams(t *testing.T) {
	ctx := context.Background()
	f, strict := newStrictFixture(t, 3)
	f.svc.WithBufferedLimit(0)

	d, err := f.svc.Download(ctx, f.viewer, "/f", files.Range{})
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, d); got != "0123456789" || d.Length() != 13 {
		t.Errorf("streamed %q announcing %d bytes", got, d.Length())
	}
	f.fs.writeTo = true
	d, _ = f.svc.Download(ctx, f.viewer, "/f", files.Range{})
	var out strings.Builder
	if _, err := io.Copy(&out, d.Body); err != nil || out.String() != "0123456789" || f.fs.writesTo.Load() != 1 {
		t.Errorf("self written %q, %v with %d self writes", out.String(), err, f.fs.writesTo.Load())
	}
	_ = d.Body.Close()
	if len(strict.noted) != 2 || strict.noted[0] != [2]int64{13, 10} || strict.noted[1] != [2]int64{13, 10} {
		t.Errorf("noted %v; want two reads of 10 bytes reported as 13", strict.noted)
	}

	strict.noted = nil
	if d, err = f.svc.Download(ctx, f.viewer, "/f", files.Range{Set: true, Start: 0, End: 3}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if _, err := io.Copy(&out, d.Body); err != nil || out.String() != "0123" {
		t.Errorf("range copied = %q, %v", out.String(), err)
	}
	_ = d.Body.Close()
	d, _ = f.svc.Download(ctx, f.viewer, "/f", files.Range{})
	_ = d.Body.Close()
	_ = d.Body.Close()
	if len(strict.noted) != 0 {
		t.Errorf("noted %v; want nothing for a range or an abandoned download", strict.noted)
	}
}

// TestService_ArchiveEntries_ReadWhole verifies that archives are read whole into memory when they are small or their server's sizes
// are not trusted, so their index is found where their bytes end.
//
// Use cases:
//   - a small ZIP the server reports larger than it is lists its entries, and the connection hears how many bytes came
//   - a larger ZIP on an untrusted server is read whole too, and an entry of it is read
//   - an archive over the cap, reported so or found so while reading, is too large
//   - a file that cannot be opened or read fails and its connection is discarded
func TestService_ArchiveEntries_ReadWhole(t *testing.T) {
	ctx := context.Background()
	f, strict := newStrictFixture(t, 100)
	archive := zipOf(t, map[string][]byte{"hello.txt": []byte("hello")})
	f.put(t, "/a.zip", string(archive))
	size := int64(len(archive))

	listing, err := f.svc.ArchiveEntries(ctx, f.viewer, "/a.zip", nil)
	if err != nil || len(listing.Entries) != 1 || listing.Entries[0].Name != "hello.txt" {
		t.Fatalf("ArchiveEntries() = %+v, %v", listing, err)
	}
	if len(strict.noted) != 1 || strict.noted[0] != [2]int64{size + 100, size} || f.idle(f.viewer) != 1 {
		t.Errorf("noted %v with %d idle; want [[%d %d]] and the connection back", strict.noted, f.idle(f.viewer), size+100, size)
	}

	strict.untrusted = true
	f.svc.WithBufferedLimit(0)
	entry, err := f.svc.OpenArchiveEntry(ctx, f.viewer, "/a.zip", nil, "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := readEntryBody(t, entry.Body); string(got) != "hello" {
		t.Errorf("entry = %q; want hello", got)
	}

	f.svc.WithNestedLimit(size - 1)
	if _, err := f.svc.ArchiveEntries(ctx, f.viewer, "/a.zip", nil); !errors.Is(err, archives.ErrTooLarge) {
		t.Errorf("ArchiveEntries() reported over the cap = %v; want ErrTooLarge", err)
	}
	strict.off = -100
	if _, err := f.svc.ArchiveEntries(ctx, f.viewer, "/a.zip", nil); !errors.Is(err, archives.ErrTooLarge) {
		t.Errorf("ArchiveEntries() found over the cap = %v; want ErrTooLarge", err)
	}

	f.svc.WithNestedLimit(1 << 20)
	f.fs.failOpen = true
	if _, err := f.svc.ArchiveEntries(ctx, f.viewer, "/a.zip", nil); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("ArchiveEntries() of a file that cannot be opened = %v; want ErrConnectionLost", err)
	}
	f.fs.failOpen, f.fs.failRead = false, true
	if _, err := f.svc.ArchiveEntries(ctx, f.viewer, "/a.zip", nil); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("ArchiveEntries() of a file that fails = %v; want ErrConnectionLost", err)
	}
	if f.idle(f.viewer) != 0 {
		t.Error("a connection that dropped while reading an archive was pooled")
	}
}
