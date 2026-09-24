package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fprambs/sentinel/internal/testutil"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

// doProblem sends a request and returns the raw response with its decoded problem document.
func doProblem(t *testing.T, method, url string) (testutil.Response, httpapi.Problem) {
	t.Helper()
	resp := testutil.Do(t, method, url)
	var p httpapi.Problem
	if err := json.Unmarshal(resp.Body, &p); err != nil {
		t.Fatalf("decoding problem: %v", err)
	}
	return resp, p
}

// TestProblem_UnknownRoute verifies that an unknown route answers 404
// as application/problem+json with the stable code not_found.
func TestProblem_UnknownRoute(t *testing.T) {
	srv := newServer(t, fakeReadiness{})
	resp, p := doProblem(t, http.MethodGet, srv.URL+"/missing")

	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	if resp.Status != http.StatusNotFound || p.Status != http.StatusNotFound {
		t.Errorf("status = %d / %d, want 404", resp.Status, p.Status)
	}
	if p.Code != "not_found" || p.Type != "about:blank" || p.Title != "Not Found" {
		t.Errorf("problem = %+v, want code not_found, type about:blank, title Not Found", p)
	}
}

// TestProblem_MethodNotAllowed verifies that a wrong method on a known route
// answers 405 as a problem document with the stable code method_not_allowed.
func TestProblem_MethodNotAllowed(t *testing.T) {
	srv := newServer(t, fakeReadiness{})
	resp, p := doProblem(t, http.MethodPost, srv.URL+"/healthz")

	if resp.Status != http.StatusMethodNotAllowed || p.Code != "method_not_allowed" {
		t.Errorf("got %d %+v, want 405 method_not_allowed", resp.Status, p)
	}
}
