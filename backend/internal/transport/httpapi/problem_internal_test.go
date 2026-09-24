package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWriteProblem_Params verifies that parameters are included in the problem document when given.
func TestWriteProblem_Params(t *testing.T) {
	rec := httptest.NewRecorder()

	writeProblem(rec, http.StatusConflict, "already_exists", map[string]any{"name": "backup"})

	var got Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding problem: %v", err)
	}
	if got.Params == nil || (*got.Params)["name"] != "backup" {
		t.Errorf("params = %v, want name=backup", got.Params)
	}
}

// TestRequestErrorHandler verifies that undecodable requests answer 400 with the stable code invalid_request.
func TestRequestErrorHandler(t *testing.T) {
	rec := httptest.NewRecorder()

	requestErrorHandler(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), errors.New("bad parameter"))

	var got Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding problem: %v", err)
	}
	if rec.Code != http.StatusBadRequest || got.Code != "invalid_request" {
		t.Errorf("got %d %q, want 400 invalid_request", rec.Code, got.Code)
	}
}

// TestResponseErrorHandler verifies that operation failures are logged and answered as 500 without details.
func TestResponseErrorHandler(t *testing.T) {
	var logs bytes.Buffer
	rec := httptest.NewRecorder()

	responseErrorHandler(slog.New(slog.NewTextHandler(&logs, nil)))(
		rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), errors.New("password=secret"),
	)

	var got Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding problem: %v", err)
	}
	if rec.Code != http.StatusInternalServerError || got.Code != "internal_error" {
		t.Errorf("got %d %q, want 500 internal_error", rec.Code, got.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("response leaks the internal error")
	}
	if !strings.Contains(logs.String(), "password=secret") {
		t.Error("the internal error was not logged")
	}
}
