package httpapi

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/mfa"
	"github.com/fprambs/sentinel/internal/core/passkey"
)

// secondFactors lists the ways a user can give a second factor: an authenticator app, passkeys and recovery codes. It is empty for a user
// who has none, who signs in with the password alone.
func (s *server) secondFactors(ctx context.Context, userID uuid.UUID) ([]MfaChallengeMethods, error) {
	status, err := s.deps.MFA.Status(ctx, userID)
	if err != nil {
		return nil, err
	}
	keys, err := s.deps.Passkeys.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	var methods []MfaChallengeMethods
	if status.TOTP {
		methods = append(methods, MfaChallengeMethodsTotp)
	}
	if len(keys) > 0 {
		methods = append(methods, MfaChallengeMethodsPasskey)
	}
	if len(methods) > 0 && status.RecoveryLeft > 0 {
		methods = append(methods, MfaChallengeMethodsRecovery)
	}
	return methods, nil
}

// clearRecoveryWithoutFactors removes the recovery codes of a user who has no second factor left, since they would protect nothing.
func (s *server) clearRecoveryWithoutFactors(ctx context.Context, userID uuid.UUID) error {
	methods, err := s.secondFactors(ctx, userID)
	if err != nil {
		return err
	}
	for _, method := range methods {
		if method != MfaChallengeMethodsRecovery {
			return nil
		}
	}
	return s.deps.MFA.ClearRecovery(ctx, userID)
}

// openSession starts the session of a user who gave every factor, and answers as a sign-in does.
func (s *server) openSession(ctx context.Context, userID uuid.UUID) (Problem, *SessionInfo, *string, error) {
	user, err := s.deps.Auth.UserByID(ctx, userID)
	if err != nil {
		return Problem{}, nil, nil, err
	}
	if user.Disabled {
		return problemBody(403, "user_disabled", nil), nil, nil, nil
	}
	if _, err := s.deps.Workspaces.EnsurePersonal(ctx, user.ID); err != nil {
		return Problem{}, nil, nil, err
	}
	token, principal, err := s.deps.Auth.StartSession(ctx, user, clientFrom(ctx))
	if err != nil {
		return Problem{}, nil, nil, err
	}
	cookie := newSessionCookie(token, s.deps.Cookie)
	info := s.sessionInfo(principal)
	return Problem{}, &info, &cookie, nil
}

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
	denied, info, cookie, err := s.openSession(ctx, userID)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return VerifyMfa403ApplicationProblemPlusJSONResponse(denied), nil
	}
	return VerifyMfa200JSONResponse{Body: *info, Headers: VerifyMfa200ResponseHeaders{SetCookie: cookie}}, nil
}

// GetSecurity reports the second factor of the signed-in account.
func (s *server) GetSecurity(ctx context.Context, _ GetSecurityRequestObject) (GetSecurityResponseObject, error) {
	p, _ := principalFrom(ctx)
	status, err := s.deps.MFA.Status(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	keys, err := s.deps.Passkeys.List(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	out := GetSecurity200JSONResponse{Totp: status.TOTP, RecoveryCodesLeft: status.RecoveryLeft, Passkeys: make([]Passkey, 0, len(keys))}
	for _, k := range keys {
		item := Passkey{Id: k.ID, Name: k.Name, CreatedAt: k.CreatedAt}
		if !k.LastUsedAt.IsZero() {
			item.LastUsedAt = &k.LastUsedAt
		}
		out.Passkeys = append(out.Passkeys, item)
	}
	return out, nil
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
	if err := s.clearRecoveryWithoutFactors(ctx, p.User.ID); err != nil {
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
	methods, err := s.secondFactors(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	if len(methods) == 0 {
		return RegenerateRecoveryCodes409ApplicationProblemPlusJSONResponse(problemBody(409, "totp_not_enabled", nil)), nil
	}
	codes, err := s.deps.MFA.RegenerateRecovery(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	return RegenerateRecoveryCodes200JSONResponse{Codes: codes}, nil
}

// asObject turns the JSON options of a ceremony into the object the API answers with.
func asObject(raw json.RawMessage) map[string]any {
	var object map[string]any
	_ = json.Unmarshal(raw, &object)
	return object
}

// BeginPasskeyLogin gives the options to sign in with a passkey.
func (s *server) BeginPasskeyLogin(ctx context.Context, request BeginPasskeyLoginRequestObject) (BeginPasskeyLoginResponseObject, error) {
	client := clientFrom(ctx)
	if ok, wait := s.deps.Limits.MFA.Allow(mfaKey("passkey:ip:" + client.IP)); !ok {
		return BeginPasskeyLogin429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	userID, err := s.deps.MFA.OpenChallenge(request.Body.Challenge)
	if err != nil {
		return BeginPasskeyLogin401ApplicationProblemPlusJSONResponse(problemBody(401, "invalid_challenge", nil)), nil
	}
	user, err := s.deps.Auth.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	options, ceremony, err := s.deps.Passkeys.BeginLogin(ctx, passkey.Account{ID: user.ID, Email: user.Email})
	if errors.Is(err, passkey.ErrNoPasskeys) {
		return BeginPasskeyLogin409ApplicationProblemPlusJSONResponse(problemBody(409, "no_passkeys", nil)), nil
	}
	if err != nil {
		return nil, err
	}
	return BeginPasskeyLogin200JSONResponse{Ceremony: ceremony, Options: asObject(options)}, nil
}

// FinishPasskeyLogin finishes signing in with the answer of a passkey.
func (s *server) FinishPasskeyLogin(ctx context.Context, request FinishPasskeyLoginRequestObject) (FinishPasskeyLoginResponseObject, error) {
	client := clientFrom(ctx)
	if ok, wait := s.deps.Limits.MFA.Allow(mfaKey("passkey:ip:" + client.IP)); !ok {
		return FinishPasskeyLogin429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	userID, err := s.deps.MFA.OpenChallenge(request.Body.Challenge)
	if err != nil {
		return FinishPasskeyLogin401ApplicationProblemPlusJSONResponse(problemBody(401, "invalid_challenge", nil)), nil
	}
	if ok, wait := s.deps.Limits.MFA.Allow(mfaKey("account:" + userID.String())); !ok {
		return FinishPasskeyLogin429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	user, err := s.deps.Auth.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(request.Body.Credential)
	err = s.deps.Passkeys.FinishLogin(ctx, passkey.Account{ID: user.ID, Email: user.Email}, request.Body.Ceremony, body)
	if errors.Is(err, passkey.ErrInvalid) || errors.Is(err, passkey.ErrNoCeremony) {
		return FinishPasskeyLogin401ApplicationProblemPlusJSONResponse(problemBody(401, "invalid_passkey", nil)), nil
	}
	if err != nil {
		return nil, err
	}
	denied, info, cookie, err := s.openSession(ctx, userID)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return FinishPasskeyLogin403ApplicationProblemPlusJSONResponse(denied), nil
	}
	return FinishPasskeyLogin200JSONResponse{Body: *info, Headers: FinishPasskeyLogin200ResponseHeaders{SetCookie: cookie}}, nil
}

// BeginPasskeyRegistration gives the options to register a passkey.
func (s *server) BeginPasskeyRegistration(ctx context.Context, _ BeginPasskeyRegistrationRequestObject) (BeginPasskeyRegistrationResponseObject, error) {
	p, _ := principalFrom(ctx)
	options, ceremony, err := s.deps.Passkeys.BeginRegistration(ctx, passkey.Account{ID: p.User.ID, Email: p.User.Email})
	if errors.Is(err, passkey.ErrTooMany) {
		return BeginPasskeyRegistration409ApplicationProblemPlusJSONResponse(problemBody(409, "too_many_passkeys", nil)), nil
	}
	if err != nil {
		return nil, err
	}
	return BeginPasskeyRegistration200JSONResponse{Ceremony: ceremony, Options: asObject(options)}, nil
}

// FinishPasskeyRegistration verifies the answer of the browser and stores the passkey.
func (s *server) FinishPasskeyRegistration(ctx context.Context, request FinishPasskeyRegistrationRequestObject) (FinishPasskeyRegistrationResponseObject, error) {
	p, _ := principalFrom(ctx)
	body, _ := json.Marshal(request.Body.Credential)
	created, err := s.deps.Passkeys.FinishRegistration(ctx, passkey.Account{ID: p.User.ID, Email: p.User.Email}, request.Body.Ceremony, request.Body.Name, body)
	switch {
	case errors.Is(err, passkey.ErrInvalidName):
		return FinishPasskeyRegistration422ApplicationProblemPlusJSONResponse(problemBody(422, "invalid_name", nil)), nil
	case errors.Is(err, passkey.ErrInvalid):
		return FinishPasskeyRegistration422ApplicationProblemPlusJSONResponse(problemBody(422, "invalid_passkey", nil)), nil
	case errors.Is(err, passkey.ErrNoCeremony):
		return FinishPasskeyRegistration409ApplicationProblemPlusJSONResponse(problemBody(409, "ceremony_expired", nil)), nil
	case err != nil:
		return nil, err
	}
	out := FinishPasskeyRegistration201JSONResponse{Passkey: Passkey{Id: created.ID, Name: created.Name, CreatedAt: created.CreatedAt}}
	codes, err := s.deps.MFA.EnsureRecovery(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	if codes != nil {
		out.RecoveryCodes = &codes
	}
	return out, nil
}

// RemovePasskey removes a passkey after asking for the password again.
func (s *server) RemovePasskey(ctx context.Context, request RemovePasskeyRequestObject) (RemovePasskeyResponseObject, error) {
	p, _ := principalFrom(ctx)
	if ok, wait := s.deps.Limits.MFA.Allow(mfaKey("password:" + p.User.ID.String())); !ok {
		return RemovePasskey429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	if err := s.deps.Auth.VerifyPassword(ctx, p.User, request.Body.Password); errors.Is(err, auth.ErrInvalidCredentials) {
		return RemovePasskey403ApplicationProblemPlusJSONResponse(problemBody(403, "invalid_password", nil)), nil
	} else if err != nil {
		return nil, err
	}
	err := s.deps.Passkeys.Delete(ctx, p.User.ID, request.PasskeyId)
	if errors.Is(err, passkey.ErrNotFound) {
		return RemovePasskey404ApplicationProblemPlusJSONResponse(problemBody(404, "passkey_not_found", nil)), nil
	}
	if err != nil {
		return nil, err
	}
	if err := s.clearRecoveryWithoutFactors(ctx, p.User.ID); err != nil {
		return nil, err
	}
	return RemovePasskey204Response{}, nil
}
