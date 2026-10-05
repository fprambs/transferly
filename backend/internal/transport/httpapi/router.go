// Package httpapi is the HTTP transport adapter that maps REST requests to the core.
package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/fprambs/transferly/internal/adapters/events"
	"github.com/fprambs/transferly/internal/adapters/googleauth"
	"github.com/fprambs/transferly/internal/core/activity"
	"github.com/fprambs/transferly/internal/core/admin"
	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/backup"
	"github.com/fprambs/transferly/internal/core/bookmark"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/credentials"
	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/fzimport"
	"github.com/fprambs/transferly/internal/core/hostkey"
	"github.com/fprambs/transferly/internal/core/invite"
	"github.com/fprambs/transferly/internal/core/limits"
	"github.com/fprambs/transferly/internal/core/mfa"
	"github.com/fprambs/transferly/internal/core/passkey"
	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/prompts"
	"github.com/fprambs/transferly/internal/core/quick"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/transfer"
	"github.com/fprambs/transferly/internal/core/vault"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/core/wskey"
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
	// Invitations limits the endpoints that take an invitation token, per client address.
	Invitations ports.RateLimiter
	// MFA limits the attempts to give a second factor, per client address and per account.
	MFA ports.RateLimiter
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
	// ConnectionLog tells the user what each connection is doing (ADR-0011); nil tells nothing.
	ConnectionLog *connlog.Recorder
	// WebSocket tunes the event stream.
	WebSocket WebSocketConfig
	// Quick keeps the servers users try without saving them.
	Quick *quick.Service
	// Transfers records uploads and downloads.
	Transfers *transfer.Service
	// MFA is the second factor service.
	MFA *mfa.Service
	// Passkeys is the WebAuthn service.
	Passkeys *passkey.Service
	// Audit is the audit trail of the workspaces.
	Audit *audit.Service
	// Quotas keeps the caps and quotas of the instance.
	Quotas *limits.Service
	// Admin is what the administrators of the instance do.
	Admin *admin.Service
	// Invites is the invitation service.
	Invites *invite.Service
	// Backup exports the sites to encrypted files and imports them back (ADR-0017).
	Backup *backup.Service
	// Import creates the sites of a FileZilla site manager file.
	Import *fzimport.Service
	// Activity keeps the log of what users did on their connections.
	Activity *activity.Service
	// Connectors tells which connectors of the catalog Transferly is built with the instance offers (ADR-0019).
	Connectors *connectors.Service
	// Google signs Google accounts in for the Google Drive connector (ADR-0024).
	Google *googleauth.Service
	// Files performs operations on the files of remote sites.
	Files *files.Service
	// Connections pools the connections to remote servers; the vault lock and sign-out close a user's connections.
	Connections *connections.Manager
	// Credentials holds the passwords typed for sites that ask for one when connecting.
	Credentials *credentials.Store
	// HostKeys is the trusted server keys service.
	HostKeys *hostkey.Service
	// Prompts relays what servers ask while an interactive site connects (ADR-0016).
	Prompts *prompts.Broker
	// Bookmarks is the service of the bookmarked folders of saved sites.
	Bookmarks *bookmark.Service
	// Sites is the saved connections service.
	Sites *site.Service
	// Keys hands out the keys that seal the secrets of each workspace.
	Keys *wskey.Service
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
	r.Use(limitBodies)
	r.Use(authenticate(d.Auth, d.Logger))

	r.NotFound(notFoundHandler(d.Web))
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusMethodNotAllowed, "method_not_allowed", nil)
	})

	r.Get(EventsPath, eventsHandler(d))
	r.Get(TerminalPath, terminalHandler(d))
	r.Get(googleauth.CallbackPath, googleCallback(d.Google))

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
