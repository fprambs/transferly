package s3fs_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/s3fs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/s3fs/s3test"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// dial connects to the bucket of a test server with an access key, and closes the file system when the test ends.
func dial(t *testing.T, s *s3test.Server) *s3fs.FS {
	t.Helper()
	fsys, err := s3fs.Dial(t.Context(), s3fs.Config{Endpoint: s.Addr, Secure: true, RootCAs: s.RootCAs, Bucket: s3test.Bucket, AccessKeyID: "key", SecretAccessKey: "secret"})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	t.Cleanup(func() { _ = fsys.Close() })
	return fsys
}

// TestConformance runs the shared RemoteFS suite against the S3 adapter and an in-memory server.
func TestConformance(t *testing.T) {
	remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS { return dial(t, s3test.Start(t, s3test.Options{})) })
}

// request names a kind of request the adapter sends, by what it matches.
type request func(r *http.Request) bool

// Requests the faults below aim at. Endpoints on an address use path-style URLs: /bucket/key.
var (
	headBucket = func(r *http.Request) bool {
		return r.Method == http.MethodHead && strings.Trim(r.URL.Path, "/") == s3test.Bucket
	}
	listing     = func(r *http.Request) bool { return r.Method == http.MethodGet && r.URL.Query().Has("list-type") }
	multiDelete = func(r *http.Request) bool { return r.Method == http.MethodPost && r.URL.Query().Has("delete") }
	copying     = func(r *http.Request) bool {
		return r.Method == http.MethodPut && r.Header.Get("X-Amz-Copy-Source") != ""
	}
)

// on matches requests of method on the object at key.
func on(method, key string) request {
	return func(r *http.Request) bool {
		return r.Method == method && r.URL.Path == "/"+s3test.Bucket+"/"+key && r.Header.Get("X-Amz-Copy-Source") == ""
	}
}

// faulty is a test server whose requests fail once a rule is armed.
type faulty struct {
	*s3test.Server
	mu    sync.Mutex
	match request
	skip  int
	fault s3test.Fault
}

// newFaulty starts a test server that fails what its armed rule matches.
func newFaulty(t *testing.T) *faulty {
	f := &faulty{}
	f.Server = s3test.Start(t, s3test.Options{Fault: f.check})
	return f
}

// arm makes the requests match matches fail with an S3 error, after letting the first skip of them through.
func (f *faulty) arm(match request, skip int, status int, code string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.match, f.skip, f.fault = match, skip, s3test.Fault{Status: status, Code: code}
}

// check answers a request with the armed fault when it matches.
func (f *faulty) check(r *http.Request) *s3test.Fault {
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

// TestDial verifies connecting to buckets.
//
// Use cases:
//   - a public bucket is reached without credentials
//   - an endpoint the client cannot use is invalid, a bucket that does not exist is not found, and refused access is a permission error
//   - a server that keeps failing is a lost connection, after the client tried again
func TestDial(t *testing.T) {
	server := newFaulty(t)
	config := s3fs.Config{Endpoint: server.Addr, Secure: true, RootCAs: server.RootCAs, Bucket: s3test.Bucket}
	if fsys, err := s3fs.Dial(t.Context(), config); err != nil {
		t.Errorf("Dial() without credentials = %v", err)
	} else {
		_ = fsys.Close()
	}

	bad := config
	bad.Endpoint = "https://" + server.Addr + "/path"
	if _, err := s3fs.Dial(t.Context(), bad); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Dial() to an endpoint with a scheme and a path = %v, want ErrInvalid", err)
	}
	absent := config
	absent.Bucket = "absent"
	if _, err := s3fs.Dial(t.Context(), absent); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Dial() to a missing bucket = %v, want ErrNotFound", err)
	}
	server.arm(headBucket, 0, http.StatusForbidden, "")
	if _, err := s3fs.Dial(t.Context(), config); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("Dial() refused = %v, want ErrPermission", err)
	}
	server.arm(headBucket, 0, http.StatusServiceUnavailable, "")
	if _, err := s3fs.Dial(t.Context(), config); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Dial() to a failing server = %v, want ErrConnectionLost", err)
	}
}

// TestFolders verifies folders the adapter did not create and paths S3 cannot hold.
//
// Use cases:
//   - keys written by another tool, with no folder markers, make folders that list and stat; nested folders list their own entries
//   - the root stats as a folder, and relative paths and names that are not UTF-8 are invalid
//   - permissions and times cannot be changed
func TestFolders(t *testing.T) {
	server := s3test.Start(t, s3test.Options{})
	fsys := dial(t, server)
	if _, err := server.Backend.PutObject(s3test.Bucket, "photos/2026/beach.jpg", map[string]string{}, strings.NewReader("jpg"), 3, nil); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"/photos", "/photos/2026"} {
		if e, err := fsys.Stat(t.Context(), p); err != nil || !e.IsDir() || !e.ModTime.IsZero() {
			t.Errorf("Stat(%s) = %+v, %v, want a folder with no time", p, e, err)
		}
	}
	if entries, err := fsys.List(t.Context(), "/photos/2026"); err != nil || len(entries) != 1 || entries[0].Path != "/photos/2026/beach.jpg" || entries[0].Size != 3 {
		t.Errorf("List(/photos/2026) = %+v, %v, want beach.jpg", entries, err)
	}
	if root, err := fsys.Stat(t.Context(), "/"); err != nil || !root.IsDir() || root.Path != "/" {
		t.Errorf("Stat(/) = %+v, %v, want the root folder", root, err)
	}
	if _, err := fsys.List(t.Context(), "photos"); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("List(relative) = %v, want ErrInvalid", err)
	}
	if _, err := fsys.Stat(t.Context(), "/\xff"); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Stat(not UTF-8) = %v, want ErrInvalid", err)
	}
	if err := fsys.Chmod(t.Context(), "/photos", 0o700); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Chmod() = %v, want ErrInvalid", err)
	}
	if err := fsys.Chtimes(t.Context(), "/photos", time.Now()); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Chtimes() = %v, want ErrInvalid", err)
	}
}

// TestWrites verifies what S3 cannot write and writes the service refuses.
//
// Use cases:
//   - the root cannot be written and writes cannot resume
//   - an upload the service refuses fails when it is closed, and a write after closing fails
//   - an upload whose context ends before it is closed is abandoned, and the object keeps what it had
func TestWrites(t *testing.T) {
	server := newFaulty(t)
	fsys := dial(t, server.Server)

	if _, err := fsys.OpenWrite(t.Context(), "/", 0, remotefs.WriteOverwrite); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("OpenWrite(/) = %v, want ErrInvalid", err)
	}
	if _, err := fsys.OpenWrite(t.Context(), "/f", 3, remotefs.WriteResume); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("OpenWrite(resume) = %v, want ErrInvalid", err)
	}

	server.arm(on(http.MethodPut, "f"), 0, http.StatusForbidden, "AccessDenied")
	w, err := fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatalf("OpenWrite() = %v", err)
	}
	_, _ = io.WriteString(w, "refused")
	if err := w.Close(); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("Close() of a refused upload = %v, want ErrPermission", err)
	}
	if _, err := w.Write([]byte("late")); err == nil {
		t.Error("Write() after Close() succeeded")
	}

	server.arm(nil, 0, 0, "")
	write(t, fsys, "/kept", "original")
	ctx, cancel := context.WithCancel(t.Context())
	w, err = fsys.OpenWrite(ctx, "/kept", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatalf("OpenWrite() = %v", err)
	}
	_, _ = io.WriteString(w, "half")
	cancel()
	if err := w.Close(); !errors.Is(err, context.Canceled) {
		t.Errorf("Close() of an abandoned upload = %v, want context.Canceled", err)
	}
	r, err := fsys.OpenRead(t.Context(), "/kept", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if got, _ := io.ReadAll(r); string(got) != "original" {
		t.Errorf("object after an abandoned upload = %q, want the original", got)
	}
}

// TestRefusals verifies that every operation reports the failures of the requests it depends on.
//
// Use cases:
//   - stats of keys, listings, reads, uploads, folder markers, deletions and copies the service refuses fail with
//     its refusal, at each step of each operation
func TestRefusals(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, fsys remotefs.RemoteFS)
		match   request
		skip    int
		run     func(ctx context.Context, fsys remotefs.RemoteFS) error
	}{
		{"stat of a key", nil, on(http.MethodHead, "f"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.Stat(ctx, "/f")) }},
		{"stat of a folder", nil, listing, 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.Stat(ctx, "/d")) }},
		{"list, looking up the folder", nil, on(http.MethodHead, "d"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.List(ctx, "/d")) }},
		{"list of the root", nil, listing, 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.List(ctx, "/")) }},
		{"read, looking up the file", nil, on(http.MethodHead, "f"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.OpenRead(ctx, "/f", 0)) }},
		{"read of the content", put("/f"), on(http.MethodGet, "f"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			r, err := fsys.OpenRead(ctx, "/f", 0)
			if err != nil {
				return err
			}
			defer func() { _ = r.Close() }()
			_, err = io.ReadAll(r)
			return err
		}},
		{"write, looking up the folder", nil, on(http.MethodHead, "d"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			return second(fsys.OpenWrite(ctx, "/d/f", 0, remotefs.WriteOverwrite))
		}},
		{"write, looking up the file", nil, on(http.MethodHead, "f"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			return second(fsys.OpenWrite(ctx, "/f", 0, remotefs.WriteCreate))
		}},
		{"mkdir, looking up the folder", nil, on(http.MethodHead, "a"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Mkdir(ctx, "/a") }},
		{"mkdir, looking up the parent", nil, on(http.MethodHead, "a"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Mkdir(ctx, "/a/b") }},
		{"mkdir, writing the marker", nil, on(http.MethodPut, "a/"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Mkdir(ctx, "/a") }},
		{"remove, looking up", nil, on(http.MethodHead, "f"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Remove(ctx, "/f") }},
		{"remove, checking the folder is empty", mkdirs("/d"), listing, 1, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Remove(ctx, "/d") }},
		{"remove of the object", put("/f"), on(http.MethodDelete, "f"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Remove(ctx, "/f") }},
		{"remove all, looking up", nil, on(http.MethodHead, "f"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.RemoveAll(ctx, "/f") }},
		{"remove all, listing the tree", put("/f"), listing, 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.RemoveAll(ctx, "/f") }},
		{"remove all, deleting", steps(mkdirs("/d"), put("/d/a"), put("/d/b")), multiDelete, 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.RemoveAll(ctx, "/d") }},
		{"rename, looking up the source", nil, on(http.MethodHead, "f"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/g") }},
		{"rename, looking up the target", put("/f"), on(http.MethodHead, "g"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/g") }},
		{"rename, looking up the target folder", put("/f"), on(http.MethodHead, "d"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/d/g") }},
		{"rename, copying", put("/f"), copying, 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/g") }},
		{"ping", nil, headBucket, 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Ping(ctx) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newFaulty(t)
			fsys := dial(t, server.Server)
			if tc.prepare != nil {
				tc.prepare(t, fsys)
			}
			server.arm(tc.match, tc.skip, http.StatusForbidden, "AccessDenied")
			if err := tc.run(t.Context(), fsys); !errors.Is(err, remotefs.ErrPermission) {
				t.Errorf("%s = %v, want ErrPermission", tc.name, err)
			}
		})
	}
	server := s3test.Start(t, s3test.Options{})
	fsys := dial(t, server)
	if err := fsys.Rename(t.Context(), "/f", "relative"); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Rename() to a relative path = %v, want ErrInvalid", err)
	}
}

// put returns a preparation that writes a file at p.
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

// steps runs preparations in order.
func steps(all ...func(t *testing.T, fsys remotefs.RemoteFS)) func(t *testing.T, fsys remotefs.RemoteFS) {
	return func(t *testing.T, fsys remotefs.RemoteFS) {
		for _, step := range all {
			step(t, fsys)
		}
	}
}

// second returns the error of a call that also returns a value.
func second[T any](_ T, err error) error { return err }
