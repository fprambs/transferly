package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/fprambs/transferly/internal/core/auth"
)

const (
	sessionCookieName = "transferly_session"
	csrfHeader        = "X-CSRF-Token"
	maxUserAgent      = 256
	unknownIP         = "unknown"
)

// contextKey is the type of the values this package stores in request contexts.
type contextKey int

const (
	clientKey contextKey = iota
	principalKey
)

// authenticated is the signed-in user of a request together with the session token that proved it.
type authenticated struct {
	auth.Principal
	token string
	// bearer is set when the token came in the Authorization header, which only sessions of the terminal client use (ADR-0029).
	bearer bool
}

// clientFrom returns the client information stored by the clientInfo middleware.
func clientFrom(ctx context.Context) auth.ClientInfo {
	info, _ := ctx.Value(clientKey).(auth.ClientInfo)
	return info
}

// principalFrom returns the signed-in user of a request, if any.
func principalFrom(ctx context.Context) (authenticated, bool) {
	p, ok := ctx.Value(principalKey).(authenticated)
	return p, ok
}

// clientIPMiddleware determines the client address: the connection's address, or the address found in
// X-Forwarded-For when the connection comes from a trusted proxy. Untrusted headers are never believed.
func clientIPMiddleware(trustedProxies []string) func(http.Handler) http.Handler {
	if len(trustedProxies) == 0 {
		return middleware.ClientIPFromRemoteAddr
	}
	return middleware.ClientIPFromXFF(trustedProxies...)
}

// clientInfo stores the client address and user agent in the request context.
func clientInfo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := middleware.GetClientIP(r.Context())
		if _, err := netip.ParseAddr(ip); err != nil {
			ip = unknownIP
		}
		agent := r.UserAgent()
		if len(agent) > maxUserAgent {
			agent = agent[:maxUserAgent]
		}
		ctx := context.WithValue(r.Context(), clientKey, auth.ClientInfo{UserAgent: agent, IP: ip})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// vaultSessions keeps the vault of a session unlocked while the session is in use (ADR-0028).
type vaultSessions interface {
	// Resume runs before a request of the session, with its token.
	Resume(ctx context.Context, session, secret string)
	// Remember runs after a request of the session, with its token.
	Remember(ctx context.Context, session, secret string)
}

// authenticate resolves the session cookie of a browser, or the bearer token of the terminal client (ADR-0029), to a signed-in user.
// A token of one kind of session sent the other way, and missing, unknown and expired sessions, leave the request anonymous; a
// request with both credentials is refused, and otherwise only infrastructure failures stop it. Every request of a signed-in
// session keeps its vault unlocked, as long as it was.
func authenticate(service *auth.Service, vaults vaultSessions, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, bearer, ambiguous := sessionToken(r)
			if ambiguous {
				writeProblem(w, http.StatusBadRequest, "ambiguous_credentials", nil)
				return
			}
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			principal, err := service.Authenticate(r.Context(), token)
			if err == nil && (principal.Session.Kind == auth.SessionCLI) != bearer {
				err = auth.ErrUnauthenticated
			}
			switch {
			case err == nil:
				ctx := context.WithValue(r.Context(), principalKey, authenticated{Principal: principal, token: token, bearer: bearer})
				session := principal.Session.ID.String()
				vaults.Resume(ctx, session, token)
				next.ServeHTTP(w, r.WithContext(ctx))
				vaults.Remember(context.WithoutCancel(ctx), session, token)
			case isAnonymous(err):
				next.ServeHTTP(w, r)
			default:
				logger.ErrorContext(r.Context(), "session lookup failed", "error", err)
				writeProblem(w, http.StatusInternalServerError, "internal_error", nil)
			}
		})
	}
}

// sessionToken returns the session token of a request and whether it came as a bearer token. A request with both a bearer token
// and the session cookie is ambiguous and must be refused, so neither credential can stand in for the other. An Authorization header
// of another scheme is not a session token.
func sessionToken(r *http.Request) (token string, bearer, ambiguous bool) {
	scheme, value, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	cookie, err := r.Cookie(sessionCookieName)
	hasCookie := err == nil && cookie.Value != ""
	if strings.EqualFold(scheme, "Bearer") && value != "" {
		return value, true, hasCookie
	}
	if hasCookie {
		return cookie.Value, false, false
	}
	return "", false, false
}

// isAnonymous reports whether an authentication error simply means the caller is not signed in.
func isAnonymous(err error) bool {
	return errors.Is(err, auth.ErrUnauthenticated) || errors.Is(err, auth.ErrUserDisabled)
}

// isUnsafe reports whether the method can change state and therefore needs CSRF protection.
func isUnsafe(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	}
	return true
}

// originGuard rejects state-changing requests that a browser marks as cross-site.
//
// It checks Fetch Metadata (Sec-Fetch-Site) and the Origin header against the allowed origins. Requests
// that carry neither header come from non-browser clients and are not exposed to ambient cookies.
func originGuard(allowed []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isUnsafe(r.Method) {
				site := r.Header.Get("Sec-Fetch-Site")
				origin := r.Header.Get("Origin")
				crossSite := site != "" && site != "same-origin" && site != "none"
				badOrigin := origin != "" && !contains(allowed, origin)
				if crossSite || badOrigin {
					writeProblem(w, http.StatusForbidden, "cross_site_request", nil)
					return
				}
				if origin != "" {
					r = r.WithContext(context.WithValue(r.Context(), originKey, origin))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// originKeyType is the type of the key of the allowed origin a state-changing request came from, in its context.
type originKeyType struct{}

// originKey keys the origin of a state-changing request in its context.
var originKey originKeyType

// originFrom returns the allowed origin a state-changing request came from, or fallback when it sent none.
func originFrom(ctx context.Context, fallback string) string {
	if origin, ok := ctx.Value(originKey).(string); ok {
		return origin
	}
	return fallback
}

// contains reports whether list has an entry equal to s, ignoring case.
func contains(list []string, s string) bool {
	for _, item := range list {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

// protectedOperations lists, by generated operation name, the operations of the contract that require a session:
// those with a non-empty security requirement, whether their own or inherited from the document.
func protectedOperations(spec *openapi3.T) map[string]bool {
	protected := map[string]bool{}
	for _, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			requirements := spec.Security
			if op.Security != nil {
				requirements = *op.Security
			}
			if len(requirements) > 0 {
				protected[strings.ToUpper(op.OperationID[:1])+op.OperationID[1:]] = true
			}
		}
	}
	return protected
}

// requireSession enforces authentication on the protected operations and, for their state-changing
// requests of browser sessions, the anti-CSRF token; a bearer token is never sent by a browser on its own, so it needs none. It answers the problem itself and stops the operation.
func requireSession(instanceKey []byte, protected map[string]bool) StrictMiddlewareFunc {
	return func(next StrictHandlerFunc, operationID string) StrictHandlerFunc {
		if !protected[operationID] {
			return next
		}
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			p, ok := principalFrom(ctx)
			if !ok {
				writeProblem(w, http.StatusUnauthorized, "unauthenticated", nil)
				return nil, nil
			}
			if isUnsafe(r.Method) && !p.bearer && !auth.VerifyCSRF(instanceKey, p.Session.ID, r.Header.Get(csrfHeader)) {
				writeProblem(w, http.StatusForbidden, "csrf_invalid", nil)
				return nil, nil
			}
			return next(ctx, w, r, request)
		}
	}
}

// newSessionCookie renders the Set-Cookie value that stores a session token.
func newSessionCookie(token string, c CookieConfig) string {
	return (&http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true, Secure: c.Secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(c.MaxAge.Seconds()),
	}).String()
}

// clearedSessionCookie renders the Set-Cookie value that removes the session cookie.
func clearedSessionCookie(c CookieConfig) string {
	return (&http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true, Secure: c.Secure,
		SameSite: http.SameSiteLaxMode, MaxAge: -1,
	}).String()
}
