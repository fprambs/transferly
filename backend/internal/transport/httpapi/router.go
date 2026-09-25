// Package httpapi is the HTTP transport adapter that maps REST requests to the core.
package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/fprambs/sentinel/internal/adapters/events"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/files"
	"github.com/fprambs/sentinel/internal/core/hostkey"
	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/core/quick"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/transfer"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// Limits are the rate limiters that protect the unauthenticated endpoints.
type Limits struct {
	// SetupIP limits first-administrator setup attempts per client address.
	SetupIP ports.RateLimiter
	// LoginIP limits sign-in attempts per client address.
	LoginIP ports.RateLimiter
	// LoginAccount limits sign-in attempts per account, whether or not it exists.
	LoginAccount ports.RateLimiter
	// QuickConnect limits how often a user starts quick connections.
	QuickConnect ports.RateLimiter
	// VaultAttempts limits attempts to prove a vault secret, per signed-in account.
	VaultAttempts ports.RateLimiter
}

// CookieConfig describes the session cookie.
type CookieConfig struct {
	// Secure marks the cookie as https only.
	Secure bool
	// MaxAge is how long the browser keeps the cookie.
	MaxAge time.Duration
}

// Deps are the dependencies of the router.
type Deps struct {
	// Logger receives access and error logs.
	Logger *slog.Logger
	// Readiness backs the readiness endpoint.
	Readiness ports.ReadinessChecker
	// Web serves the frontend for unknown GET and HEAD paths outside /api/; nil disables it.
	Web http.Handler
	// Auth is the account and session service.
	Auth *auth.Service
	// Workspaces is the workspace and permission service.
	Workspaces *workspace.Service
	// Events delivers events to the clients connected through the WebSocket.
	Events *events.Hub
	// WebSocket tunes the event stream.
	WebSocket WebSocketConfig
	// Quick keeps the servers users try without saving them.
	Quick *quick.Service
	// Transfers records uploads and downloads.
	Transfers *transfer.Service
	// Files performs operations on the files of remote sites.
	Files *files.Service
	// Connections pools the connections to remote servers; the vault lock and sign-out close a user's connections.
	Connections *connections.Manager
	// HostKeys is the trusted server keys service.
	HostKeys *hostkey.Service
	// Sites is the saved connections service.
	Sites *site.Service
	// Vault is the credential vault service.
	Vault *vault.Service
	// Limits are the rate limiters of the unauthenticated endpoints.
	Limits Limits
	// InstanceKey signs the anti-CSRF tokens.
	InstanceKey []byte
	// Cookie configures the session cookie.
	Cookie CookieConfig
	// AllowedOrigins lists the origins allowed to make state-changing requests.
	AllowedOrigins []string
	// TrustedProxies lists the addresses whose X-Forwarded-For header is believed; empty trusts none.
	TrustedProxies []string
}

// NewRouter returns the HTTP handler with the base middleware stack.
//
// Routes come from the OpenAPI contract in api/openapi.yaml. Operations whose security requirement is not empty
// require a valid session and, for state-changing methods, the anti-CSRF token.
// Errors, including unknown routes and wrong methods, are returned as [Problem] documents.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(clientIPMiddleware(d.TrustedProxies))
	r.Use(securityHeaders)
	r.Use(requestLogger(d.Logger))
	r.Use(middleware.Recoverer)
	r.Use(originGuard(d.AllowedOrigins))
	r.Use(clientInfo)
	r.Use(authenticate(d.Auth, d.Logger))

	r.NotFound(notFoundHandler(d.Web))
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusMethodNotAllowed, "method_not_allowed", nil)
	})

	r.Get(EventsPath, eventsHandler(d))

	spec, _ := GetSpec()
	strict := NewStrictHandlerWithOptions(newServer(d), []StrictMiddlewareFunc{requireSession(d.InstanceKey, protectedOperations(spec))}, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  requestErrorHandler,
		ResponseErrorHandlerFunc: responseErrorHandler(d.Logger),
	})
	return HandlerWithOptions(strict, ChiServerOptions{BaseRouter: r, ErrorHandlerFunc: requestErrorHandler})
}

// notFoundHandler answers unknown routes: application paths go to the frontend when one is configured,
// everything else, including any /api/ path, is a not_found problem.
func notFoundHandler(web http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		readOnly := r.Method == http.MethodGet || r.Method == http.MethodHead
		if web != nil && readOnly && !strings.HasPrefix(r.URL.Path, "/api/") {
			web.ServeHTTP(w, r)
			return
		}
		writeProblem(w, http.StatusNotFound, "not_found", nil)
	}
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
