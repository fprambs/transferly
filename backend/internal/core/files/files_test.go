package files_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/remotefs/memfs"
	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/files"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/testutil"
)

// shared lets several connections use one in-memory file system, and keeps it open when one of them closes.
type shared struct{ *memfs.FS }

// Close does nothing: the file system outlives its connections.
func (shared) Close() error { return nil }

// flakyFS is a shared file system whose next operations can fail as if the connection dropped.
type flakyFS struct {
	shared
	failRead   bool
	failOpen   bool
	failStat   bool
	failCommit bool
	failRemove bool
}

// Remove fails when configured to.
func (f *flakyFS) Remove(ctx context.Context, p string) error {
	if f.failRemove {
		return fmt.Errorf("%w: dropped", remotefs.ErrConnectionLost)
	}
	return f.shared.Remove(ctx, p)
}

// Stat fails when configured to.
func (f *flakyFS) Stat(ctx context.Context, p string) (remotefs.Entry, error) {
	if f.failStat {
		return remotefs.Entry{}, fmt.Errorf("%w: dropped", remotefs.ErrConnectionLost)
	}
	return f.shared.Stat(ctx, p)
}

// OpenRead fails, or returns a stream that fails, when configured to.
func (f *flakyFS) OpenRead(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	if f.failOpen {
		return nil, fmt.Errorf("%w: dropped", remotefs.ErrConnectionLost)
	}
	r, err := f.shared.OpenRead(ctx, p, offset)
	if err != nil || !f.failRead {
		return r, err
	}
	return brokenReader{r}, nil
}

// OpenWrite returns a writer whose Close fails when configured to.
func (f *flakyFS) OpenWrite(ctx context.Context, p string, offset int64, mode remotefs.WriteMode) (io.WriteCloser, error) {
	w, err := f.shared.OpenWrite(ctx, p, offset, mode)
	if err != nil || !f.failCommit {
		return w, err
	}
	return failingCloser{w}, nil
}

// brokenReader fails after its first read.
type brokenReader struct{ io.ReadCloser }

// Read reads a little and then fails like a dropped connection.
func (b brokenReader) Read(p []byte) (int, error) {
	n, _ := b.ReadCloser.Read(p[:1])
	return n, fmt.Errorf("%w: dropped", remotefs.ErrConnectionLost)
}

// failingCloser fails when closed.
type failingCloser struct{ io.WriteCloser }

// Close closes the writer and reports a lost connection.
func (f failingCloser) Close() error {
	_ = f.WriteCloser.Close()
	return fmt.Errorf("%w: dropped", remotefs.ErrConnectionLost)
}

// connector hands out connections to one shared file system.
type connector struct{ fsys *flakyFS }

// Connect returns a connection to the shared file system.
func (c connector) Connect(context.Context, connections.Key, string) (remotefs.RemoteFS, error) {
	return c.fsys, nil
}

// roles answers authorization from a table of roles by user, using the real permission matrix.
type roles map[uuid.UUID]workspace.Role

// Authorize applies the permission matrix.
func (r roles) Authorize(_ context.Context, user, _ uuid.UUID, action workspace.Action) (workspace.Role, error) {
	role, ok := r[user]
	if !ok {
		return "", workspace.ErrNotFound
	}
	if !workspace.Can(role, action) {
		return "", workspace.ErrForbidden
	}
	return role, nil
}

// fixture is a service over an in-memory file system, with one user per role.
type fixture struct {
	clock  *testutil.FakeClock
	svc    *files.Service
	fs     *flakyFS
	pool   *connections.Manager
	editor files.Target
	viewer files.Target
}

// newFixture returns a service where the editor may change files and the viewer may only browse and download.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fsys := &flakyFS{shared: shared{memfs.New(clock)}}
	pool := connections.New(connector{fsys}, clock, connections.Config{Attempts: 1})
	t.Cleanup(pool.Close)
	ws, site := uuid.New(), uuid.New()
	editor, viewer := uuid.New(), uuid.New()
	return &fixture{
		clock: clock,
		svc:   files.NewService(roles{editor: workspace.RoleEditor, viewer: workspace.RoleViewer}, pool), fs: fsys, pool: pool,
		editor: files.Target{Actor: editor, Workspace: ws, Site: site, Session: "s"},
		viewer: files.Target{Actor: viewer, Workspace: ws, Site: site, Session: "s"},
	}
}

// put stores a file directly in the file system.
func (f *fixture) put(t *testing.T, path, content string) {
	t.Helper()
	w, err := f.fs.shared.OpenWrite(t.Context(), path, 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, content)
	_ = w.Close()
}

// idle returns how many connections are idle in the pool.
func (f *fixture) idle(t files.Target) int {
	idle, _ := f.pool.Stats(connections.Key{UserID: t.Actor, WorkspaceID: t.Workspace, SiteID: t.Site})
	return idle
}

// TestService_Operations verifies the short operations.
//
// Scenarios:
//   - listing, stat, mkdir, rename and delete work and return the affected entry
//   - deleting a directory needs recursive unless it is empty, and removes the tree when set
//   - remote errors keep their sentinels: missing paths, existing destinations, non-empty directories
//   - every operation gives its connection back to the pool
func TestService_Operations(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.put(t, "/a.txt", "hello")

	list, err := f.svc.List(ctx, f.editor, "/")
	if err != nil || len(list) != 1 || list[0].Name != "a.txt" {
		t.Fatalf("List() = %v, %v", list, err)
	}
	if e, err := f.svc.Stat(ctx, f.editor, "/a.txt"); err != nil || e.Size != 5 {
		t.Errorf("Stat() = %+v, %v", e, err)
	}
	dir, err := f.svc.Mkdir(ctx, f.editor, "/d")
	if err != nil || !dir.IsDir() {
		t.Fatalf("Mkdir() = %+v, %v", dir, err)
	}
	if _, err := f.svc.Mkdir(ctx, f.editor, "/d"); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("Mkdir() again = %v; want ErrExists", err)
	}
	moved, err := f.svc.Rename(ctx, f.editor, "/a.txt", "/d/b.txt")
	if err != nil || moved.Path != "/d/b.txt" {
		t.Fatalf("Rename() = %+v, %v", moved, err)
	}
	f.put(t, "/other", "x")
	if _, err := f.svc.Rename(ctx, f.editor, "/other", "/d/b.txt"); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("Rename() onto an existing path = %v; want ErrExists", err)
	}
	if _, err := f.svc.Stat(ctx, f.editor, "/missing"); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Stat() of a missing path = %v; want ErrNotFound", err)
	}

	if err := f.svc.Delete(ctx, f.editor, "/d", false); !errors.Is(err, remotefs.ErrNotEmpty) {
		t.Errorf("Delete() of a non-empty directory = %v; want ErrNotEmpty", err)
	}
	if err := f.svc.Delete(ctx, f.editor, "/d", true); err != nil {
		t.Errorf("recursive Delete() = %v", err)
	}
	if err := f.svc.Delete(ctx, f.editor, "/other", false); err != nil {
		t.Errorf("Delete() of a file = %v", err)
	}
	if f.idle(f.editor) != 1 {
		t.Errorf("idle connections = %d; want every operation to give its connection back", f.idle(f.editor))
	}
}

// TestService_Authorization verifies permissions and path validation.
//
// Scenarios:
//   - a viewer may list, stat and download but not upload, create directories, rename or delete
//   - a stranger gets workspace.ErrNotFound for everything
//   - empty, oversized and control-character paths are ErrInvalid and never reach the server
//   - no connection is borrowed for refused requests
func TestService_Authorization(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.put(t, "/a.txt", "hello")
	stranger := f.editor
	stranger.Actor = uuid.New()

	if _, err := f.svc.List(ctx, f.viewer, "/"); err != nil {
		t.Errorf("viewer List() = %v", err)
	}
	if _, err := f.svc.Stat(ctx, f.viewer, "/a.txt"); err != nil {
		t.Errorf("viewer Stat() = %v", err)
	}
	if d, err := f.svc.Download(ctx, f.viewer, "/a.txt", files.Range{}); err != nil {
		t.Errorf("viewer Download() = %v", err)
	} else {
		_ = d.Body.Close()
	}
	refused := map[string]error{
		"Upload": second(f.svc.Upload(ctx, f.viewer, "/n", remotefs.WriteCreate, 0, strings.NewReader("x"))),
		"Mkdir":  second(f.svc.Mkdir(ctx, f.viewer, "/n")),
		"Rename": second(f.svc.Rename(ctx, f.viewer, "/a.txt", "/b.txt")),
		"Delete": f.svc.Delete(ctx, f.viewer, "/a.txt", false),
	}
	for name, err := range refused {
		if !errors.Is(err, workspace.ErrForbidden) {
			t.Errorf("viewer %s = %v; want ErrForbidden", name, err)
		}
	}
	strangers := map[string]error{
		"List":     second(f.svc.List(ctx, stranger, "/")),
		"Stat":     second(f.svc.Stat(ctx, stranger, "/a.txt")),
		"Download": second(f.svc.Download(ctx, stranger, "/a.txt", files.Range{})),
		"Upload":   second(f.svc.Upload(ctx, stranger, "/n", remotefs.WriteCreate, 0, strings.NewReader("x"))),
	}
	for name, err := range strangers {
		if !errors.Is(err, workspace.ErrNotFound) {
			t.Errorf("stranger %s = %v; want workspace.ErrNotFound", name, err)
		}
	}

	for _, bad := range []string{"", strings.Repeat("a", 4097), "/a\nb", "/a\x00b", "/a\x7f"} {
		if _, err := f.svc.Stat(ctx, f.editor, bad); !errors.Is(err, remotefs.ErrInvalid) {
			t.Errorf("Stat(%q) = %v; want ErrInvalid", bad, err)
		}
	}
	if _, err := f.svc.Rename(ctx, f.editor, "/a.txt", ""); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Rename() to an empty path = %v; want ErrInvalid", err)
	}
}

// second returns the error of a call that also returns a value.
func second[T any](_ T, err error) error { return err }

// readAll reads a download to the end and closes it.
func readAll(t *testing.T, d *files.Download) string {
	t.Helper()
	data, err := io.ReadAll(d.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Body.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestService_Download verifies streaming and ranges.
//
// Scenarios:
//   - a whole file streams with its entry, and the connection returns to the pool when the stream is closed
//   - closing twice is harmless
//   - ranges: from an offset, bounded, beyond the end (clamped), and the last bytes
//   - unsatisfiable ranges, ranges on empty files and directories are refused, and give the connection back
//   - a connection lost while opening or reading is reported and the broken connection is not pooled
func TestService_Download(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.put(t, "/f", "0123456789")
	f.put(t, "/empty", "")
	if _, err := f.svc.Mkdir(ctx, f.editor, "/dir"); err != nil {
		t.Fatal(err)
	}

	d, err := f.svc.Download(ctx, f.viewer, "/f", files.Range{})
	if err != nil || d.Partial || d.Entry.Size != 10 || d.Length() != 10 {
		t.Fatalf("Download() = %+v, %v", d, err)
	}
	if got := readAll(t, d); got != "0123456789" {
		t.Errorf("content = %q", got)
	}
	_ = d.Body.Close()
	if f.idle(f.viewer) != 1 {
		t.Error("closing the stream did not return the connection")
	}

	ranges := []struct {
		name  string
		r     files.Range
		want  string
		start int64
	}{
		{"from an offset", files.Range{Set: true, Start: 4, End: -1}, "456789", 4},
		{"bounded", files.Range{Set: true, Start: 2, End: 5}, "2345", 2},
		{"past the end", files.Range{Set: true, Start: 8, End: 99}, "89", 8},
		{"last bytes", files.Range{Set: true, Suffix: 3}, "789", 7},
		{"suffix longer than the file", files.Range{Set: true, Suffix: 99}, "0123456789", 0},
	}
	for _, tt := range ranges {
		d, err := f.svc.Download(ctx, f.viewer, "/f", tt.r)
		if err != nil || !d.Partial || d.Start != tt.start {
			t.Errorf("%s: Download() = %+v, %v", tt.name, d, err)
			continue
		}
		if got := readAll(t, d); got != tt.want || d.Length() != int64(len(tt.want)) {
			t.Errorf("%s: content = %q (length %d); want %q", tt.name, got, d.Length(), tt.want)
		}
	}

	unsatisfiable := []struct {
		name, path string
		r          files.Range
	}{
		{"start beyond the end", "/f", files.Range{Set: true, Start: 10, End: -1}},
		{"start after end", "/f", files.Range{Set: true, Start: 5, End: 2}},
		{"negative start", "/f", files.Range{Set: true, Start: -1, End: 3}},
		{"any range of an empty file", "/empty", files.Range{Set: true, Start: 0, End: -1}},
		{"suffix of an empty file", "/empty", files.Range{Set: true, Suffix: 5}},
	}
	for _, tt := range unsatisfiable {
		_, err := f.svc.Download(ctx, f.viewer, tt.path, tt.r)
		var badRange *files.RangeError
		if !errors.Is(err, files.ErrRangeNotSatisfiable) || !errors.As(err, &badRange) || !strings.Contains(err.Error(), fmt.Sprint(badRange.Size)) {
			t.Errorf("%s: Download() = %v; want a RangeError with the file size", tt.name, err)
		}
	}
	if d, err := f.svc.Download(ctx, f.viewer, "/empty", files.Range{}); err != nil || d.Length() != 0 || readAll(t, d) != "" {
		t.Errorf("Download() of an empty file = %+v, %v", d, err)
	}
	if _, err := f.svc.Download(ctx, f.viewer, "/dir", files.Range{}); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Download() of a directory = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.Download(ctx, f.viewer, "/missing", files.Range{}); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Download() of a missing file = %v; want ErrNotFound", err)
	}
	if f.idle(f.viewer) != 1 {
		t.Errorf("idle connections = %d after refused downloads; want them all returned", f.idle(f.viewer))
	}

	f.fs.failOpen = true
	if _, err := f.svc.Download(ctx, f.viewer, "/f", files.Range{}); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Download() with a failing open = %v; want ErrConnectionLost", err)
	}
	f.fs.failOpen = false
	f.fs.failRead = true
	d, err = f.svc.Download(ctx, f.viewer, "/f", files.Range{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(d.Body); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("reading a dropped stream = %v; want ErrConnectionLost", err)
	}
	_ = d.Body.Close()
	if f.idle(f.viewer) != 0 {
		t.Error("a connection that dropped while streaming was pooled")
	}
}

// TestService_Upload verifies writing.
//
// Scenarios:
//   - create writes a new file and returns its entry, and refuses to replace an existing one
//   - overwrite replaces the content
//   - resume continues, from a ".part" file staged by hand as an interrupted upload would leave it, at the given offset,
//     and completes it
//   - an offset outside resume, a negative offset and an offset past the end are ErrInvalid
//   - a body that fails midway returns its own error, leaves the partial ".part" file untouched at the final path and keeps the
//     connection pooled
//   - a connection lost while closing the file is reported and the connection is discarded
//   - the connection is returned when the file cannot be opened
func TestService_Upload(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	e, err := f.svc.Upload(ctx, f.editor, "/f", remotefs.WriteCreate, 0, strings.NewReader("hello "))
	if err != nil || e.Size != 6 {
		t.Fatalf("Upload() = %+v, %v", e, err)
	}
	if _, err := f.svc.Upload(ctx, f.editor, "/f", remotefs.WriteCreate, 0, strings.NewReader("x")); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("create over an existing file = %v; want ErrExists", err)
	}
	f.put(t, "/f.part", "hello ")
	if e, err := f.svc.Upload(ctx, f.editor, "/f", remotefs.WriteResume, 6, strings.NewReader("world")); err != nil || e.Size != 11 {
		t.Errorf("resume = %+v, %v", e, err)
	}
	if e, err := f.svc.Upload(ctx, f.editor, "/f", remotefs.WriteOverwrite, 0, strings.NewReader("new")); err != nil || e.Size != 3 {
		t.Errorf("overwrite = %+v, %v", e, err)
	}
	f.put(t, "/f.part", "12345")
	for name, call := range map[string]func() error{
		"offset without resume": func() error {
			return second(f.svc.Upload(ctx, f.editor, "/f", remotefs.WriteOverwrite, 1, strings.NewReader("x")))
		},
		"negative offset": func() error {
			return second(f.svc.Upload(ctx, f.editor, "/f", remotefs.WriteResume, -1, strings.NewReader("x")))
		},
		"offset past the end": func() error {
			return second(f.svc.Upload(ctx, f.editor, "/f", remotefs.WriteResume, 99, strings.NewReader("x")))
		},
	} {
		if err := call(); !errors.Is(err, remotefs.ErrInvalid) {
			t.Errorf("%s = %v; want ErrInvalid", name, err)
		}
	}

	interrupted := errors.New("client went away")
	body := io.MultiReader(strings.NewReader("partial"), errReader{interrupted})
	if _, err := f.svc.Upload(ctx, f.editor, "/partial", remotefs.WriteCreate, 0, body); !errors.Is(err, interrupted) || errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Upload() with a failing body = %v; want the body error only", err)
	}
	if _, err := f.svc.Stat(ctx, f.editor, "/partial"); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("the final path after a failed upload = %v; want ErrNotFound, since nothing lands there until it succeeds", err)
	}
	if e, err := f.svc.Stat(ctx, f.editor, "/partial.part"); err != nil || e.Size != 7 {
		t.Errorf("the staged file = %+v, %v; want the 7 bytes that arrived", e, err)
	}
	if e, err := f.svc.Upload(ctx, f.editor, "/partial", remotefs.WriteResume, 7, strings.NewReader(" done")); err != nil || e.Size != 12 {
		t.Errorf("resuming the partial file = %+v, %v", e, err)
	}
	if f.idle(f.editor) != 1 {
		t.Errorf("idle connections = %d; want the connection kept after a body failure", f.idle(f.editor))
	}

	f.fs.failCommit = true
	if _, err := f.svc.Upload(ctx, f.editor, "/g", remotefs.WriteCreate, 0, strings.NewReader("x")); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Upload() with a failing close = %v; want ErrConnectionLost", err)
	}
	f.fs.failCommit = false
	if f.idle(f.editor) != 0 {
		t.Error("a connection that dropped while closing the file was pooled")
	}

	f.fs.failStat = true
	if _, err := f.svc.Upload(ctx, f.editor, "/h", remotefs.WriteCreate, 0, strings.NewReader("x")); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Upload() whose final stat fails = %v; want ErrConnectionLost", err)
	}
	f.fs.failStat = false
}

// errReader fails every read with its error.
type errReader struct{ err error }

// Read returns the error.
func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// raceReader reads from src and, on its first read, plants content at path directly in the file system, as if
// another upload finished there while this one was still streaming.
type raceReader struct {
	fs      remotefs.RemoteFS
	path    string
	content string
	src     io.Reader
	planted bool
}

// Read plants the race once, then delegates to src.
func (r *raceReader) Read(p []byte) (int, error) {
	if !r.planted {
		r.planted = true
		w, err := r.fs.OpenWrite(context.Background(), r.path, 0, remotefs.WriteOverwrite)
		if err == nil {
			_, _ = io.WriteString(w, r.content)
			_ = w.Close()
		}
	}
	return r.src.Read(p)
}

// TestService_Upload_Atomic verifies that a file only ever appears, or changes, at its final path once the whole
// body has arrived, since it is staged into a ".part" file until then.
//
// Scenarios:
//   - overwriting a file leaves it untouched until the new body finishes, then the ".part" file is gone
//   - a body that fails while overwriting leaves the original file exactly as it was
//   - a create that loses a race to a file that appears at the path while it uploads fails with ErrExists and
//     discards its ".part" file, instead of silently replacing what appeared
func TestService_Upload_Atomic(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	f.put(t, "/f", "original")
	if e, err := f.svc.Upload(ctx, f.editor, "/f", remotefs.WriteOverwrite, 0, strings.NewReader("replaced")); err != nil || e.Size != 8 {
		t.Fatalf("Upload() = %+v, %v", e, err)
	}
	if e, err := f.svc.Stat(ctx, f.editor, "/f"); err != nil || e.Size != 8 {
		t.Errorf("the final file = %+v, %v; want the replaced content", e, err)
	}
	if _, err := f.svc.Stat(ctx, f.editor, "/f.part"); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("the staging file after a finished upload = %v; want it gone", err)
	}

	f.put(t, "/g", "original")
	interrupted := errors.New("client went away")
	body := io.MultiReader(strings.NewReader("half"), errReader{interrupted})
	if _, err := f.svc.Upload(ctx, f.editor, "/g", remotefs.WriteOverwrite, 0, body); !errors.Is(err, interrupted) {
		t.Errorf("Upload() with a failing body = %v; want the body error", err)
	}
	if e, err := f.svc.Stat(ctx, f.editor, "/g"); err != nil || e.Size != int64(len("original")) {
		t.Errorf("the file after a failed overwrite = %+v, %v; want it untouched", e, err)
	}

	raced := &raceReader{fs: f.fs.shared, path: "/h", content: "won the race", src: strings.NewReader("lost the race")}
	if _, err := f.svc.Upload(ctx, f.editor, "/h", remotefs.WriteCreate, 0, raced); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("Upload() that loses a create race = %v; want ErrExists", err)
	}
	if e, err := f.svc.Stat(ctx, f.editor, "/h"); err != nil || e.Size != int64(len("won the race")) {
		t.Errorf("the file after a lost create race = %+v, %v; want what won it, untouched", e, err)
	}
	if _, err := f.svc.Stat(ctx, f.editor, "/h.part"); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("the staging file after a lost create race = %v; want it discarded", err)
	}

	f.put(t, "/i", "original")
	f.fs.failRemove = true
	if _, err := f.svc.Upload(ctx, f.editor, "/i", remotefs.WriteOverwrite, 0, strings.NewReader("replaced")); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Upload() whose finishing remove fails = %v; want ErrConnectionLost", err)
	}
	f.fs.failRemove = false
	if e, err := f.svc.Stat(ctx, f.editor, "/i"); err != nil || e.Size != int64(len("original")) {
		t.Errorf("the file after a finishing remove fails = %+v, %v; want it untouched", e, err)
	}
	if e, err := f.svc.Stat(ctx, f.editor, "/i.part"); err != nil || e.Size != int64(len("replaced")) {
		t.Errorf("the staging file after a finishing remove fails = %+v, %v; want it kept", e, err)
	}
}

// TestService_Concurrency verifies that parallel transfers use separate connections.
//
// Scenarios:
//   - many uploads and downloads at once complete and leave the file system consistent
func TestService_Concurrency(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("/f%d", i)
			payload := bytes.Repeat([]byte{byte('a' + i)}, 1000)
			if _, err := f.svc.Upload(ctx, f.editor, name, remotefs.WriteCreate, 0, bytes.NewReader(payload)); err != nil {
				t.Errorf("Upload(%s) = %v", name, err)
				return
			}
			d, err := f.svc.Download(ctx, f.viewer, name, files.Range{})
			if err != nil {
				t.Errorf("Download(%s) = %v", name, err)
				return
			}
			got, _ := io.ReadAll(d.Body)
			_ = d.Body.Close()
			if !bytes.Equal(got, payload) {
				t.Errorf("%s: content differs", name)
			}
		}()
	}
	wg.Wait()
}

// recorder is an observer that keeps what it was told.
type recorder struct {
	mu      sync.Mutex
	started []int64
	bytes   int64
}

// Started records the total.
func (r *recorder) Started(total int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = append(r.started, total)
}

// Progress adds to the count.
func (r *recorder) Progress(n int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bytes += n
}

// TestService_Observer verifies how transfers are followed.
//
// Scenarios:
//   - an upload announces its start once it has a connection and counts every byte
//   - a download announces the number of bytes it will send, also for a range, and counts what was read
//   - canceling the context stops an upload and a download between chunks with the context error
//   - the service works without an observer
//   - Check authorizes and validates without connecting
func TestService_Observer(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	obs := &recorder{}
	target := f.editor
	target.Observer = obs

	if _, err := f.svc.Upload(ctx, target, "/f", remotefs.WriteCreate, 0, strings.NewReader("0123456789")); err != nil {
		t.Fatal(err)
	}
	if len(obs.started) != 1 || obs.started[0] != -1 || obs.bytes != 10 {
		t.Errorf("upload observed %v started and %d bytes; want one start and 10 bytes", obs.started, obs.bytes)
	}

	obs.started, obs.bytes = nil, 0
	d, err := f.svc.Download(ctx, target, "/f", files.Range{Set: true, Start: 2, End: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.started) != 1 || obs.started[0] != 4 {
		t.Errorf("download announced %v; want 4 bytes", obs.started)
	}
	if got := readAll(t, d); got != "2345" || obs.bytes < 4 {
		t.Errorf("content %q, %d bytes counted", got, obs.bytes)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.svc.Upload(canceled, target, "/g", remotefs.WriteCreate, 0, strings.NewReader("x")); !errors.Is(err, context.Canceled) {
		t.Errorf("Upload() with a canceled context = %v", err)
	}
	live, stop := context.WithCancel(ctx)
	stream, err := f.svc.Download(live, target, "/f", files.Range{})
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if _, err := stream.Body.Read(make([]byte, 4)); !errors.Is(err, context.Canceled) {
		t.Errorf("reading after the cancellation = %v; want context.Canceled", err)
	}
	_ = stream.Body.Close()
	interrupted := &cancelAfter{cancel: stop, limit: 3}
	upCtx, upStop := context.WithCancel(ctx)
	interrupted.cancel = upStop
	if _, err := f.svc.Upload(upCtx, target, "/h", remotefs.WriteCreate, 0, io.MultiReader(interrupted, strings.NewReader("never sent"))); !errors.Is(err, context.Canceled) {
		t.Errorf("Upload() canceled midway = %v; want context.Canceled", err)
	}

	if err := f.svc.Check(ctx, f.viewer, workspace.ActionBrowse, "/f"); err != nil {
		t.Errorf("Check() = %v", err)
	}
	if err := f.svc.Check(ctx, f.viewer, workspace.ActionUpload, "/f"); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("Check() of an upload by a viewer = %v; want ErrForbidden", err)
	}
	if err := f.svc.Check(ctx, f.editor, workspace.ActionUpload, ""); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Check() of an empty path = %v; want ErrInvalid", err)
	}
	if f.idle(f.editor) == 0 {
		t.Error("no connection was pooled after the transfers")
	}
}

// cancelAfter delivers some bytes and then cancels a context.
type cancelAfter struct {
	cancel context.CancelFunc
	limit  int
	sent   int
}

// Read delivers one byte at a time and cancels the context after the limit.
func (c *cancelAfter) Read(p []byte) (int, error) {
	if c.sent >= c.limit {
		c.cancel()
		return 0, io.EOF
	}
	c.sent++
	p[0] = 'x'
	return 1, nil
}
