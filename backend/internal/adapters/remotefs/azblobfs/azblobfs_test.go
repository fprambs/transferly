package azblobfs_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/azblobfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/azure"
	"github.com/fprambs/transferly/internal/adapters/remotefs/azuretest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// dial opens the container of a fake with the shared key, and closes it when the test ends.
func dial(t *testing.T, s *azuretest.Server) *azblobfs.FS {
	t.Helper()
	fsys, err := azblobfs.Dial(t.Context(), azblobfs.Config{
		Service:   azure.Config{Endpoint: s.Endpoint(), Account: azuretest.Account, Key: azuretest.Key, Client: &http.Client{}},
		Container: azuretest.Container,
	})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	t.Cleanup(func() { _ = fsys.Close() })
	return fsys
}

// TestConformance runs the shared RemoteFS suite against the Blob adapter and a fake of Azure Storage.
func TestConformance(t *testing.T) {
	remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS { return dial(t, azuretest.StartBlob(t, azuretest.Options{})) })
}

// request names a kind of request the adapter sends, by what it matches.
type request func(r *http.Request) bool

// on matches requests of a method on a blob of the container, without query unless comp is given.
func on(method, name, comp string) request {
	return func(r *http.Request) bool {
		return r.Method == method && strings.HasSuffix(r.URL.Path, "/"+azuretest.Container+"/"+name) && r.URL.Query().Get("comp") == comp && r.Header.Get("x-ms-copy-source") == ""
	}
}

// Requests the faults below aim at.
var (
	listing   = func(r *http.Request) bool { return r.URL.Query().Get("comp") == "list" }
	container = func(r *http.Request) bool {
		return r.URL.Query().Get("restype") == "container" && r.URL.Query().Get("comp") == ""
	}
	copying = func(r *http.Request) bool { return r.Header.Get("x-ms-copy-source") != "" }
)

// faulty is a fake whose requests fail once a rule is armed.
type faulty struct {
	*azuretest.Server
	mu    sync.Mutex
	match request
	skip  int
	fault azuretest.Fault
}

// newFaulty starts a fake of the Blob service that fails what its armed rule matches.
func newFaulty(t *testing.T, opt azuretest.Options) *faulty {
	f := &faulty{}
	opt.Fault = f.check
	f.Server = azuretest.StartBlob(t, opt)
	return f
}

// arm makes the requests match matches fail with a code of Azure Storage, after letting the first skip of them through.
func (f *faulty) arm(match request, skip int, status int, code string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.match, f.skip, f.fault = match, skip, azuretest.Fault{Status: status, Code: code}
}

// check answers a request with the armed fault when it matches.
func (f *faulty) check(r *http.Request) *azuretest.Fault {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.match == nil || !f.match(r) {
		return nil
	}
	if f.skip > 0 {
		f.skip--
		return nil
	}
	fault := f.fault
	return &fault
}

// open dials the container of a fake with a credential.
func open(t *testing.T, s *azuretest.Server, key, sas, container string) (*azblobfs.FS, error) {
	return azblobfs.Dial(t.Context(), azblobfs.Config{
		Service:   azure.Config{Endpoint: s.Endpoint(), Account: azuretest.Account, Key: key, SAS: sas, Client: &http.Client{}},
		Container: container,
	})
}

// write stores content at p.
func write(t *testing.T, fsys remotefs.RemoteFS, p, content string) {
	t.Helper()
	w, err := fsys.OpenWrite(t.Context(), p, 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatalf("OpenWrite(%s) = %v", p, err)
	}
	_, _ = io.WriteString(w, content)
	if err := w.Close(); err != nil {
		t.Fatalf("Close(%s) = %v", p, err)
	}
}

// read returns the content of p.
func read(t *testing.T, fsys remotefs.RemoteFS, p string) string {
	t.Helper()
	r, err := fsys.OpenRead(t.Context(), p, 0)
	if err != nil {
		t.Fatalf("OpenRead(%s) = %v", p, err)
	}
	defer func() { _ = r.Close() }()
	data, _ := io.ReadAll(r)
	return string(data)
}

// TestDial verifies connecting to containers with each credential.
//
// Use cases:
//   - a SAS token, and anonymous access to a public container, reach the container
//   - anonymous access to a private container is refused credentials, a key that is not base64 is invalid, and a container that does
//     not exist is not found
func TestDial(t *testing.T) {
	private := azuretest.StartBlob(t, azuretest.Options{})
	public := azuretest.StartBlob(t, azuretest.Options{Public: true})
	if fsys, err := open(t, private, "", azuretest.SAS, azuretest.Container); err != nil {
		t.Errorf("Dial() with a SAS token = %v", err)
	} else {
		_ = fsys.Close()
	}
	if fsys, err := open(t, public, "", "", azuretest.Container); err != nil {
		t.Errorf("Dial() anonymous to a public container = %v", err)
	} else {
		_ = fsys.Close()
	}
	if _, err := open(t, private, "", "", azuretest.Container); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Dial() anonymous to a private container = %v, want ErrAuth", err)
	}
	if _, err := open(t, private, "not base64!", "", azuretest.Container); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Dial() with a bad key = %v, want ErrInvalid", err)
	}
	if _, err := open(t, private, azuretest.Key, "", "absent"); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Dial() to a missing container = %v, want ErrNotFound", err)
	}
}

// TestFolders verifies folders the adapter did not create and what blobs cannot do.
//
// Use cases:
//   - blobs written by another tool, with no folder markers, make folders that list and stat; nested folders list their own entries
//   - the root stats as a folder, and relative paths are invalid
//   - permissions and times cannot be changed, writes cannot resume and the root cannot be written
func TestFolders(t *testing.T) {
	server := azuretest.StartBlob(t, azuretest.Options{})
	fsys := dial(t, server)
	server.Put("photos/2026/beach.jpg", []byte("jpg"))
	for _, p := range []string{"/photos", "/photos/2026"} {
		if e, err := fsys.Stat(t.Context(), p); err != nil || !e.IsDir() {
			t.Errorf("Stat(%s) = %+v, %v, want a folder", p, e, err)
		}
	}
	if entries, err := fsys.List(t.Context(), "/photos/2026"); err != nil || len(entries) != 1 || entries[0].Path != "/photos/2026/beach.jpg" || entries[0].Size != 3 {
		t.Errorf("List(/photos/2026) = %+v, %v", entries, err)
	}
	if err := fsys.Mkdir(t.Context(), "/made"); err != nil {
		t.Fatal(err)
	}
	write(t, fsys, "/made/x", "x")
	if entries, err := fsys.List(t.Context(), "/made"); err != nil || len(entries) != 1 || entries[0].Name != "x" {
		t.Errorf("List(/made) = %+v, %v, want x without the marker of the folder", entries, err)
	}
	if root, err := fsys.Stat(t.Context(), "/"); err != nil || !root.IsDir() {
		t.Errorf("Stat(/) = %+v, %v", root, err)
	}
	for name, err := range map[string]error{
		"List(relative)":    second(fsys.List(t.Context(), "photos")),
		"Chmod()":           fsys.Chmod(t.Context(), "/photos", 0o700),
		"Chtimes()":         fsys.Chtimes(t.Context(), "/photos", time.Now()),
		"OpenWrite(resume)": second(fsys.OpenWrite(t.Context(), "/f", 3, remotefs.WriteResume)),
		"OpenWrite(root)":   second(fsys.OpenWrite(t.Context(), "/", 0, remotefs.WriteOverwrite)),
	} {
		if !errors.Is(err, remotefs.ErrInvalid) {
			t.Errorf("%s = %v, want ErrInvalid", name, err)
		}
	}
}

// TestWrites verifies how uploads end.
//
// Use cases:
//   - an upload the service refuses fails when it is closed, a write after closing fails, and closing again does nothing
//   - an upload whose context ends before it is closed is abandoned, and the blob keeps what it had
//   - creating a blob that appeared while it was being written does not replace it
func TestWrites(t *testing.T) {
	server := newFaulty(t, azuretest.Options{})
	fsys := dial(t, server.Server)

	server.arm(on(http.MethodPut, "f", ""), 0, http.StatusForbidden, "AuthorizationPermissionMismatch")
	w, err := fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "refused")
	if err := w.Close(); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("Close() of a refused upload = %v, want ErrPermission", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil", err)
	}
	if _, err := w.Write([]byte("late")); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Write() after Close() = %v, want ErrInvalid", err)
	}

	server.arm(nil, 0, 0, "")
	write(t, fsys, "/kept", "original")
	ctx, cancel := context.WithCancel(t.Context())
	w, _ = fsys.OpenWrite(ctx, "/kept", 0, remotefs.WriteOverwrite)
	_, _ = io.WriteString(w, "half")
	cancel()
	if err := w.Close(); !errors.Is(err, context.Canceled) {
		t.Errorf("Close() of an abandoned upload = %v, want context.Canceled", err)
	}
	if got := read(t, fsys, "/kept"); got != "original" {
		t.Errorf("blob after an abandoned upload = %q", got)
	}

	w, err = fsys.OpenWrite(t.Context(), "/raced", 0, remotefs.WriteCreate)
	if err != nil {
		t.Fatal(err)
	}
	server.Put("raced", []byte("first"))
	_, _ = io.WriteString(w, "second")
	if err := w.Close(); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("Close() of a create that lost a race = %v, want ErrExists", err)
	}
	if got := read(t, fsys, "/raced"); got != "first" {
		t.Errorf("blob after a lost race = %q, want what won it", got)
	}
}

// TestRefusals verifies that every operation reports the failures of the requests it depends on.
//
// Use cases:
//   - properties, listings, reads, uploads, markers, deletions, copies and pings the service refuses fail with its refusal, at each
//     step of each operation
func TestRefusals(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, fsys remotefs.RemoteFS)
		match   request
		skip    int
		run     func(ctx context.Context, fsys remotefs.RemoteFS) error
	}{
		{"stat of a blob", nil, on(http.MethodHead, "f", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.Stat(ctx, "/f")) }},
		{"stat of a folder", nil, listing, 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.Stat(ctx, "/d")) }},
		{"list, looking up the folder", nil, on(http.MethodHead, "d", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.List(ctx, "/d")) }},
		{"list of the root", nil, listing, 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.List(ctx, "/")) }},
		{"read, looking up the blob", nil, on(http.MethodHead, "f", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.OpenRead(ctx, "/f", 0)) }},
		{"read of the content", put("/f"), on(http.MethodGet, "f", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			return second(fsys.OpenRead(ctx, "/f", 0))
		}},
		{"write, looking up the folder", nil, on(http.MethodHead, "d", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			return second(fsys.OpenWrite(ctx, "/d/f", 0, remotefs.WriteOverwrite))
		}},
		{"write, looking up the blob", nil, on(http.MethodHead, "f", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			return second(fsys.OpenWrite(ctx, "/f", 0, remotefs.WriteCreate))
		}},
		{"mkdir, looking up", nil, on(http.MethodHead, "a", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Mkdir(ctx, "/a") }},
		{"mkdir, looking up the parent", nil, on(http.MethodHead, "a", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Mkdir(ctx, "/a/b") }},
		{"mkdir, writing the marker", nil, on(http.MethodPut, "a/", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Mkdir(ctx, "/a") }},
		{"remove, looking up", nil, on(http.MethodHead, "f", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Remove(ctx, "/f") }},
		{"remove, checking the folder is empty", mkdirs("/d"), listing, 1, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Remove(ctx, "/d") }},
		{"remove of the blob", put("/f"), on(http.MethodDelete, "f", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Remove(ctx, "/f") }},
		{"remove all, looking up", nil, on(http.MethodHead, "f", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.RemoveAll(ctx, "/f") }},
		{"remove all, listing the tree", put("/f"), listing, 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.RemoveAll(ctx, "/f") }},
		{"remove all, deleting", put("/f"), on(http.MethodDelete, "f", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.RemoveAll(ctx, "/f") }},
		{"rename, looking up the source", nil, on(http.MethodHead, "f", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/g") }},
		{"rename, looking up the target", put("/f"), on(http.MethodHead, "g", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/g") }},
		{"rename, looking up the target folder", put("/f"), on(http.MethodHead, "d", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/d/g") }},
		{"rename, copying", put("/f"), copying, 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/g") }},
		{"rename, copying a marker", mkdirs("/d"), on(http.MethodPut, "e/", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/d", "/e") }},
		{"ping", nil, container, 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Ping(ctx) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newFaulty(t, azuretest.Options{})
			fsys := dial(t, server.Server)
			if tc.prepare != nil {
				tc.prepare(t, fsys)
			}
			server.arm(tc.match, tc.skip, http.StatusForbidden, "AuthorizationPermissionMismatch")
			if err := tc.run(t.Context(), fsys); !errors.Is(err, remotefs.ErrPermission) {
				t.Errorf("%s = %v, want ErrPermission", tc.name, err)
			}
		})
	}
	server := azuretest.StartBlob(t, azuretest.Options{})
	fsys := dial(t, server)
	write(t, fsys, "/f", "x")
	for name, tc := range map[string]struct {
		err, want error
	}{
		"Rename(to relative)":  {fsys.Rename(t.Context(), "/f", "relative"), remotefs.ErrInvalid},
		"Rename(missing blob)": {fsys.Rename(t.Context(), "/nope", "/x"), remotefs.ErrNotFound},
		"OpenRead(past end)":   {second(fsys.OpenRead(t.Context(), "/f", 9)), remotefs.ErrInvalid},
	} {
		if !errors.Is(tc.err, tc.want) {
			t.Errorf("%s = %v, want %v", name, tc.err, tc.want)
		}
	}
}

// put returns a preparation that writes a blob at p.
func put(p string) func(t *testing.T, fsys remotefs.RemoteFS) {
	return func(t *testing.T, fsys remotefs.RemoteFS) { write(t, fsys, p, "x") }
}

// mkdirs returns a preparation that creates the folders ps, in order.
func mkdirs(ps ...string) func(t *testing.T, fsys remotefs.RemoteFS) {
	return func(t *testing.T, fsys remotefs.RemoteFS) {
		for _, p := range ps {
			if err := fsys.Mkdir(t.Context(), p); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// second returns the error of a call that also returns a value.
func second[T any](_ T, err error) error { return err }
