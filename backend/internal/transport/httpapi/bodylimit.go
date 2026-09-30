package httpapi

import (
	"net/http"
	"strings"

	"github.com/fprambs/sentinel/internal/core/backup"
	"github.com/fprambs/sentinel/internal/core/fzimport"
)

const (
	// maxBody bounds the body of every request that has no limit of its own: far above any real JSON request, whose largest value is a
	// private key of at most 64 KiB, and far below what could exhaust the server.
	maxBody = 1 << 20
	// maxFilezillaImportBody bounds a FileZilla import, a site manager file of at most [fzimport.MaxBytes] sent as the body, leaving room
	// for its parser to answer that it is too large.
	maxFilezillaImportBody = fzimport.MaxBytes + 64<<10
	// maxSentinelImportBody bounds a Sentinel import: a file of [backup.MaxBytes] written as a JSON string, whose escapes may nearly
	// double it, and its password.
	maxSentinelImportBody = 2*backup.MaxBytes + 64<<10
)

// bodyLimit returns the largest body a path accepts, and false for uploads, whose bodies are files of any size streamed to the server
// and bounded by the quotas of the workspace instead.
func bodyLimit(path string) (int64, bool) {
	switch {
	case strings.HasSuffix(path, "/files/content"):
		return 0, false
	case strings.HasSuffix(path, "/imports/filezilla"):
		return maxFilezillaImportBody, true
	case strings.HasSuffix(path, "/imports/sentinel"):
		return maxSentinelImportBody, true
	}
	return maxBody, true
}

// limitBodies caps the body of every request before anything reads it, whatever its declared type, so a large or endless body cannot
// hold memory or a decoder busy (security review of phase 4). A request over its limit fails to decode and is answered invalid_request.
func limitBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limit, ok := bodyLimit(r.URL.Path); ok {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}
