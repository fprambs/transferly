package httpapi

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
)

// ApproveCli approves a sign-in of the terminal client from the browser, asking for the password again (ADR-0029). A session of the
// terminal client cannot approve another one.
func (s *server) ApproveCli(ctx context.Context, request ApproveCliRequestObject) (ApproveCliResponseObject, error) {
	p, _ := principalFrom(ctx)
	if p.Session.Kind == auth.SessionCLI {
		return ApproveCli403ApplicationProblemPlusJSONResponse(problemBody(403, "browser_session_required", nil)), nil
	}
	if ok, wait := s.deps.Limits.MFA.Allow(mfaKey("password:" + p.User.ID.String())); !ok {
		return ApproveCli429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	body := request.Body
	code, err := s.deps.Auth.ApproveCLI(ctx, p.User, body.Password, auth.CLIRequest{
		CodeChallenge: body.CodeChallenge, RedirectURI: deref(body.RedirectUri), ClientName: body.ClientName, ClientHost: deref(body.ClientHost),
	}, clientFrom(ctx))
	switch {
	case errors.Is(err, auth.ErrCLILoginDisabled):
		return ApproveCli403ApplicationProblemPlusJSONResponse(problemBody(403, "cli_login_disabled", nil)), nil
	case errors.Is(err, auth.ErrInvalidCredentials):
		return ApproveCli403ApplicationProblemPlusJSONResponse(problemBody(403, "invalid_password", nil)), nil
	case errors.Is(err, auth.ErrInvalidRedirect):
		return ApproveCli400ApplicationProblemPlusJSONResponse(problemBody(400, "invalid_redirect", nil)), nil
	case errors.Is(err, auth.ErrInvalidCLIRequest):
		return ApproveCli400ApplicationProblemPlusJSONResponse(problemBody(400, "invalid_cli_request", nil)), nil
	case err != nil:
		return nil, err
	}
	return ApproveCli201JSONResponse{Code: code}, nil
}

// ExchangeCliCode turns an approved code into a session of the terminal client, whose token goes back in the body and never as a
// cookie. Attempts are limited per client address.
func (s *server) ExchangeCliCode(ctx context.Context, request ExchangeCliCodeRequestObject) (ExchangeCliCodeResponseObject, error) {
	client := clientFrom(ctx)
	if ok, wait := s.deps.Limits.CLIToken.Allow("cli-token:ip:" + client.IP); !ok {
		return ExchangeCliCode429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	body := request.Body
	token, principal, err := s.deps.Auth.ExchangeCLI(ctx, auth.CLIExchange{
		Code: body.Code, CodeVerifier: body.CodeVerifier, RedirectURI: deref(body.RedirectUri),
	}, client)
	switch {
	case errors.Is(err, auth.ErrCLILoginDisabled):
		return ExchangeCliCode403ApplicationProblemPlusJSONResponse(problemBody(403, "cli_login_disabled", nil)), nil
	case errors.Is(err, auth.ErrInvalidGrant):
		return ExchangeCliCode400ApplicationProblemPlusJSONResponse(problemBody(400, "invalid_grant", nil)), nil
	case err != nil:
		return nil, err
	}
	return ExchangeCliCode200JSONResponse{Token: token, ExpiresAt: principal.Session.ExpiresAt, User: userDTO(principal.User)}, nil
}

// CreateCliLink registers a request of the terminal client under a short link to the approval page. Attempts are limited per client
// address, with the exchanges of codes.
func (s *server) CreateCliLink(ctx context.Context, request CreateCliLinkRequestObject) (CreateCliLinkResponseObject, error) {
	client := clientFrom(ctx)
	if ok, wait := s.deps.Limits.CLIToken.Allow("cli-link:ip:" + client.IP); !ok {
		return CreateCliLink429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	body := request.Body
	link, err := s.deps.Auth.CreateCLILink(ctx, auth.CLIRequest{
		CodeChallenge: body.CodeChallenge, RedirectURI: deref(body.RedirectUri), ClientName: body.ClientName, ClientHost: deref(body.ClientHost),
	}, deref(body.State), client)
	switch {
	case errors.Is(err, auth.ErrCLILoginDisabled):
		return CreateCliLink403ApplicationProblemPlusJSONResponse(problemBody(403, "cli_login_disabled", nil)), nil
	case errors.Is(err, auth.ErrInvalidRedirect):
		return CreateCliLink400ApplicationProblemPlusJSONResponse(problemBody(400, "invalid_redirect", nil)), nil
	case errors.Is(err, auth.ErrInvalidCLIRequest):
		return CreateCliLink400ApplicationProblemPlusJSONResponse(problemBody(400, "invalid_cli_request", nil)), nil
	case err != nil:
		return nil, err
	}
	code := auth.FormatLinkCode(link.Code)
	return CreateCliLink201JSONResponse{Code: code, Path: "/cli/a/" + code, ExpiresAt: link.ExpiresAt}, nil
}

// GetCliLink returns the request of the terminal client behind a short link, for the approval page to show.
func (s *server) GetCliLink(ctx context.Context, request GetCliLinkRequestObject) (GetCliLinkResponseObject, error) {
	link, err := s.deps.Auth.CLILinkRequest(ctx, request.Code)
	if errors.Is(err, auth.ErrNotFound) {
		return GetCliLink404ApplicationProblemPlusJSONResponse(problemBody(404, "cli_link_not_found", nil)), nil
	} else if err != nil {
		return nil, err
	}
	req := link.Request
	return GetCliLink200JSONResponse{
		CodeChallenge: req.CodeChallenge, ClientName: req.ClientName, ClientHost: optional(req.ClientHost),
		RedirectUri: optional(req.RedirectURI), State: optional(link.State),
	}, nil
}

// ListCliSessions lists the sessions of the terminal client of the signed-in account, marking the one making the request.
func (s *server) ListCliSessions(ctx context.Context, _ ListCliSessionsRequestObject) (ListCliSessionsResponseObject, error) {
	p, _ := principalFrom(ctx)
	sessions, err := s.deps.Auth.CLISessions(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	out := make(ListCliSessions200JSONResponse, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, CliSession{
			Id: session.ID, Name: session.Name, Host: session.Host, Ip: session.IP, UserAgent: session.UserAgent,
			CreatedAt: session.CreatedAt, LastSeenAt: session.LastSeenAt, ExpiresAt: session.ExpiresAt, Current: session.ID == p.Session.ID,
		})
	}
	return out, nil
}

// RevokeSession ends a session of the signed-in account at once: its row, its vault keys, the passwords typed in it, its event
// streams and its terminals, as signing out does for the current one.
func (s *server) RevokeSession(ctx context.Context, request RevokeSessionRequestObject) (RevokeSessionResponseObject, error) {
	p, _ := principalFrom(ctx)
	id := uuid.UUID(request.SessionId)
	if err := s.deps.Auth.RevokeSession(ctx, p.User.ID, id); errors.Is(err, auth.ErrNotFound) {
		return RevokeSession404ApplicationProblemPlusJSONResponse(problemBody(404, "session_not_found", nil)), nil
	} else if err != nil {
		return nil, err
	}
	key := id.String()
	s.deps.Vault.Lock(key)
	s.closeCredentials(key)
	s.deps.Events.CloseSession(key)
	s.deps.terminals.revoke(key, p.User.ID, uuid.Nil)
	return RevokeSession204Response{}, nil
}
