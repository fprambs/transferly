package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
