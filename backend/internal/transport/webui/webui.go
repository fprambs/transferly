// Package webui serves the single-page application embedded in the binary.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var assets embed.FS

const (
	indexName      = "index.html"
	immutableCache = "public, max-age=31536000, immutable"
	revalidate     = "no-cache"
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
// immutably and everything else must be revalidated. A path without a file extension that does
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
	http.ServeContent(w, r, name, time.Time{}, seeker)
	return true
}

// readSeeker is the file capability http.ServeContent needs.
type readSeeker interface {
	Read(p []byte) (int, error)
	Seek(offset int64, whence int) (int64, error)
}
