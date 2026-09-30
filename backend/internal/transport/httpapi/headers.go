package httpapi

import (
	"net/http"
	"strings"
)

const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; " +
	"font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// sandboxPrefix is where the isolated page of the file viewers lives (ADR-0018).
const sandboxPrefix = "/sandbox/"

// sandboxPolicy is the Content-Security-Policy of the isolated page: an opaque origin even when opened directly, its own scripts and
// styles, blobs for what it draws, no network at all, and framing by Sentinel only.
const sandboxPolicy = "sandbox allow-scripts; default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src blob: data:; media-src blob:; font-src blob: data:; worker-src blob:; connect-src 'none'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'self'"

// securityHeaders sets the response headers recommended by the OWASP HTTP Headers cheat sheet.
//
// Responses are not cacheable by default; handlers that serve static assets replace Cache-Control.
// Strict-Transport-Security is left to the TLS terminating proxy. The isolated page of the file viewers gets its own policy and may be
// framed by Sentinel itself (ADR-0018).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, sandboxPrefix) {
			h.Set("Content-Security-Policy", sandboxPolicy)
			h.Set("X-Frame-Options", "SAMEORIGIN")
		}
		next.ServeHTTP(w, r)
	})
}
