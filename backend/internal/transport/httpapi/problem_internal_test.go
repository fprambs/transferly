package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/core/remotefs"
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

// TestRemoteEntryDTO verifies how remote entries are presented.
//
// Scenarios:
//   - regular files, directories, symbolic links and special files get their type
//   - the mode is in ls notation, the time is UTC and the precision has its public name
func TestRemoteEntryDTO(t *testing.T) {
	when := time.Date(2026, 3, 8, 12, 0, 0, 0, time.FixedZone("UTC-3", -3*3600))
	tests := []struct {
		name string
		mode fs.FileMode
		want RemoteEntryType
	}{
		{"file", 0o644, "file"},
		{"directory", fs.ModeDir | 0o755, "directory"},
		{"symlink", fs.ModeSymlink | 0o777, "symlink"},
		{"device", fs.ModeDevice | 0o600, "other"},
		{"socket", fs.ModeSocket | 0o600, "other"},
	}
	for _, tt := range tests {
		dto := remoteEntryDTO(remotefs.Entry{Name: tt.name, Path: "/" + tt.name, Mode: tt.mode, ModTime: when, ModTimePrecision: remotefs.PrecisionMinute})
		if dto.Type != tt.want || dto.ModifiedPrecision != "minute" || dto.ModifiedAt.Location() != time.UTC || !dto.ModifiedAt.Equal(when) || dto.Mode != tt.mode.String() {
			t.Errorf("%s: dto = %+v", tt.name, dto)
		}
	}
	for precision, name := range map[remotefs.Precision]RemoteEntryModifiedPrecision{remotefs.PrecisionSecond: "second", remotefs.PrecisionDay: "day"} {
		if got := remoteEntryDTO(remotefs.Entry{ModTimePrecision: precision}).ModifiedPrecision; got != name {
			t.Errorf("precision %d = %q; want %q", precision, got, name)
		}
	}
}

// TestAttachment verifies that download names cannot inject headers.
//
// Scenarios:
//   - plain names are quoted parameters, and names with line breaks, NUL bytes, invalid UTF-8 or non-ASCII text are
//     percent-encoded, never containing raw control characters
func TestAttachment(t *testing.T) {
	if got := attachment("report.txt"); got != "attachment; filename=report.txt" {
		t.Errorf("attachment(report.txt) = %q", got)
	}
	for _, name := range []string{"a\r\nSet-Cookie: x=1", "a\x00b", "\xff\xfe", "é.txt"} {
		got := attachment(name)
		if strings.ContainsAny(got, "\r\n\x00") || !strings.HasPrefix(got, "attachment; filename*=utf-8''") {
			t.Errorf("attachment(%q) = %q; want a percent-encoded name without control characters", name, got)
		}
	}
}

// TestProblemOr verifies that a failure with no problem of its own is left for the internal error handler.
//
// Use cases:
//   - a domain error becomes its problem as a response of the operation
//   - any other error is returned as it is, with no response
func TestProblemOr(t *testing.T) {
	if response, err := problemOr[ListArchiveEntriesResponseObject](remotefs.ErrNotFound); err != nil || response.(domainProblem).code != "path_not_found" {
		t.Errorf("problemOr(ErrNotFound) = %v, %v", response, err)
	}
	internal := errors.New("broken")
	if response, err := problemOr[DownloadArchiveEntryResponseObject](internal); response != nil || !errors.Is(err, internal) {
		t.Errorf("problemOr(internal) = %v, %v", response, err)
	}
}
