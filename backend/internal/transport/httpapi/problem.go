package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

const problemContentType = "application/problem+json"

// writeProblem writes an RFC 9457 problem document with the given status and stable code.
func writeProblem(w http.ResponseWriter, status int, code string, params map[string]any) {
	problem := Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Code:   code,
	}
	if params != nil {
		problem.Params = &params
	}
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem)
}

// requestErrorHandler answers requests the generated code could not decode or bind.
func requestErrorHandler(w http.ResponseWriter, _ *http.Request, _ error) {
	writeProblem(w, http.StatusBadRequest, "invalid_request", nil)
}

// responseErrorHandler returns a handler that logs errors raised by operations and answers 500 without details.
func responseErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		logger.ErrorContext(r.Context(), "operation failed", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", nil)
	}
}
