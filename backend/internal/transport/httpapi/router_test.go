package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fprambs/sentinel/internal/testutil"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

// fakeReadiness is a readiness checker that returns a fixed error.
type fakeReadiness struct{ err error }

// Ping returns the configured error.
func (f fakeReadiness) Ping(context.Context) error { return f.err }

// newServer starts a test server around the router and stops it when the test ends.
func newServer(t *testing.T, ready fakeReadiness) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.Deps{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Readiness: ready,
	}))
	t.Cleanup(srv.Close)
	return srv
}

// getStatus performs a GET request and returns the status and the decoded JSON body.
func getStatus(t *testing.T, url string) (int, map[string]string) {
	t.Helper()
	resp := testutil.Do(t, http.MethodGet, url)
	var body map[string]string
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("decoding response from %s: %v", url, err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	return resp.Status, body
}

// TestHealthz verifies that GET /healthz answers 200 with status ok.
func TestHealthz(t *testing.T) {
	srv := newServer(t, fakeReadiness{})
	status, body := getStatus(t, srv.URL+"/healthz")
	if status != http.StatusOK || body["status"] != "ok" {
		t.Errorf("healthz = %d %v, want 200 ok", status, body)
	}
}

// TestHealthz_IgnoresDependencyFailures verifies that liveness stays 200
// while the readiness dependency is down, so orchestrators do not restart a healthy process.
func TestHealthz_IgnoresDependencyFailures(t *testing.T) {
	srv := newServer(t, fakeReadiness{err: errors.New("database down")})
	status, _ := getStatus(t, srv.URL+"/healthz")
	if status != http.StatusOK {
		t.Errorf("healthz with database down = %d, want 200", status)
	}
}

// TestReadyz verifies the readiness endpoint.
//
// Scenarios:
//   - dependency reachable: 200 with status ok
//   - dependency down: 503 with status unavailable
func TestReadyz(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"ready", nil, http.StatusOK, "ok"},
		{"dependency down", errors.New("database down"), http.StatusServiceUnavailable, "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(t, fakeReadiness{err: tt.err})
			status, body := getStatus(t, srv.URL+"/readyz")
			if status != tt.wantStatus || body["status"] != tt.wantBody {
				t.Errorf("readyz = %d %v, want %d %s", status, body, tt.wantStatus, tt.wantBody)
			}
		})
	}
}

// TestReadyz_DoesNotLeakInternalErrors verifies that a failing dependency
// never exposes its error text (hosts, credentials) in the response body.
func TestReadyz_DoesNotLeakInternalErrors(t *testing.T) {
	srv := newServer(t, fakeReadiness{err: errors.New("password=secret host=internal")})
	resp := testutil.Do(t, http.MethodGet, srv.URL+"/readyz")
	if string(resp.Body) != "{\"status\":\"unavailable\"}\n" {
		t.Errorf("response exposes internal details: %q", resp.Body)
	}
}

// fakeWeb is a frontend stand-in that answers with a marker and its own cache policy.
var fakeWeb = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=60")
	_, _ = w.Write([]byte("frontend"))
})

// newServerWithWeb starts a test server whose router has a frontend handler.
func newServerWithWeb(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.Deps{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Readiness: fakeReadiness{},
		Web:       fakeWeb,
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSecurityHeaders verifies the hardening headers.
//
// Scenarios:
//   - API, problem and frontend responses all carry the OWASP recommended headers
//   - the content security policy forbids inline scripts and framing
//   - responses are not cacheable by default, and the frontend can set its own cache policy
//   - the isolated page of the file viewers has an opaque origin, no network, and may be framed by Sentinel only (ADR-0018)
func TestSecurityHeaders(t *testing.T) {
	api := newServer(t, fakeReadiness{})
	web := newServerWithWeb(t)

	for name, url := range map[string]string{
		"api":      api.URL + "/healthz",
		"problem":  api.URL + "/missing",
		"frontend": web.URL + "/sites",
	} {
		resp := testutil.Do(t, http.MethodGet, url)
		for header, want := range map[string]string{
			"X-Content-Type-Options":       "nosniff",
			"X-Frame-Options":              "DENY",
			"Referrer-Policy":              "strict-origin-when-cross-origin",
			"Cross-Origin-Opener-Policy":   "same-origin",
			"Cross-Origin-Resource-Policy": "same-origin",
			"Permissions-Policy":           "geolocation=(), camera=(), microphone=()",
		} {
			if got := resp.Header.Get(header); got != want {
				t.Errorf("%s: %s = %q, want %q", name, header, got, want)
			}
		}
		csp := resp.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "unsafe-inline") {
			t.Errorf("%s: Content-Security-Policy = %q, want a strict policy", name, csp)
		}
	}

	sandbox := testutil.Do(t, http.MethodGet, web.URL+"/sandbox/viewer.html")
	policy := sandbox.Header.Get("Content-Security-Policy")
	for _, directive := range []string{"sandbox allow-scripts;", "connect-src 'none'", "frame-ancestors 'self'", "default-src 'none'"} {
		if !strings.Contains(policy, directive) {
			t.Errorf("sandbox Content-Security-Policy = %q, want %q", policy, directive)
		}
	}
	if strings.Contains(policy, "allow-same-origin") || sandbox.Header.Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Errorf("sandbox headers = %v", sandbox.Header)
	}

	if got := testutil.Do(t, http.MethodGet, api.URL+"/healthz").Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("API Cache-Control = %q, want no-store", got)
	}
	if got := testutil.Do(t, http.MethodGet, web.URL+"/sites").Header.Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("frontend Cache-Control = %q, want the frontend policy", got)
	}
}

// TestNotFound_Frontend verifies which unknown paths reach the frontend.
//
// Scenarios:
//   - GET and HEAD on application paths are served by the frontend
//   - anything under /api/ is a not_found problem, never HTML
//   - other methods on unknown paths are a not_found problem
func TestNotFound_Frontend(t *testing.T) {
	srv := newServerWithWeb(t)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if resp := testutil.Do(t, method, srv.URL+"/sites/42"); resp.Status != http.StatusOK {
			t.Errorf("%s /sites/42 = %d, want the frontend", method, resp.Status)
		}
	}
	if resp := testutil.Do(t, http.MethodGet, srv.URL+"/sites/42"); string(resp.Body) != "frontend" {
		t.Errorf("body = %q, want the frontend marker", resp.Body)
	}
	for _, target := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/unknown"},
		{http.MethodPost, "/sites"},
	} {
		resp := testutil.Do(t, target.method, srv.URL+target.path)
		if resp.Status != http.StatusNotFound || !strings.Contains(resp.Header.Get("Content-Type"), "problem+json") {
			t.Errorf("%s %s = %d %q, want a not_found problem", target.method, target.path, resp.Status, resp.Header.Get("Content-Type"))
		}
	}
}
