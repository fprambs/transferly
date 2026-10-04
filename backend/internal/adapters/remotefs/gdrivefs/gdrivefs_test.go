package gdrivefs_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/fprambs/transferly/internal/adapters/remotefs/gdrivefs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/gdrivefs/googletest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// client returns an HTTP client authorized as the account of a fake, refreshing its access token from a refresh token.
func client(s *googletest.Server, refresh string) *http.Client {
	cfg := oauth2.Config{ClientID: googletest.ClientID, ClientSecret: googletest.ClientSecret, Endpoint: oauth2.Endpoint{TokenURL: s.URL + "/token"}}
	return cfg.Client(context.Background(), &oauth2.Token{RefreshToken: refresh})
}

// dial opens My Drive of a fake, or the shared drive named, and closes it when the test ends.
func dial(t *testing.T, s *googletest.Server, shared string) *gdrivefs.FS {
	t.Helper()
	fsys, err := gdrivefs.Dial(t.Context(), gdrivefs.Config{APIURL: s.URL, Client: client(s, s.RefreshToken()), SharedDrive: shared})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	t.Cleanup(func() { _ = fsys.Close() })
	return fsys
}

// TestConformance runs the shared RemoteFS suite against the Drive adapter and a fake of Google, in My Drive and in a shared drive.
func TestConformance(t *testing.T) {
	t.Run("my drive", func(t *testing.T) {
		remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS { return dial(t, googletest.Start(t, googletest.Options{}), "") })
	})
	t.Run("shared drive", func(t *testing.T) {
		remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS {
			s := googletest.Start(t, googletest.Options{})
			s.AddSharedDrive("Team")
			return dial(t, s, "Team")
		})
	})
}

// faulty is a fake of Google whose requests fail once a rule is armed.
type faulty struct {
	*googletest.Server
	mu    sync.Mutex
	match func(r *http.Request) bool
	skip  int
	fault googletest.Fault
}

// newFaulty starts a fake that fails what its armed rule matches.
func newFaulty(t *testing.T) *faulty {
	f := &faulty{}
	f.Server = googletest.Start(t, googletest.Options{Fault: f.check})
	return f
}

// arm makes the requests match matches fail with an error of the Drive API, after letting the first skip of them through.
func (f *faulty) arm(match func(r *http.Request) bool, skip, status int, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.match, f.skip, f.fault = match, skip, googletest.Fault{Status: status, Reason: reason}
}

// check answers a request with the armed fault when it matches.
func (f *faulty) check(r *http.Request) *googletest.Fault {
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

// request matches requests of a method on endpoints that start with prefix and, when given, carry a query parameter.
func request(method, prefix, param string) func(r *http.Request) bool {
	return func(r *http.Request) bool {
		return r.Method == method && strings.HasPrefix(r.URL.Path, prefix) && (param == "" || r.URL.Query().Has(param))
	}
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

// read returns the content of p from offset.
func read(t *testing.T, fsys remotefs.RemoteFS, p string, offset int64) string {
	t.Helper()
	r, err := fsys.OpenRead(t.Context(), p, offset)
	if err != nil {
		t.Fatalf("OpenRead(%s) = %v", p, err)
	}
	defer func() { _ = r.Close() }()
	data, _ := io.ReadAll(r)
	return string(data)
}

// TestDial verifies opening drives.
//
// Use cases:
//   - a shared drive that does not exist is not found
//   - a token Google revoked is refused credentials
//   - failures to ask who the account is, to find My Drive or to list the shared drives are reported
func TestDial(t *testing.T) {
	s := newFaulty(t)
	if _, err := gdrivefs.Dial(t.Context(), gdrivefs.Config{APIURL: s.URL, Client: client(s.Server, s.RefreshToken()), SharedDrive: "Absent"}); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Dial(missing shared drive) = %v, want ErrNotFound", err)
	}
	if _, err := gdrivefs.Dial(t.Context(), gdrivefs.Config{APIURL: s.URL, Client: client(s.Server, "refresh-revoked")}); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Dial(revoked token) = %v, want ErrAuth", err)
	}
	for name, tc := range map[string]struct {
		match  func(r *http.Request) bool
		shared string
	}{
		"about":         {request(http.MethodGet, "/drive/v3/about", ""), ""},
		"my drive":      {request(http.MethodGet, "/drive/v3/files/root", ""), ""},
		"shared drives": {request(http.MethodGet, "/drive/v3/drives", ""), "Team"},
	} {
		s.arm(tc.match, 0, http.StatusForbidden, "insufficientPermissions")
		if _, err := gdrivefs.Dial(t.Context(), gdrivefs.Config{APIURL: s.URL, Client: client(s.Server, s.RefreshToken()), SharedDrive: tc.shared}); !errors.Is(err, remotefs.ErrPermission) {
			t.Errorf("Dial() refused at %s = %v, want ErrPermission", name, err)
		}
	}
}

// TestGoogleDocuments verifies the files Drive keeps in formats of its own.
//
// Use cases:
//   - Google documents are listed with the extension of their export and no size, and read exported, from an offset too
//   - Google files that cannot be exported, such as forms, are not listed
//   - a Google document cannot be written, and is renamed keeping its extension
func TestGoogleDocuments(t *testing.T) {
	s := googletest.Start(t, googletest.Options{})
	fsys := dial(t, s, "")
	doc := s.Put(s.Root, googletest.File{Name: "Plan", MimeType: googletest.DocumentMime, Content: []byte("draft")})
	s.Put(s.Root, googletest.File{Name: "Survey", MimeType: "application/vnd.google-apps.form"})

	entries, err := fsys.List(t.Context(), "/")
	if err != nil || len(entries) != 1 || entries[0].Name != "Plan.docx" || entries[0].Size != 0 || entries[0].IsDir() {
		t.Fatalf("List(/) = %+v, %v, want only Plan.docx with no size", entries, err)
	}
	const docx = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	if got := read(t, fsys, "/Plan.docx", 0); got != docx+":draft" {
		t.Errorf("read of a document = %q, want its export", got)
	}
	if got := read(t, fsys, "/Plan.docx", int64(len(docx)+1)); got != "draft" {
		t.Errorf("read of a document from an offset = %q, want the rest of its export", got)
	}
	if _, err := fsys.OpenWrite(t.Context(), "/Plan.docx", 0, remotefs.WriteOverwrite); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("OpenWrite(document) = %v, want ErrInvalid", err)
	}
	if err := fsys.Rename(t.Context(), "/Plan.docx", "/Plan.txt"); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Rename(document to another extension) = %v, want ErrInvalid", err)
	}
	if err := fsys.Rename(t.Context(), "/Plan.docx", "/Plan 2026.docx"); err != nil {
		t.Fatalf("Rename(document) = %v", err)
	}
	if f, _ := s.Get(doc); f.Name != "Plan 2026" {
		t.Errorf("renamed document = %q, want Plan 2026 without the extension", f.Name)
	}
}

// TestNames verifies files of the same name and folders of many pages.
//
// Use cases:
//   - of two files of the same name, the oldest is listed and read
//   - a folder whose listing takes several pages lists every file
//   - names with quotes and backslashes are found
func TestNames(t *testing.T) {
	s := googletest.Start(t, googletest.Options{PageSize: 2})
	fsys := dial(t, s, "")
	s.Put(s.Root, googletest.File{Name: "same.txt", MimeType: "text/plain", Content: []byte("old")})
	s.Put(s.Root, googletest.File{Name: "same.txt", MimeType: "text/plain", Content: []byte("new")})
	s.Put(s.Root, googletest.File{Name: `it's \ odd`, MimeType: "text/plain", Content: []byte("x")})

	entries, err := fsys.List(t.Context(), "/")
	if err != nil || len(entries) != 2 {
		t.Fatalf("List(/) = %+v, %v, want same.txt once and the odd name", entries, err)
	}
	if got := read(t, fsys, "/same.txt", 0); got != "old" {
		t.Errorf("read of a duplicate = %q, want the oldest", got)
	}
	if got := read(t, fsys, `/it's \ odd`, 0); got != "x" {
		t.Errorf("read of an odd name = %q", got)
	}
}

// TestRefusals verifies that every operation reports the failures of the requests it depends on, and refuses what the port does not allow.
//
// Use cases:
//   - listings, contents, exports, uploads, their chunks, folders, trash, renames, times and pings Drive refuses fail with its refusal
//   - relative paths, resumes and unknown write modes, writing a folder, permissions, renaming the root or into itself, renaming onto
//     an existing file, into a missing folder or a missing file, and times of a missing file are refused as the port says
//   - an upload whose context ended stores nothing, a closed upload writes no more, and closing it again does nothing
func TestRefusals(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, s *googletest.Server, fsys remotefs.RemoteFS)
		match   func(r *http.Request) bool
		skip    int
		run     func(ctx context.Context, fsys remotefs.RemoteFS) error
	}{
		{"list", nil, request(http.MethodGet, "/drive/v3/files", "q"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.List(ctx, "/")) }},
		{"list of a folder", nil, request(http.MethodGet, "/drive/v3/files", "q"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.List(ctx, "/d")) }},
		{"stat", nil, request(http.MethodGet, "/drive/v3/files", "q"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.Stat(ctx, "/f")) }},
		{"read", put("/f"), request(http.MethodGet, "/drive/v3/files/", "alt"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.OpenRead(ctx, "/f", 0)) }},
		{"read of a missing file", nil, request(http.MethodGet, "/drive/v3/files", "q"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return second(fsys.OpenRead(ctx, "/f", 0)) }},
		{"write, looking up the folder", nil, request(http.MethodGet, "/drive/v3/files", "q"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			return second(fsys.OpenWrite(ctx, "/d/f", 0, remotefs.WriteCreate))
		}},
		{"write, looking up the file", nil, request(http.MethodGet, "/drive/v3/files", "q"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			return second(fsys.OpenWrite(ctx, "/f", 0, remotefs.WriteCreate))
		}},
		{"write, opening the session", nil, request(http.MethodPost, "/upload/drive/v3/files", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			return second(fsys.OpenWrite(ctx, "/f", 0, remotefs.WriteCreate))
		}},
		{"write, sending the content", nil, request(http.MethodPut, "/upload/session/", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			w, err := fsys.OpenWrite(ctx, "/f", 0, remotefs.WriteCreate)
			if err != nil {
				return err
			}
			return w.Close()
		}},
		{"mkdir, looking up", nil, request(http.MethodGet, "/drive/v3/files", "q"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Mkdir(ctx, "/d") }},
		{"mkdir, looking up the parent", nil, request(http.MethodGet, "/drive/v3/files", "q"), 1, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Mkdir(ctx, "/a/b") }},
		{"mkdir", nil, request(http.MethodPost, "/drive/v3/files", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Mkdir(ctx, "/d") }},
		{"remove, looking up", nil, request(http.MethodGet, "/drive/v3/files", "q"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Remove(ctx, "/f") }},
		{"remove, checking the folder is empty", mkdirs("/d"), request(http.MethodGet, "/drive/v3/files", "q"), 1, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Remove(ctx, "/d") }},
		{"remove", put("/f"), request(http.MethodPatch, "/drive/v3/files/", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Remove(ctx, "/f") }},
		{"remove all, looking up", nil, request(http.MethodGet, "/drive/v3/files", "q"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.RemoveAll(ctx, "/f") }},
		{"rename, looking up the source", nil, request(http.MethodGet, "/drive/v3/files", "q"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/g") }},
		{"rename, looking up the target", put("/f"), request(http.MethodGet, "/drive/v3/files", "q"), 1, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/g") }},
		{"rename, looking up the target folder", put("/f"), request(http.MethodGet, "/drive/v3/files", "q"), 2, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/d/g") }},
		{"rename", put("/f"), request(http.MethodPatch, "/drive/v3/files/", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Rename(ctx, "/f", "/g") }},
		{"chtimes, looking up", nil, request(http.MethodGet, "/drive/v3/files", "q"), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Chtimes(ctx, "/f", time.Now()) }},
		{"chtimes", put("/f"), request(http.MethodPatch, "/drive/v3/files/", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error { return fsys.Chtimes(ctx, "/f", time.Now()) }},
		{"export", doc("Plan"), request(http.MethodGet, "/drive/v3/files/", ""), 0, func(ctx context.Context, fsys remotefs.RemoteFS) error {
			return second(fsys.OpenRead(ctx, "/Plan.docx", 0))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newFaulty(t)
			fsys := dial(t, s.Server, "")
			if tc.prepare != nil {
				tc.prepare(t, s.Server, fsys)
			}
			if tc.name == "export" {
				s.arm(func(r *http.Request) bool { return strings.HasSuffix(r.URL.Path, "/export") }, 0, http.StatusForbidden, "insufficientPermissions")
			} else {
				s.arm(tc.match, tc.skip, http.StatusForbidden, "insufficientPermissions")
			}
			if err := tc.run(t.Context(), fsys); !errors.Is(err, remotefs.ErrPermission) {
				t.Errorf("%s = %v, want ErrPermission", tc.name, err)
			}
		})
	}

	s := googletest.Start(t, googletest.Options{})
	fsys := dial(t, s, "")
	write(t, fsys, "/f", "content")
	write(t, fsys, "/g", "other")
	if err := fsys.Mkdir(t.Context(), "/d"); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"List(relative)":          second(fsys.List(t.Context(), "f")),
		"OpenWrite(resume)":       second(fsys.OpenWrite(t.Context(), "/f", 3, remotefs.WriteResume)),
		"OpenWrite(unknown mode)": second(fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteMode(9))),
		"OpenWrite(root)":         second(fsys.OpenWrite(t.Context(), "/", 0, remotefs.WriteOverwrite)),
		"OpenWrite(folder)":       second(fsys.OpenWrite(t.Context(), "/d", 0, remotefs.WriteOverwrite)),
		"Chmod()":                 fsys.Chmod(t.Context(), "/f", 0o600),
		"Rename(root)":            fsys.Rename(t.Context(), "/", "/x"),
		"Rename(to relative)":     fsys.Rename(t.Context(), "/f", "x"),
	} {
		if !errors.Is(err, remotefs.ErrInvalid) {
			t.Errorf("%s = %v, want ErrInvalid", name, err)
		}
	}
	for name, tc := range map[string]struct {
		err  error
		want error
	}{
		"Rename(onto an existing file)": {fsys.Rename(t.Context(), "/f", "/g"), remotefs.ErrExists},
		"Rename(missing file)":          {fsys.Rename(t.Context(), "/nope", "/x"), remotefs.ErrNotFound},
		"Rename(into a missing folder)": {fsys.Rename(t.Context(), "/f", "/no/f"), remotefs.ErrNotFound},
		"Rename(below a file)":          {fsys.Rename(t.Context(), "/f", "/g/f"), remotefs.ErrNotDirectory},
		"Chtimes(missing file)":         {fsys.Chtimes(t.Context(), "/nope", time.Now()), remotefs.ErrNotFound},
		"RemoveAll(missing file)":       {fsys.RemoveAll(t.Context(), "/nope"), nil},
	} {
		if !errors.Is(tc.err, tc.want) {
			t.Errorf("%s = %v, want %v", name, tc.err, tc.want)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	w, err := fsys.OpenWrite(ctx, "/f", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
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
	if got := read(t, fsys, "/f", 0); got != "content" {
		t.Errorf("file after an abandoned upload = %q, want what it had", got)
	}
}

// put returns a preparation that writes a file at p.
func put(p string) func(t *testing.T, s *googletest.Server, fsys remotefs.RemoteFS) {
	return func(t *testing.T, _ *googletest.Server, fsys remotefs.RemoteFS) { write(t, fsys, p, "x") }
}

// mkdirs returns a preparation that creates the folders ps, in order.
func mkdirs(ps ...string) func(t *testing.T, s *googletest.Server, fsys remotefs.RemoteFS) {
	return func(t *testing.T, _ *googletest.Server, fsys remotefs.RemoteFS) {
		for _, p := range ps {
			if err := fsys.Mkdir(t.Context(), p); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// doc returns a preparation that leaves a Google document of a name in My Drive, as only Google writes them.
func doc(name string) func(t *testing.T, s *googletest.Server, fsys remotefs.RemoteFS) {
	return func(_ *testing.T, s *googletest.Server, _ remotefs.RemoteFS) {
		s.Put(s.Root, googletest.File{Name: name, MimeType: googletest.DocumentMime, Content: []byte("x")})
	}
}

// second returns the error of a call that also returns a value.
func second[T any](_ T, err error) error { return err }
