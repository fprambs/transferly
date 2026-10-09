package httpapi

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
)

// GetSetupStatus reports whether the first administrator still has to be created.
func (s *server) GetSetupStatus(ctx context.Context, _ GetSetupStatusRequestObject) (GetSetupStatusResponseObject, error) {
	required, err := s.deps.Auth.SetupRequired(ctx)
	if err != nil {
		return nil, err
	}
	return GetSetupStatus200JSONResponse{Required: required}, nil
}

// Setup creates the first administrator and signs them in.
func (s *server) Setup(ctx context.Context, request SetupRequestObject) (SetupResponseObject, error) {
	client := clientFrom(ctx)
	if ok, wait := s.deps.Limits.SetupIP.Allow("setup:" + client.IP); !ok {
		return Setup429ApplicationProblemPlusJSONResponse{RateLimitedApplicationProblemPlusJSONResponse{
			Body:    problemBody(429, "rate_limited", nil),
			Headers: RateLimitedResponseHeaders{RetryAfter: retryAfter(wait)},
		}}, nil
	}

	user, err := s.deps.Auth.Setup(ctx, request.Body.Email, request.Body.Password)
	if code, ok := isValidation(err); ok {
		return Setup422ApplicationProblemPlusJSONResponse(problemBody(422, code, nil)), nil
	}
	if errors.Is(err, auth.ErrSetupComplete) {
		return Setup409ApplicationProblemPlusJSONResponse(problemBody(409, "setup_complete", nil)), nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.deps.Workspaces.EnsurePersonal(ctx, user.ID); err != nil {
		return nil, err
	}
	token, principal, err := s.deps.Auth.StartSession(ctx, user, client)
	if err != nil {
		return nil, err
	}
	cookie := newSessionCookie(token, s.deps.Cookie)
	return Setup201JSONResponse{Body: s.sessionInfo(principal), Headers: Setup201ResponseHeaders{SetCookie: &cookie}}, nil
}

// Login signs a user in.
func (s *server) Login(ctx context.Context, request LoginRequestObject) (LoginResponseObject, error) {
	client := clientFrom(ctx)
	account := strings.ToLower(strings.TrimSpace(request.Body.Email))
	ipOK, ipWait := s.deps.Limits.LoginIP.Allow("login:ip:" + client.IP)
	accountOK, accountWait := s.deps.Limits.LoginAccount.Allow("login:account:" + account)
	if !ipOK || !accountOK {
		return Login429ApplicationProblemPlusJSONResponse{RateLimitedApplicationProblemPlusJSONResponse{
			Body:    problemBody(429, "rate_limited", nil),
			Headers: RateLimitedResponseHeaders{RetryAfter: retryAfter(max(ipWait, accountWait))},
		}}, nil
	}

	user, err := s.deps.Auth.CheckPassword(ctx, request.Body.Email, request.Body.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		return Login401ApplicationProblemPlusJSONResponse(problemBody(401, "invalid_credentials", nil)), nil
	}
	if errors.Is(err, auth.ErrUserDisabled) {
		return Login403ApplicationProblemPlusJSONResponse(problemBody(403, "user_disabled", nil)), nil
	}
	if err != nil {
		return nil, err
	}
	methods, err := s.secondFactors(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if len(methods) > 0 {
		return Login202JSONResponse{Challenge: s.deps.MFA.Challenge(user.ID), Methods: methods}, nil
	}
	if _, err := s.deps.Workspaces.EnsurePersonal(ctx, user.ID); err != nil {
		return nil, err
	}
	token, principal, err := s.deps.Auth.StartSession(ctx, user, client)
	if err != nil {
		return nil, err
	}
	cookie := newSessionCookie(token, s.deps.Cookie)
	return Login200JSONResponse{Body: s.sessionInfo(principal), Headers: Login200ResponseHeaders{SetCookie: &cookie}}, nil
}

// Logout ends the current session and clears its cookie.
func (s *server) Logout(ctx context.Context, _ LogoutRequestObject) (LogoutResponseObject, error) {
	p, _ := principalFrom(ctx)
	s.deps.Vault.Lock(vaultKey(p))
	s.deps.Connections.CloseUser(p.User.ID)
	s.deps.Quick.CloseUser(p.User.ID)
	s.closeCredentials(vaultKey(p))
	s.deps.Events.CloseSession(vaultKey(p))
	if err := s.deps.Auth.Logout(ctx, p.token); err != nil {
		return nil, err
	}
	s.deps.terminals.revoke(vaultKey(p), p.User.ID, uuid.Nil)
	cleared := clearedSessionCookie(s.deps.Cookie)
	return Logout204Response{Headers: Logout204ResponseHeaders{SetCookie: &cleared}}, nil
}

// GetMe returns the signed-in user and the anti-CSRF token of the session.
func (s *server) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	p, _ := principalFrom(ctx)
	return GetMe200JSONResponse(s.sessionInfo(p.Principal)), nil
}

// UpdateMe changes the preferences of the signed-in user: interface language, time zone, theme and what a double click on a file does.
func (s *server) UpdateMe(ctx context.Context, request UpdateMeRequestObject) (UpdateMeResponseObject, error) {
	p, _ := principalFrom(ctx)
	b := request.Body
	prefs := auth.Preferences{Locale: deref(b.Locale), Timezone: deref(b.Timezone), Theme: deref(b.Theme)}
	if b.FileDoubleClick != nil {
		prefs.FileDoubleClick = string(*b.FileDoubleClick)
	}
	user, err := s.deps.Auth.UpdatePreferences(ctx, p.User, prefs)
	if code, ok := isValidation(err); ok {
		return UpdateMe422ApplicationProblemPlusJSONResponse(problemBody(422, code, nil)), nil
	}
	if err != nil {
		return nil, err
	}
	return UpdateMe200JSONResponse(userDTO(user)), nil
}

// deref returns the value a pointer points to, or the zero value for nil: an optional field left out of a request.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
