package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/vault"
)

// vaultKey is the key ring identity of the request's session.
func vaultKey(p authenticated) string {
	return p.Session.ID.String()
}

// closeCredentials drops the passwords typed in a session: it ended or its vault was locked (ADR-0010).
func (s *server) closeCredentials(session string) {
	s.deps.Credentials.CloseSession(session)
}

// vaultAttempt counts one attempt to prove knowledge of a vault secret by the signed-in account.
func (s *server) vaultAttempt(p authenticated) (bool, time.Duration) {
	return s.deps.Limits.VaultAttempts.Allow("vault:" + p.User.ID.String())
}

// tooManyAttempts builds the 429 answer with its Retry-After header.
func tooManyAttempts(wait time.Duration) RateLimitedApplicationProblemPlusJSONResponse {
	return RateLimitedApplicationProblemPlusJSONResponse{
		Body:    problemBody(429, "rate_limited", nil),
		Headers: RateLimitedResponseHeaders{RetryAfter: retryAfter(wait)},
	}
}

// GetVault reports whether the user has a vault and whether this session has unlocked it.
func (s *server) GetVault(ctx context.Context, _ GetVaultRequestObject) (GetVaultResponseObject, error) {
	p, _ := principalFrom(ctx)
	status, err := s.deps.Vault.Status(ctx, p.User.ID, vaultKey(p))
	if err != nil {
		return nil, err
	}
	return GetVault200JSONResponse{Initialized: status.Initialized, Unlocked: status.Unlocked}, nil
}

// SetupVault creates the vault and returns the recovery kit, once.
func (s *server) SetupVault(ctx context.Context, request SetupVaultRequestObject) (SetupVaultResponseObject, error) {
	p, _ := principalFrom(ctx)
	kit, err := s.deps.Vault.Setup(ctx, p.User.ID, vaultKey(p), request.Body.MasterPassword)
	switch {
	case errors.Is(err, vault.ErrWeakPassword):
		return SetupVault422ApplicationProblemPlusJSONResponse(problemBody(422, "weak_password", nil)), nil
	case errors.Is(err, vault.ErrAlreadyInitialized):
		return SetupVault409ApplicationProblemPlusJSONResponse(problemBody(409, "vault_exists", nil)), nil
	case err != nil:
		return nil, err
	}
	if err := s.deps.Keys.Refresh(ctx, p.User.ID, vaultKey(p)); err != nil {
		return nil, err
	}
	return SetupVault201JSONResponse{RecoveryKit: kit}, nil
}

// UnlockVault opens the vault for this session with the master password.
func (s *server) UnlockVault(ctx context.Context, request UnlockVaultRequestObject) (UnlockVaultResponseObject, error) {
	p, _ := principalFrom(ctx)
	if ok, wait := s.vaultAttempt(p); !ok {
		return UnlockVault429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	err := s.deps.Vault.Unlock(ctx, p.User.ID, vaultKey(p), request.Body.MasterPassword)
	switch {
	case errors.Is(err, vault.ErrWrongSecret):
		return UnlockVault403ApplicationProblemPlusJSONResponse(problemBody(403, "invalid_secret", nil)), nil
	case errors.Is(err, vault.ErrNotInitialized):
		return UnlockVault409ApplicationProblemPlusJSONResponse(problemBody(409, "vault_not_initialized", nil)), nil
	case err != nil:
		return nil, err
	}
	if err := s.deps.Keys.Refresh(ctx, p.User.ID, vaultKey(p)); err != nil {
		return nil, err
	}
	return UnlockVault204Response{}, nil
}

// LockVault removes the key of this session from memory and closes the user's connections to remote servers.
func (s *server) LockVault(ctx context.Context, _ LockVaultRequestObject) (LockVaultResponseObject, error) {
	p, _ := principalFrom(ctx)
	s.deps.Vault.Lock(vaultKey(p))
	s.deps.Connections.CloseUser(p.User.ID)
	s.closeCredentials(vaultKey(p))
	s.deps.Events.Publish(ports.Event{Type: "vault.locked", UserID: p.User.ID})
	return LockVault204Response{}, nil
}

// ChangeMasterPassword replaces the master password.
func (s *server) ChangeMasterPassword(ctx context.Context, request ChangeMasterPasswordRequestObject) (ChangeMasterPasswordResponseObject, error) {
	p, _ := principalFrom(ctx)
	if ok, wait := s.vaultAttempt(p); !ok {
		return ChangeMasterPassword429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	err := s.deps.Vault.ChangePassword(ctx, p.User.ID, vaultKey(p), request.Body.CurrentPassword, request.Body.NewPassword)
	switch {
	case errors.Is(err, vault.ErrWeakPassword):
		return ChangeMasterPassword422ApplicationProblemPlusJSONResponse(problemBody(422, "weak_password", nil)), nil
	case errors.Is(err, vault.ErrWrongSecret):
		return ChangeMasterPassword403ApplicationProblemPlusJSONResponse(problemBody(403, "invalid_secret", nil)), nil
	case errors.Is(err, vault.ErrNotInitialized):
		return ChangeMasterPassword409ApplicationProblemPlusJSONResponse(problemBody(409, "vault_not_initialized", nil)), nil
	case err != nil:
		return nil, err
	}
	return ChangeMasterPassword204Response{}, nil
}

// RecoverVault sets a new master password using the recovery kit.
func (s *server) RecoverVault(ctx context.Context, request RecoverVaultRequestObject) (RecoverVaultResponseObject, error) {
	p, _ := principalFrom(ctx)
	if ok, wait := s.vaultAttempt(p); !ok {
		return RecoverVault429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	err := s.deps.Vault.Recover(ctx, p.User.ID, vaultKey(p), request.Body.RecoveryKit, request.Body.NewPassword)
	switch {
	case errors.Is(err, vault.ErrWeakPassword):
		return RecoverVault422ApplicationProblemPlusJSONResponse(problemBody(422, "weak_password", nil)), nil
	case errors.Is(err, vault.ErrWrongSecret):
		return RecoverVault403ApplicationProblemPlusJSONResponse(problemBody(403, "invalid_secret", nil)), nil
	case errors.Is(err, vault.ErrNotInitialized):
		return RecoverVault409ApplicationProblemPlusJSONResponse(problemBody(409, "vault_not_initialized", nil)), nil
	case err != nil:
		return nil, err
	}
	if err := s.deps.Keys.Refresh(ctx, p.User.ID, vaultKey(p)); err != nil {
		return nil, err
	}
	return RecoverVault204Response{}, nil
}

// RegenerateRecoveryKit replaces the recovery kit and returns the new one, once.
func (s *server) RegenerateRecoveryKit(ctx context.Context, request RegenerateRecoveryKitRequestObject) (RegenerateRecoveryKitResponseObject, error) {
	p, _ := principalFrom(ctx)
	if ok, wait := s.vaultAttempt(p); !ok {
		return RegenerateRecoveryKit429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	kit, err := s.deps.Vault.RegenerateRecovery(ctx, p.User.ID, request.Body.MasterPassword)
	switch {
	case errors.Is(err, vault.ErrWrongSecret):
		return RegenerateRecoveryKit403ApplicationProblemPlusJSONResponse(problemBody(403, "invalid_secret", nil)), nil
	case errors.Is(err, vault.ErrNotInitialized):
		return RegenerateRecoveryKit409ApplicationProblemPlusJSONResponse(problemBody(409, "vault_not_initialized", nil)), nil
	case err != nil:
		return nil, err
	}
	return RegenerateRecoveryKit200JSONResponse{RecoveryKit: kit}, nil
}
