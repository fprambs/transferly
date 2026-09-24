// Package httpapi is the HTTP transport adapter that maps REST requests to the core.
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/fprambs/sentinel/internal/core/ports"
)

// Deps are the dependencies of the router.
type Deps struct {
	// Logger receives access and error logs.
	Logger *slog.Logger
	// Readiness backs the readiness endpoint.
	Readiness ports.ReadinessChecker
}

// NewRouter returns the HTTP handler with the base middleware stack.
//
// Routes come from the OpenAPI contract in api/openapi.yaml.
// Errors, including unknown routes and wrong methods, are returned as [Problem] documents.
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

	strict := NewStrictHandlerWithOptions(newServer(d), nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  requestErrorHandler,
		ResponseErrorHandlerFunc: responseErrorHandler(d.Logger),
	})
	return HandlerWithOptions(strict, ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: requestErrorHandler,
	})
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
