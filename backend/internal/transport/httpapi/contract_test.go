package httpapi_test

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

// loadSpec returns the OpenAPI document embedded in the generated code and a router over it.
func loadSpec(t *testing.T) (*openapi3.T, routers.Router) {
	t.Helper()
	spec, err := httpapi.GetSpec()
	if err != nil {
		t.Fatalf("loading the embedded spec: %v", err)
	}
	router, err := legacy.NewRouter(spec)
	if err != nil {
		t.Fatalf("building the spec router: %v", err)
	}
	return spec, router
}

// TestContract_ResponsesMatchTheSpec verifies that real responses satisfy api/openapi.yaml.
//
// Scenarios:
//   - GET /healthz answers 200 with a HealthStatus
//   - GET /readyz answers 200 with a HealthStatus when the dependency is up
//   - GET /readyz answers 503 with a HealthStatus when the dependency is down
func TestContract_ResponsesMatchTheSpec(t *testing.T) {
	_, router := loadSpec(t)
	tests := []struct {
		name string
		path string
		err  error
	}{
		{"liveness", "/healthz", nil},
		{"ready", "/readyz", nil},
		{"not ready", "/readyz", errors.New("database down")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(t, fakeReadiness{err: tt.err})
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = response.Body.Close() }()

			lookup := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil)
			route, params, err := router.FindRoute(lookup)
			if err != nil {
				t.Fatalf("finding the route in the spec: %v", err)
			}
			input := &openapi3filter.ResponseValidationInput{
				RequestValidationInput: &openapi3filter.RequestValidationInput{Request: lookup, PathParams: params, Route: route},
				Status:                 response.StatusCode,
				Header:                 response.Header,
				Options:                &openapi3filter.Options{IncludeResponseStatus: true},
			}
			raw, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			input.SetBodyBytes(raw)

			if err := openapi3filter.ValidateResponse(t.Context(), input); err != nil {
				t.Errorf("response does not satisfy the spec: %v", err)
			}
		})
	}
}

// TestContract_ProblemsMatchTheSchema verifies that error responses satisfy the Problem schema,
// for an unknown route and a wrong method.
func TestContract_ProblemsMatchTheSchema(t *testing.T) {
	spec, _ := loadSpec(t)
	schema := spec.Components.Schemas["Problem"].Value
	srv := newServer(t, fakeReadiness{})

	for _, tt := range []struct{ method, path string }{
		{http.MethodGet, "/missing"},
		{http.MethodPost, "/healthz"},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), tt.method, srv.URL+tt.path, nil)
		httpapi.NewRouter(httpapi.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Readiness: fakeReadiness{}}).ServeHTTP(recorder, request)

		var document any
		if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
			t.Fatalf("%s %s: decoding: %v", tt.method, tt.path, err)
		}
		if err := schema.VisitJSON(document); err != nil {
			t.Errorf("%s %s: not a Problem: %v", tt.method, tt.path, err)
		}
	}
}

// validateReply checks a real reply against the response definition of the spec for its operation and status.
func validateReply(t *testing.T, router routers.Router, method, target string, r reply) {
	t.Helper()
	lookup := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	route, params, err := router.FindRoute(lookup)
	if err != nil {
		t.Fatalf("%s %s: finding the route in the spec: %v", method, target, err)
	}
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: lookup, PathParams: params, Route: route},
		Status:                 r.Status,
		Header:                 r.Header,
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}
	input.SetBodyBytes(r.Body)
	if err := openapi3filter.ValidateResponse(t.Context(), input); err != nil {
		t.Errorf("%s %s answered %d: the response does not satisfy the spec: %v", method, target, r.Status, err)
	}
}

// TestContract_FileResponses verifies that the responses of the file endpoints, successes and problems, satisfy the spec.
//
// Scenarios:
//   - listing, describing, creating, uploading, renaming and deleting answer as documented
//   - a whole download, a range and an unsatisfiable range answer as documented, headers included
//   - path, exists, not empty and invalid mode problems answer as documented
func TestContract_FileResponses(t *testing.T) {
	_, router := loadSpec(t)
	e := newFileEnv(t)
	check := func(method, target string, r reply) reply {
		validateReply(t, router, method, target, r)
		return r
	}

	dirs := filesPath(e.ws, e.site, "/directories", nil)
	check(http.MethodPost, dirs, e.a.send(http.MethodPost, dirs, map[string]string{"path": "/d"}, e.cookie, e.csrf))
	check(http.MethodPost, dirs, e.a.send(http.MethodPost, dirs, map[string]string{"path": "/d"}, e.cookie, e.csrf))

	put := filesPath(e.ws, e.site, "/content", q("path", "/d/f"))
	check(http.MethodPut, put, e.a.upload(e.ws, e.site, q("path", "/d/f"), "0123456789", e.cookie, e.csrf))
	check(http.MethodPut, put, e.a.upload(e.ws, e.site, q("path", "/d/f"), "again", e.cookie, e.csrf))
	bad := filesPath(e.ws, e.site, "/content", q("path", "/d/f", "mode", "sideways"))
	check(http.MethodPut, bad, e.a.upload(e.ws, e.site, q("path", "/d/f", "mode", "sideways"), "x", e.cookie, e.csrf))

	list := filesPath(e.ws, e.site, "", q("path", "/d"))
	check(http.MethodGet, list, e.get("", q("path", "/d"), nil))
	entry := filesPath(e.ws, e.site, "/entry", q("path", "/d/f"))
	check(http.MethodGet, entry, e.get("/entry", q("path", "/d/f"), nil))
	missing := filesPath(e.ws, e.site, "/entry", q("path", "/nope"))
	check(http.MethodGet, missing, e.get("/entry", q("path", "/nope"), nil))

	content := filesPath(e.ws, e.site, "/content", q("path", "/d/f"))
	check(http.MethodGet, content, e.get("/content", q("path", "/d/f"), nil))
	check(http.MethodGet, content, e.get("/content", q("path", "/d/f"), map[string]string{"Range": "bytes=2-4"}))
	check(http.MethodGet, content, e.get("/content", q("path", "/d/f"), map[string]string{"Range": "bytes=50-"}))

	rename := filesPath(e.ws, e.site, "/rename", nil)
	check(http.MethodPost, rename, e.a.send(http.MethodPost, rename, map[string]string{"from": "/d/f", "to": "/g"}, e.cookie, e.csrf))
	del := filesPath(e.ws, e.site, "", q("path", "/d"))
	check(http.MethodDelete, del, e.a.send(http.MethodDelete, del, nil, e.cookie, e.csrf))
	e.a.upload(e.ws, e.site, q("path", "/d/x"), "x", e.cookie, e.csrf)
	check(http.MethodDelete, del, e.a.send(http.MethodDelete, del, nil, e.cookie, e.csrf))
	check(http.MethodDelete, del, e.a.send(http.MethodDelete, filesPath(e.ws, e.site, "", q("path", "/d", "recursive", "true")), nil, e.cookie, e.csrf))
}

// TestContract_TransferResponses verifies that the responses of the transfer endpoints, and the transfer headers of
// uploads and downloads, satisfy the spec.
//
// Scenarios:
//   - uploading and downloading with an identifier answer with the documented headers
//   - listing, reading, canceling a finished transfer and deleting answer as documented, problems included
func TestContract_TransferResponses(t *testing.T) {
	_, router := loadSpec(t)
	e := newFileEnv(t)
	check := func(method, target string, r reply) reply {
		validateReply(t, router, method, target, r)
		return r
	}

	id := uuid.New()
	put := filesPath(e.ws, e.site, "/content", q("path", "/f", "transferId", id.String(), "size", "3"))
	check(http.MethodPut, put, e.a.upload(e.ws, e.site, q("path", "/f", "transferId", id.String(), "size", "3"), "abc", e.cookie, e.csrf))
	content := filesPath(e.ws, e.site, "/content", q("path", "/f"))
	check(http.MethodGet, content, e.get("/content", q("path", "/f"), nil))
	check(http.MethodGet, content, e.get("/content", q("path", "/f", "transferId", uuid.NewString()), map[string]string{"Range": "bytes=0-1"}))

	check(http.MethodGet, "/api/v1/transfers", e.a.do(request{method: http.MethodGet, path: "/api/v1/transfers", cookie: e.cookie}))
	one := "/api/v1/transfers/" + id.String()
	check(http.MethodGet, one, e.a.do(request{method: http.MethodGet, path: one, cookie: e.cookie}))
	missing := "/api/v1/transfers/" + uuid.NewString()
	check(http.MethodGet, missing, e.a.do(request{method: http.MethodGet, path: missing, cookie: e.cookie}))
	cancel := one + "/cancel"
	check(http.MethodPost, cancel, e.a.send(http.MethodPost, cancel, nil, e.cookie, e.csrf))
	check(http.MethodDelete, one, e.a.send(http.MethodDelete, one, nil, e.cookie, e.csrf))
	check(http.MethodDelete, one, e.a.send(http.MethodDelete, one, nil, e.cookie, e.csrf))
}
