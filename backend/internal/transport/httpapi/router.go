// Package httpapi is the HTTP transport adapter that maps REST requests to the core.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/fprambs/sentinel/internal/core/ports"
)

const readinessTimeout = 2 * time.Second

// Deps are the dependencies of the router.
type Deps struct {
	// Logger receives access and error logs.
	Logger *slog.Logger
	// Readiness backs the /readyz endpoint.
	Readiness ports.ReadinessChecker
}

// NewRouter returns the HTTP handler with the base middleware stack.
//
// It serves GET /healthz (liveness, independent of dependencies)
// and GET /readyz (readiness, backed by [Deps.Readiness]).
// Errors are returned as [Problem] documents.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(d.Logger))
	r.Use(middleware.Recoverer)

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusNotFound, "not_found", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusMethodNotAllowed, "method_not_allowed", nil)
	})

	r.Get("/healthz", healthz)
	r.Get("/readyz", readyz(d))
	return r
}

// healthz answers liveness probes; it never touches dependencies.
func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz answers readiness probes by pinging the readiness checker within a timeout.
// Failures are logged and reported as 503 without exposing internal details.
func readyz(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()
		if err := d.Readiness.Ping(ctx); err != nil {
			d.Logger.WarnContext(r.Context(), "readiness check failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// writeJSON writes v as a JSON body with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// requestLogger logs one structured line per request with method, path, status, size, duration and request id.
func requestLogger(l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			l.InfoContext(r.Context(), "http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
