package azfilefs_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/azfilefs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/azure"
	"github.com/fprambs/transferly/internal/adapters/remotefs/azuretest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// dial opens the share of a fake with the shared key, and closes it when the test ends.
func dial(t *testing.T, s *azuretest.Server) *azfilefs.FS {
	t.Helper()
	fsys, err := azfilefs.Dial(t.Context(), azfilefs.Config{
		Service: azure.Config{Endpoint: s.Endpoint(), Account: azuretest.Account, Key: azuretest.Key, Client: &http.Client{}},
		Share:   azuretest.Share,
	})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	t.Cleanup(func() { _ = fsys.Close() })
	return fsys
}

// TestConformance runs the shared RemoteFS suite against the Files adapter and a fake of Azure Storage.
func TestConformance(t *testing.T) {
	remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS { return dial(t, azuretest.StartFiles(t, azuretest.Options{})) })
}

// request names a kind of request the adapter sends, by what it matches.
type request func(r *http.Request) bool

// on matches requests of a method on a path of the share, with the restype and comp given.
func on(method, name, restype, comp string) request {
	return func(r *http.Request) bool {
		q := r.URL.Query()
		return r.Method == method && strings.TrimSuffix(r.URL.Path, "/") == strings.TrimSuffix("/"+azuretest.Account+"/"+azuretest.Share+"/"+name, "/") &&
			q.Get("restype") == restype && q.Get("comp") == comp
	}
}

// faulty is a fake whose requests fail once a rule is armed.
type faulty struct {
	*azuretest.Server
	mu    sync.Mutex
	match request
	skip  int
}

// newFaulty starts a fake of the Files service that fails what its armed rule matches.
func newFaulty(t *testing.T) *faulty {
	f := &faulty{}
	f.Server = azuretest.StartFiles(t, azuretest.Options{Fault: f.check})
	return f
}

// arm makes the requests match matches fail as refused, after letting the first skip of them through.
func (f *faulty) arm(match request, skip int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.match, f.skip = match, skip
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
	return &azuretest.Fault{Status: http.StatusForbidden, Code: "AuthorizationPermissionMismatch"}
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

// TestDial verifies connecting to shares.
//
// Use cases:
//   - a SAS token reaches the share; no credential is refused, a key that is not base64 is invalid, a share that does not exist is
//     not found
func TestDial(t *testing.T) {
	s := azuretest.StartFiles(t, azuretest.Options{})
	open := func(key, sas, share string) error {
		fsys, err := azfilefs.Dial(t.Context(), azfilefs.Config{Service: azure.Config{Endpoint: s.Endpoint(), Account: azuretest.Account, Key: key, SAS: sas, Client: &http.Client{}}, Share: share})
		if err == nil {
			_ = fsys.Close()
		}
		return err
	}
	if err := open("", azuretest.SAS, azuretest.Share); err != nil {
		t.Errorf("Dial() with a SAS token = %v", err)
	}
	for name, tc := range map[string]struct {
		err, want error
	}{
		"anonymous":     {open("", "", azuretest.Share), remotefs.ErrAuth},
		"bad key":       {open("not base64!", "", azuretest.Share), remotefs.ErrInvalid},
		"missing share": {open(azuretest.Key, "", "absent"), remotefs.ErrNotFound},
	} {
		if !errors.Is(tc.err, tc.want) {
			t.Errorf("Dial() %s = %v, want %v", name, tc.err, tc.want)
		}
	}
}

// TestRefusals verifies that every operation reports the failures of the requests it depends on, and refuses what the port does not
// allow.
//
// Use cases:
//   - properties, listings, reads, creations, sizes, ranges, directories, deletions, renames, times and pings the service refuses fail
//     with its refusal, at each step of each operation
//   - relative paths, unknown write modes, the root, permissions, resumes of a missing file or past its end, times of a directory and
//     renames into a missing folder are refused as the port says
//   - an upload whose context ended writes no more, a closed upload writes no more, and closing it again does nothing
func TestRefusals(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, fsys remotefs.RemoteFS)
		match   request
		skip    int
		run     func(ctx context.Context, fsys remotefs.RemoteFS) error
	}{
		{"stat of a file", nil, on(http.MethodHead, "f", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.Stat(ctx, "/f")) }},
		{"stat of a directory", nil, on(http.MethodGet, "d", "directory", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.Stat(ctx, "/d")) }},
		{"list, looking up", nil, on(http.MethodHead, "d", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.List(ctx, "/d")) }},
		{"list of the root", nil, on(http.MethodGet, "", "directory", "list"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.List(ctx, "/")) }},
		{"read, looking up", nil, on(http.MethodHead, "f", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.OpenRead(ctx, "/f", 0)) }},
		{"read of the content", put("/f"), on(http.MethodGet, "f", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.OpenRead(ctx, "/f", 0)) }},
		{"write, looking up the folder", nil, on(http.MethodHead, "d", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			return second(fsys.OpenWrite(ctx, "/d/f", 0, remotefs.WriteCreate))
		}},
		{"write, looking up the file", nil, on(http.MethodHead, "f", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			return second(fsys.OpenWrite(ctx, "/f", 0, remotefs.WriteCreate))
		}},
		{"write, creating the file", nil, on(http.MethodPut, "f", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			return second(fsys.OpenWrite(ctx, "/f", 0, remotefs.WriteCreate))
		}},
		{"write, growing the file", nil, on(http.MethodPut, "f", "", "properties"), 0, closing},
		{"write, writing a range", nil, on(http.MethodPut, "f", "", "range"), 0, closing},
		{"mkdir, looking up", nil, on(http.MethodHead, "a", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Mkdir(ctx, "/a") }},
		{"mkdir, looking up the parent", nil, on(http.MethodHead, "a", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Mkdir(ctx, "/a/b") }},
		{"mkdir", nil, on(http.MethodPut, "a", "directory", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Mkdir(ctx, "/a") }},
		{"remove, looking up", nil, on(http.MethodHead, "f", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Remove(ctx, "/f") }},
		{"remove", put("/f"), on(http.MethodDelete, "f", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Remove(ctx, "/f") }},
		{"remove all, looking up", nil, on(http.MethodHead, "f", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.RemoveAll(ctx, "/f") }},
		{"remove all of a file", put("/f"), on(http.MethodDelete, "f", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.RemoveAll(ctx, "/f") }},
		{"remove all, listing", mkdirs("/d"), on(http.MethodGet, "d", "directory", "list"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.RemoveAll(ctx, "/d") }},
		{"remove all, deleting a file", tree, on(http.MethodDelete, "d/f", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.RemoveAll(ctx, "/d") }},
		{"remove all, deleting a subdirectory", tree, on(http.MethodDelete, "d/e", "directory", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.RemoveAll(ctx, "/d") }},
		{"rename, looking up the source", nil, on(http.MethodHead, "f", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/g") }},
		{"rename, looking up the target", put("/f"), on(http.MethodHead, "g", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/g") }},
		{"rename, looking up the target folder", put("/f"), on(http.MethodHead, "d", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/d/g") }},
		{"rename", put("/f"), on(http.MethodPut, "g", "", "rename"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/g") }},
		{"chtimes, looking up", nil, on(http.MethodHead, "f", "", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Chtimes(ctx, "/f", time.Now()) }},
		{"chtimes", put("/f"), on(http.MethodPut, "f", "", "properties"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Chtimes(ctx, "/f", time.Now()) }},
		{"ping", nil, func(r *http.Request) bool { return r.URL.Query().Get("restype") == "share" }, 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Ping(ctx) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newFaulty(t)
			fsys := dial(t, s.Server)
			if tc.prepare != nil {
				tc.prepare(t, fsys)
			}
			s.arm(tc.match, tc.skip)
			if err := tc.run(t.Context(), fsys); !errors.Is(err, remotefs.ErrPermission) {
				t.Errorf("%s = %v, want ErrPermission", tc.name, err)
			}
		})
	}

	fsys := dial(t, azuretest.StartFiles(t, azuretest.Options{}))
	write(t, fsys, "/f", "content")
	if err := fsys.Mkdir(t.Context(), "/d"); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		err, want error
	}{
		"List(relative)":          {second(fsys.List(t.Context(), "f")), remotefs.ErrInvalid},
		"OpenWrite(unknown mode)": {second(fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteMode(9))), remotefs.ErrInvalid},
		"OpenWrite(root)":         {second(fsys.OpenWrite(t.Context(), "/", 0, remotefs.WriteOverwrite)), remotefs.ErrInvalid},
		"Chmod()":                 {fsys.Chmod(t.Context(), "/f", 0o600), remotefs.ErrInvalid},
		"Chtimes(directory)":      {fsys.Chtimes(t.Context(), "/d", time.Now()), remotefs.ErrNotFound},
		"Rename(to relative)":     {fsys.Rename(t.Context(), "/f", "relative"), remotefs.ErrInvalid},
		"Rename(into missing)":    {fsys.Rename(t.Context(), "/f", "/no/f"), remotefs.ErrNotFound},
		"Rename(directory)":       {fsys.Rename(t.Context(), "/d", "/e"), nil},
		"RemoveAll(missing)":      {fsys.RemoveAll(t.Context(), "/nope"), nil},
	} {
		if !errors.Is(tc.err, tc.want) {
			t.Errorf("%s = %v, want %v", name, tc.err, tc.want)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	w, _ := fsys.OpenWrite(ctx, "/g", 0, remotefs.WriteOverwrite)
	_, _ = io.WriteString(w, "half")
	cancel()
	if err := w.Close(); !errors.Is(err, context.Canceled) {
		t.Errorf("Close() of an abandoned upload = %v, want context.Canceled", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil", err)
	}
	if _, err := w.Write([]byte("late")); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Write() after Close() = %v, want ErrInvalid", err)
	}
}

// closing opens an upload of a file, writes to it and closes it, returning the first failure.
func closing(ctx context.Context, fsys remotefs.RemoteFS) error {
	w, err := fsys.OpenWrite(ctx, "/f", 0, remotefs.WriteOverwrite)
	if err != nil {
		return err
	}
	_, _ = io.WriteString(w, "content")
	return w.Close()
}

// put returns a preparation that writes a file at p.
func put(p string) func(t *testing.T, fsys remotefs.RemoteFS) {
	return func(t *testing.T, fsys remotefs.RemoteFS) { write(t, fsys, p, "x") }
}

// mkdirs returns a preparation that creates the directories ps, in order.
func mkdirs(ps ...string) func(t *testing.T, fsys remotefs.RemoteFS) {
	return func(t *testing.T, fsys remotefs.RemoteFS) {
		for _, p := range ps {
			if err := fsys.Mkdir(t.Context(), p); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// tree prepares a directory d with a file and a subdirectory.
func tree(t *testing.T, fsys remotefs.RemoteFS) {
	mkdirs("/d", "/d/e")(t, fsys)
	write(t, fsys, "/d/f", "x")
}

// second returns the error of a call that also returns a value.
func second[T any](_ T, err error) error { return err }
