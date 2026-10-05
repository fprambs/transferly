package davfs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// multistatus is a PROPFIND answer with the given responses.
func multistatus(responses ...string) string {
	return `<?xml version="1.0" encoding="utf-8"?><D:multistatus xmlns:D="DAV:">` + strings.Join(responses, "") + `</D:multistatus>`
}

// folderAt is a response describing a folder at href.
func folderAt(href string) string {
	return `<D:response><D:href>` + href + `</D:href><D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>`
}

// fileAt is a response describing a file at href with the given length.
func fileAt(href, length string) string {
	return `<D:response><D:href>` + href + `</D:href><D:propstat><D:prop><D:resourcetype/><D:getcontentlength>` + length +
		`</D:getcontentlength><D:getlastmodified>Sun, 04 Oct 2026 12:00:00 GMT</D:getlastmodified></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>`
}

// scripted starts a server that answers with handler and returns a file system on its root /dav/, past Dial.
func scripted(t *testing.T, handler http.HandlerFunc) *FS {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	f, err := Dial(t.Context(), Config{Endpoint: server.URL + "/dav", Client: server.Client()})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// root answers the PROPFIND of the root of the site, which Dial sends.
func root(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == "PROPFIND" && r.URL.Path == "/dav/" {
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, multistatus(folderAt("/dav/")))
		return true
	}
	return false
}

// TestMultistatus verifies how answers of servers are read.
//
// Use cases:
//   - hrefs may be absolute URLs, percent-encoded or carry a trailing slash; items outside the root, with an href that cannot be
//     parsed, or without properties found, are left out
//   - a lookup that answers one item for another path takes it as the path asked for; one that answers none fails
//   - a listing that cannot be parsed is a lost connection
//   - a redirect that adds a trailing slash is followed
func TestMultistatus(t *testing.T) {
	f := scripted(t, func(w http.ResponseWriter, r *http.Request) {
		if root(w, r) {
			return
		}
		switch r.URL.Path {
		case "/dav/folder":
			w.Header().Set("Location", "/dav/folder/")
			w.WriteHeader(http.StatusMovedPermanently)
		case "/dav/folder/":
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, multistatus(
				folderAt("/dav/folder/"),
				fileAt("http://elsewhere/dav/folder/a%20b.txt", "3"),
				fileAt("/outside/x", "1"),
				fileAt("%zz", "1"),
				`<D:response><D:href>/dav/folder/hidden</D:href><D:propstat><D:prop/><D:status>HTTP/1.1 404 Not Found</D:status></D:propstat></D:response>`,
			))
		case "/dav/alias":
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, multistatus(fileAt("/dav/elsewhere", "5")))
		case "/dav/nothing":
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, multistatus())
		case "/dav/broken":
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, "<not xml")
		}
	})
	entries, err := f.List(t.Context(), "/folder")
	if err != nil || len(entries) != 1 || entries[0].Name != "a b.txt" || entries[0].Size != 3 || entries[0].ModTime.IsZero() {
		t.Errorf("List(/folder) = %+v, %v, want the one file inside the root", entries, err)
	}
	if e, err := f.Stat(t.Context(), "/alias"); err != nil || e.Path != "/alias" || e.Size != 5 {
		t.Errorf("Stat(/alias) = %+v, %v, want the item as /alias", e, err)
	}
	if _, err := f.Stat(t.Context(), "/nothing"); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Stat(/nothing) = %v, want ErrConnectionLost", err)
	}
	if _, err := f.Stat(t.Context(), "/broken"); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Stat(/broken) = %v, want ErrConnectionLost", err)
	}
}

// TestStreams verifies reads and writes that the network cuts short.
//
// Use cases:
//   - a body shorter than the offset a server that ignores ranges must skip fails the read
//   - a body cut in the middle fails the read with a lost connection
//   - a write to a server that answered the upload before reading it fails with a lost connection
//   - a write to a file system that was closed fails with a lost connection
func TestStreams(t *testing.T) {
	f := scripted(t, func(w http.ResponseWriter, r *http.Request) {
		if root(w, r) {
			return
		}
		switch {
		case r.Method == "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, multistatus(fileAt(r.URL.Path, "10")))
		case r.URL.Path == "/dav/short":
			w.Header().Set("Content-Length", "10")
			_, _ = io.WriteString(w, "01")
		case r.URL.Path == "/dav/cut":
			w.Header().Set("Content-Length", "10")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "01")
		case r.Method == http.MethodPut && r.URL.Path == "/dav/hasty":
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut:
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusCreated)
		}
	})
	if _, err := f.OpenRead(t.Context(), "/short", 5); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("OpenRead past a short body = %v, want ErrConnectionLost", err)
	}
	r, err := f.OpenRead(t.Context(), "/cut", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(r); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("read of a cut body = %v, want ErrConnectionLost", err)
	}
	_ = r.Close()
	hasty, err := f.OpenWrite(t.Context(), "/hasty", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	var cut error
	for i := 0; i < 2000 && cut == nil; i++ {
		_, cut = hasty.Write(make([]byte, 64<<10))
	}
	if !errors.Is(cut, remotefs.ErrConnectionLost) {
		t.Errorf("Write to a server that answered before reading = %v, want ErrConnectionLost", cut)
	}
	_ = hasty.Close()
	w, err := f.OpenWrite(t.Context(), "/new", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := w.Write([]byte("x")); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Write after Close = %v, want ErrConnectionLost", err)
	}
	_ = w.Close()
}

// TestSend verifies that a request that cannot be built is invalid, and how failures name the request.
func TestSend(t *testing.T) {
	f := &FS{client: http.DefaultClient, life: context.Background()}
	resp, err := f.send(t.Context(), "BAD METHOD", "http://h/", nil, nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("send(bad method) = %v, want ErrInvalid", err)
	}
	if got := (&StatusError{Method: "MOVE", Status: 412}).Error(); got != "MOVE answered 412" {
		t.Errorf("Error() = %q", got)
	}
	if !absent(&StatusError{Status: 409}) || absent(&StatusError{Status: 500}) || absent(io.EOF) {
		t.Error("absent() does not tell missing paths from failures")
	}
}
