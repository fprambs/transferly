package httpapi

import (
	"context"
	"errors"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/mfa"
)

// mfaMethods are the ways a challenge can be answered.
var mfaMethods = []MfaChallengeMethods{Totp, Recovery}

// mfaKey is the key of the attempts limiter of an account, kept apart from the sign-in limiter.
func mfaKey(id string) string { return "mfa:" + id }

// VerifyMfa finishes signing in with a code of the authenticator app or a recovery code.
func (s *server) VerifyMfa(ctx context.Context, request VerifyMfaRequestObject) (VerifyMfaResponseObject, error) {
	client := clientFrom(ctx)
	ipOK, ipWait := s.deps.Limits.MFA.Allow(mfaKey("ip:" + client.IP))
	if !ipOK {
		return VerifyMfa429ApplicationProblemPlusJSONResponse{tooManyAttempts(ipWait)}, nil
	}
	userID, err := s.deps.MFA.OpenChallenge(request.Body.Challenge)
	if err != nil {
		return VerifyMfa401ApplicationProblemPlusJSONResponse(problemBody(401, "invalid_challenge", nil)), nil
	}
	if ok, wait := s.deps.Limits.MFA.Allow(mfaKey("account:" + userID.String())); !ok {
		return VerifyMfa429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	err = s.deps.MFA.VerifyCode(ctx, userID, request.Body.Code)
	if errors.Is(err, mfa.ErrInvalidCode) || errors.Is(err, mfa.ErrNotEnrolled) {
		return VerifyMfa401ApplicationProblemPlusJSONResponse(problemBody(401, "invalid_code", nil)), nil
	}
	if err != nil {
		return nil, err
	}
	user, err := s.deps.Auth.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.Disabled {
		return VerifyMfa403ApplicationProblemPlusJSONResponse(problemBody(403, "user_disabled", nil)), nil
	}
	if _, err := s.deps.Workspaces.EnsurePersonal(ctx, user.ID); err != nil {
		return nil, err
	}
	token, principal, err := s.deps.Auth.StartSession(ctx, user, client)
	if err != nil {
		return nil, err
	}
	cookie := newSessionCookie(token, s.deps.Cookie)
	return VerifyMfa200JSONResponse{Body: s.sessionInfo(principal), Headers: VerifyMfa200ResponseHeaders{SetCookie: &cookie}}, nil
}

// GetSecurity reports the second factor of the signed-in account.
func (s *server) GetSecurity(ctx context.Context, _ GetSecurityRequestObject) (GetSecurityResponseObject, error) {
	p, _ := principalFrom(ctx)
	status, err := s.deps.MFA.Status(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	return GetSecurity200JSONResponse{Totp: status.TOTP, RecoveryCodesLeft: status.RecoveryLeft}, nil
}

// BeginTotp starts enrolling an authenticator app.
func (s *server) BeginTotp(ctx context.Context, _ BeginTotpRequestObject) (BeginTotpResponseObject, error) {
	p, _ := principalFrom(ctx)
	secret, uri, err := s.deps.MFA.BeginTOTP(ctx, p.User.ID, p.User.Email)
	if err != nil {
		return nil, err
	}
	return BeginTotp200JSONResponse{Secret: secret, Uri: uri}, nil
}

// ConfirmTotp activates the authenticator app with a code and returns the recovery codes.
func (s *server) ConfirmTotp(ctx context.Context, request ConfirmTotpRequestObject) (ConfirmTotpResponseObject, error) {
	p, _ := principalFrom(ctx)
	if ok, wait := s.deps.Limits.MFA.Allow(mfaKey("account:" + p.User.ID.String())); !ok {
		return ConfirmTotp429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	codes, err := s.deps.MFA.ConfirmTOTP(ctx, p.User.ID, request.Body.Code)
	switch {
	case errors.Is(err, mfa.ErrInvalidCode):
		return ConfirmTotp422ApplicationProblemPlusJSONResponse(problemBody(422, "invalid_code", nil)), nil
	case errors.Is(err, mfa.ErrNoSetup):
		return ConfirmTotp409ApplicationProblemPlusJSONResponse(problemBody(409, "totp_not_started", nil)), nil
	case err != nil:
		return nil, err
	}
	return ConfirmTotp200JSONResponse{Codes: codes}, nil
}

// DisableTotp turns the second factor off after asking for the password again.
func (s *server) DisableTotp(ctx context.Context, request DisableTotpRequestObject) (DisableTotpResponseObject, error) {
	p, _ := principalFrom(ctx)
	if ok, wait := s.deps.Limits.MFA.Allow(mfaKey("password:" + p.User.ID.String())); !ok {
		return DisableTotp429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	if err := s.deps.Auth.VerifyPassword(ctx, p.User, request.Body.Password); errors.Is(err, auth.ErrInvalidCredentials) {
		return DisableTotp403ApplicationProblemPlusJSONResponse(problemBody(403, "invalid_password", nil)), nil
	} else if err != nil {
		return nil, err
	}
	if err := s.deps.MFA.Disable(ctx, p.User.ID); err != nil {
		return nil, err
	}
	return DisableTotp204Response{}, nil
}

// RegenerateRecoveryCodes replaces the recovery codes after asking for the password again.
func (s *server) RegenerateRecoveryCodes(ctx context.Context, request RegenerateRecoveryCodesRequestObject) (RegenerateRecoveryCodesResponseObject, error) {
	p, _ := principalFrom(ctx)
	if ok, wait := s.deps.Limits.MFA.Allow(mfaKey("password:" + p.User.ID.String())); !ok {
		return RegenerateRecoveryCodes429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	if err := s.deps.Auth.VerifyPassword(ctx, p.User, request.Body.Password); errors.Is(err, auth.ErrInvalidCredentials) {
		return RegenerateRecoveryCodes403ApplicationProblemPlusJSONResponse(problemBody(403, "invalid_password", nil)), nil
	} else if err != nil {
		return nil, err
	}
	codes, err := s.deps.MFA.RegenerateRecovery(ctx, p.User.ID)
	if errors.Is(err, mfa.ErrNotEnrolled) {
		return RegenerateRecoveryCodes409ApplicationProblemPlusJSONResponse(problemBody(409, "totp_not_enabled", nil)), nil
	}
	if err != nil {
		return nil, err
	}
	return RegenerateRecoveryCodes200JSONResponse{Codes: codes}, nil
}
