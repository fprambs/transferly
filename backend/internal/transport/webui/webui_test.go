package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// testFiles is a small build of the frontend.
var testFiles = fstest.MapFS{
	"index.html":          {Data: []byte("<html>app</html>")},
	"favicon.svg":         {Data: []byte("<svg/>")},
	"assets/app-1.js":     {Data: []byte("console.log(1)")},
	"assets/app-1.css":    {Data: []byte("body{}")},
	"sandbox/viewer.html": {Data: []byte("<html>viewer</html>")},
}

// get sends a request with the given method to a handler over testFiles.
func get(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	newHandler(testFiles).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, nil))
	return rec
}

// TestHandler_ServesFiles verifies that existing files are served with the right type and caching.
//
// Scenarios:
//   - the root serves the index document and must be revalidated
//   - a hashed asset is cached as immutable and has a script content type
//   - a root file that is not an asset must be revalidated
//   - HEAD returns the headers without a body
func TestHandler_ServesFiles(t *testing.T) {
	tests := []struct {
		name, method, target, wantType, wantCache string
		wantBody                                  string
	}{
		{"index", http.MethodGet, "/", "text/html", "no-cache", "<html>app</html>"},
		{"asset", http.MethodGet, "/assets/app-1.js", "text/javascript", "public, max-age=31536000, immutable", "console.log(1)"},
		{"root file", http.MethodGet, "/favicon.svg", "image/svg+xml", "no-cache", "<svg/>"},
		{"head", http.MethodHead, "/assets/app-1.css", "text/css", "public, max-age=31536000, immutable", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, tt.method, tt.target)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, tt.wantType) {
				t.Errorf("Content-Type = %q, want prefix %q", got, tt.wantType)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
			if rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

// TestHandler_CrossOrigin covers the headers that let the opaque origin of the isolated page load the build (ADR-0018).
//
// Use cases:
//   - hashed assets and the isolated page allow any origin and cross-origin loading
//   - the index document and other root files do not
func TestHandler_CrossOrigin(t *testing.T) {
	for target, want := range map[string]bool{"/assets/app-1.js": true, "/sandbox/viewer.html": true, "/": false, "/favicon.svg": false} {
		rec := get(t, http.MethodGet, target)
		open := rec.Header().Get("Access-Control-Allow-Origin") == "*" && rec.Header().Get("Cross-Origin-Resource-Policy") == "cross-origin"
		if open != want || (!want && rec.Header().Get("Access-Control-Allow-Origin") != "") {
			t.Errorf("%s: cross-origin headers = %v; want open %v", target, rec.Header(), want)
		}
	}
}

// TestHandler_ApplicationRoutes verifies the single-page fallback.
//
// Scenarios:
//   - an unknown path without an extension receives the index document
//   - a directory path receives the index document
//   - a missing file with an extension is a 404, never HTML
//   - path traversal attempts, plain, percent-encoded and double-encoded, only ever reach the index document
//   - backslash traversal is a 404 and never a host file
//   - dotfiles are never served
//   - a build without an index document answers 404 for application routes
func TestHandler_ApplicationRoutes(t *testing.T) {
	if rec := get(t, http.MethodGet, "/sites/42"); rec.Code != http.StatusOK || rec.Body.String() != "<html>app</html>" {
		t.Errorf("app route = %d %q, want 200 with the index", rec.Code, rec.Body.String())
	}
	if rec := get(t, http.MethodGet, "/assets"); rec.Code != http.StatusOK || rec.Body.String() != "<html>app</html>" {
		t.Errorf("directory = %d %q, want 200 with the index", rec.Code, rec.Body.String())
	}
	if rec := get(t, http.MethodGet, "/assets/missing.js"); rec.Code != http.StatusNotFound {
		t.Errorf("missing asset = %d, want 404", rec.Code)
	}
	for _, target := range []string{
		"/../../etc/passwd",
		"/%2e%2e/%2e%2e/etc/passwd",
		"/..%2f..%2fetc/passwd",
		"/assets/../../../etc/passwd",
		"//etc/passwd",
		"/index.html/../../etc/passwd",
		"/%252e%252e/%252e%252e/etc/passwd",
		"/....//....//etc/passwd",
	} {
		rec := get(t, http.MethodGet, target)
		if rec.Code != http.StatusOK || rec.Body.String() != "<html>app</html>" {
			t.Errorf("traversal %q = %d %q, want only the index document", target, rec.Code, rec.Body.String())
		}
	}
	if rec := get(t, http.MethodGet, "/\\..\\..\\etc\\passwd"); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "root:") {
		t.Errorf("backslash traversal = %d, want 404 and no host file", rec.Code)
	}
	if rec := get(t, http.MethodGet, "/.env"); rec.Code != http.StatusNotFound {
		t.Errorf("dotfile = %d, want 404", rec.Code)
	}

	empty := httptest.NewRecorder()
	newHandler(fstest.MapFS{}).ServeHTTP(empty, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/sites", nil))
	if empty.Code != http.StatusNotFound {
		t.Errorf("build without index = %d, want 404", empty.Code)
	}
}

// TestHandler_RejectsOtherMethods verifies that only GET and HEAD are served, advertising them in Allow.
func TestHandler_RejectsOtherMethods(t *testing.T) {
	rec := get(t, http.MethodPost, "/")

	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST = %d Allow=%q, want 405 GET, HEAD", rec.Code, rec.Header().Get("Allow"))
	}
}

// TestHandler_Embedded verifies that the embedded handler builds and answers 404 while the frontend is not built.
func TestHandler_Embedded(t *testing.T) {
	rec := httptest.NewRecorder()

	Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if rec.Code != http.StatusNotFound && rec.Code != http.StatusOK {
		t.Errorf("embedded handler = %d, want 404 without a build or 200 with one", rec.Code)
	}
}

// TestHandler_UnreadableEntries verifies that non-seekable files and stat failures are treated as missing.
func TestHandler_UnreadableEntries(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(unseekableFS{}).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/index.html", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("non-seekable file = %d, want 404", rec.Code)
	}
}
