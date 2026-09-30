// Package webui serves the single-page application embedded in the binary.
package webui

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var assets embed.FS

const (
	// noncePlaceholder is replaced in the index document by the nonce of its response.
	noncePlaceholder = "__CSP_NONCE__"
	indexName        = "index.html"
	immutableCache   = "public, max-age=31536000, immutable"
	revalidate       = "no-cache"
)

// Handler returns the handler that serves the embedded build of the frontend.
func Handler() http.Handler {
	sub, _ := fs.Sub(assets, "dist")
	return newHandler(sub)
}

// handler serves files from a file system and falls back to the index document for application routes.
type handler struct {
	files fs.FS
}

// newHandler returns a handler over files.
func newHandler(files fs.FS) http.Handler {
	return handler{files: files}
}

// ServeHTTP serves GET and HEAD requests.
//
// Existing files are served with a content type from their extension; hashed assets are cached
// immutably and everything else must be revalidated. The built assets and the isolated page of the file
// viewers are public and may be loaded from the opaque origin of that page (ADR-0018). A path without a file extension that does
// not exist is an application route and receives the index document. A missing file with an
// extension is a 404, so a broken script or style URL is never answered with HTML.
func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = indexName
	}
	if !h.serve(w, r, name) {
		if path.Ext(name) != "" || !h.serve(w, r, indexName) {
			http.NotFound(w, r)
		}
	}
}

// serve writes the named file and reports whether it exists as a regular file.
func (h handler) serve(w http.ResponseWriter, r *http.Request, name string) bool {
	file, err := h.files.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	seeker, seekable := file.(readSeeker)
	if err != nil || info.IsDir() || !seekable {
		return false
	}

	cache := revalidate
	if strings.HasPrefix(name, "assets/") {
		cache = immutableCache
	}
	w.Header().Set("Cache-Control", cache)
	if strings.HasPrefix(name, "assets/") || strings.HasPrefix(name, "sandbox/") {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	}
	if name == indexName {
		seeker = withNonce(w, seeker)
	}
	http.ServeContent(w, r, name, time.Time{}, seeker)
	return true
}

// withNonce gives the index document a nonce of its own for the styles the application injects at run time, such as the editor's:
// the placeholder of the document becomes the nonce, and the style-src of the policy set before allows it. Nothing else the policy says
// changes, and every response gets a new nonce, so a nonce seen once cannot be reused.
func withNonce(w http.ResponseWriter, file io.Reader) io.ReadSeeker {
	body, _ := io.ReadAll(file)
	random := make([]byte, 16)
	_, _ = rand.Read(random)
	nonce := base64.StdEncoding.EncodeToString(random)
	if policy := w.Header().Get("Content-Security-Policy"); policy != "" {
		w.Header().Set("Content-Security-Policy", strings.Replace(policy, "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1))
	}
	return bytes.NewReader(bytes.ReplaceAll(body, []byte(noncePlaceholder), []byte(nonce)))
}

// readSeeker is the file capability http.ServeContent needs.
type readSeeker interface {
	Read(p []byte) (int, error)
	Seek(offset int64, whence int) (int64, error)
}
