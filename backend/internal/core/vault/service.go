package vault

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	minMasterRunes = 12
	maxMasterBytes = 1024
)

var (
	// ErrNotInitialized means the user has not created a vault yet.
	ErrNotInitialized = errors.New("vault not initialized")
	// ErrAlreadyInitialized means the user already has a vault.
	ErrAlreadyInitialized = errors.New("vault already initialized")
	// ErrLocked means the operation needs the vault to be unlocked in this session.
	ErrLocked = errors.New("vault is locked")
	// ErrWeakPassword means the master password breaks the password policy.
	ErrWeakPassword = errors.New("master password does not meet the policy")
	// ErrNoVault is returned by a [Store] when the user has no vault.
	ErrNoVault = errors.New("no vault stored")
	// ErrVaultExists is returned by a [Store] when the user already has a vault.
	ErrVaultExists = errors.New("vault already stored")
)

// Store persists the vault enrollment of each user.
type Store interface {
	// CreateVault stores the enrollment of a user, or returns [ErrVaultExists].
	CreateVault(ctx context.Context, userID uuid.UUID, e Enrollment) error
	// Vault returns the enrollment of a user, or [ErrNoVault].
	Vault(ctx context.Context, userID uuid.UUID) (Enrollment, error)
	// UpdateVault replaces the enrollment of a user, or returns [ErrNoVault].
	UpdateVault(ctx context.Context, userID uuid.UUID, e Enrollment) error
}

// Status tells the state of a user's vault in one session.
type Status struct {
	// Initialized is true once the user created a vault.
	Initialized bool
	// Unlocked is true while the session holds the decrypted key.
	Unlocked bool
}

// Service implements the vault use cases of a user: creation, unlocking, locking, password changes and recovery.
// Unlocked keys live in the key ring, per session.
type Service struct {
	vault  *Vault
	store  Store
	ring   *KeyRing
	memory *sessionMemory
}

// NewService returns a service over the given vault, store and key ring.
func NewService(v *Vault, store Store, ring *KeyRing) *Service {
	return &Service{vault: v, store: store, ring: ring}
}

// checkPassword applies the master password policy: at least 12 characters and at most 1024 bytes.
func checkPassword(password string) error {
	if utf8.RuneCountInString(password) < minMasterRunes || len(password) > maxMasterBytes {
		return ErrWeakPassword
	}
	return nil
}

// enrollment loads the enrollment of a user, mapping a missing vault to [ErrNotInitialized].
func (s *Service) enrollment(ctx context.Context, userID uuid.UUID) (Enrollment, error) {
	e, err := s.store.Vault(ctx, userID)
	if errors.Is(err, ErrNoVault) {
		return Enrollment{}, ErrNotInitialized
	}
	return e, err
}

// Status reports whether the user has a vault and whether the session has unlocked it.
func (s *Service) Status(ctx context.Context, userID uuid.UUID, session string) (Status, error) {
	_, err := s.store.Vault(ctx, userID)
	if errors.Is(err, ErrNoVault) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	dek, unlocked := s.ring.Get(session)
	Wipe(dek)
	return Status{Initialized: true, Unlocked: unlocked}, nil
}

// Setup creates the vault of a user, unlocks it for the session and returns the recovery kit,
// which is shown once and never stored.
func (s *Service) Setup(ctx context.Context, userID uuid.UUID, session, password string) (string, error) {
	if err := checkPassword(password); err != nil {
		return "", err
	}
	if _, err := s.store.Vault(ctx, userID); err == nil {
		return "", ErrAlreadyInitialized
	} else if !errors.Is(err, ErrNoVault) {
		return "", err
	}
	enrollment, dek, kit, err := s.vault.Setup(ctx, password)
	if err != nil {
		return "", err
	}
	defer Wipe(dek)
	err = s.store.CreateVault(ctx, userID, enrollment)
	if errors.Is(err, ErrVaultExists) {
		return "", ErrAlreadyInitialized
	}
	if err != nil {
		return "", err
	}
	s.ring.Put(session, dek)
	return kit, nil
}

// Unlock opens the vault with the master password for the session. A wrong password is [ErrWrongSecret].
func (s *Service) Unlock(ctx context.Context, userID uuid.UUID, session, password string) error {
	e, err := s.enrollment(ctx, userID)
	if err != nil {
		return err
	}
	dek, err := s.vault.Unlock(ctx, password, e)
	if err != nil {
		return err
	}
	defer Wipe(dek)
	s.ring.Put(session, dek)
	return nil
}

// Lock removes the session's key, and the one kept for it when the service remembers sessions. It is safe to call on a session that
// is not unlocked.
func (s *Service) Lock(session string) {
	s.ring.Lock(session)
	s.forget(session)
}

// Key returns a copy of the session's key, or [ErrLocked]. The caller wipes it after use.
func (s *Service) Key(session string) ([]byte, error) {
	dek, ok := s.ring.Get(session)
	if !ok {
		return nil, ErrLocked
	}
	return dek, nil
}

// ChangePassword replaces the master password after checking the current one, and keeps the session unlocked.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, session, current, next string) error {
	if err := checkPassword(next); err != nil {
		return err
	}
	e, err := s.enrollment(ctx, userID)
	if err != nil {
		return err
	}
	dek, err := s.vault.Unlock(ctx, current, e)
	if err != nil {
		return err
	}
	defer Wipe(dek)
	updated, err := s.vault.ChangePassword(ctx, dek, next, e)
	if err != nil {
		return err
	}
	if err := s.store.UpdateVault(ctx, userID, updated); err != nil {
		return err
	}
	s.ring.Put(session, dek)
	return nil
}

// Recover opens the vault with the recovery kit, sets a new master password and unlocks the session.
// The recovery kit keeps working afterwards.
func (s *Service) Recover(ctx context.Context, userID uuid.UUID, session, kit, next string) error {
	if err := checkPassword(next); err != nil {
		return err
	}
	e, err := s.enrollment(ctx, userID)
	if err != nil {
		return err
	}
	dek, err := s.vault.Recover(kit, e)
	if err != nil {
		return err
	}
	defer Wipe(dek)
	updated, err := s.vault.ChangePassword(ctx, dek, next, e)
	if err != nil {
		return err
	}
	if err := s.store.UpdateVault(ctx, userID, updated); err != nil {
		return err
	}
	s.ring.Put(session, dek)
	return nil
}

// RegenerateRecovery replaces the recovery kit after checking the master password, and returns the new kit.
// The previous kit stops working.
func (s *Service) RegenerateRecovery(ctx context.Context, userID uuid.UUID, password string) (string, error) {
	e, err := s.enrollment(ctx, userID)
	if err != nil {
		return "", err
	}
	dek, err := s.vault.Unlock(ctx, password, e)
	if err != nil {
		return "", err
	}
	defer Wipe(dek)
	updated, kit, err := s.vault.RegenerateRecovery(dek, e)
	if err != nil {
		return "", err
	}
	if err := s.store.UpdateVault(ctx, userID, updated); err != nil {
		return "", err
	}
	return kit, nil
}
