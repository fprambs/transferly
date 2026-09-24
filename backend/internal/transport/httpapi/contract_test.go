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
