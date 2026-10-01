package httpapi

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/fprambs/transferly/internal/core/auth"
)

const readinessTimeout = 2 * time.Second

// server implements [StrictServerInterface] on top of the core services.
type server struct {
	deps Deps
}

// newServer returns the strict server for the given dependencies.
func newServer(d Deps) *server {
	return &server{deps: d}
}

// GetHealth answers liveness probes; it never touches dependencies.
func (s *server) GetHealth(context.Context, GetHealthRequestObject) (GetHealthResponseObject, error) {
	return GetHealth200JSONResponse{Status: Ok}, nil
}

// GetReadiness answers readiness probes by pinging the readiness checker within a timeout.
// Failures are logged and reported as 503 without exposing internal details.
func (s *server) GetReadiness(ctx context.Context, _ GetReadinessRequestObject) (GetReadinessResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()

	if err := s.deps.Readiness.Ping(ctx); err != nil {
		s.deps.Logger.WarnContext(ctx, "readiness check failed", "error", err)
		return GetReadiness503JSONResponse{Status: Unavailable}, nil
	}
	return GetReadiness200JSONResponse{Status: Ok}, nil
}

// retryAfter converts a wait to whole seconds, rounding up so clients never retry too early.
func retryAfter(d time.Duration) *int {
	seconds := int(math.Ceil(d.Seconds()))
	return &seconds
}

// userDTO converts an account to its public representation.
func userDTO(u auth.User) User {
	return User{Id: u.ID, Email: u.Email, Role: UserRole(u.Role), Locale: u.Locale, Timezone: u.Timezone, Theme: u.Theme, FileDoubleClick: UserFileDoubleClick(u.FileDoubleClick)}
}

// sessionInfo builds the session document, including the anti-CSRF token of the session.
func (s *server) sessionInfo(p auth.Principal) SessionInfo {
	return SessionInfo{User: userDTO(p.User), CsrfToken: auth.CSRFToken(s.deps.InstanceKey, p.Session.ID)}
}

// isValidation reports whether err is an input validation failure, and returns its stable code.
func isValidation(err error) (string, bool) {
	switch {
	case errors.Is(err, auth.ErrInvalidEmail):
		return "invalid_email", true
	case errors.Is(err, auth.ErrWeakPassword):
		return "weak_password", true
	case errors.Is(err, auth.ErrInvalidLocale):
		return "invalid_locale", true
	case errors.Is(err, auth.ErrInvalidTimezone):
		return "invalid_timezone", true
	case errors.Is(err, auth.ErrInvalidTheme):
		return "invalid_theme", true
	case errors.Is(err, auth.ErrInvalidFileDoubleClick):
		return "invalid_file_double_click", true
	}
	return "", false
}
