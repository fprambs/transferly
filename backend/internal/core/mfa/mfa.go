// Package mfa implements the second factor of an account: an authenticator app (TOTP), recovery codes and the short-lived challenge
// that separates the password step of signing in from the second one. See ADR-0007.
package mfa

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/ports"
)

const (
	// RecoveryCodes is how many recovery codes are created.
	RecoveryCodes = 10
	// ChallengeTTL is how long the password step of signing in stays valid.
	ChallengeTTL = 5 * time.Minute
	issuer       = "Sentinel"
)

// Errors returned by the package.
var (
	// ErrInvalidCode means the code is wrong, was already used or does not have the shape of a code.
	ErrInvalidCode = errors.New("invalid code")
	// ErrNotEnrolled means the account has no second factor to check, change or remove.
	ErrNotEnrolled = errors.New("no second factor")
	// ErrInvalidChallenge means the challenge is malformed, forged or expired.
	ErrInvalidChallenge = errors.New("invalid challenge")
	// ErrNoSetup means a code was confirmed without starting the setup.
	ErrNoSetup = errors.New("second factor setup not started")
)

// Factor is the stored authenticator app of a user.
type Factor struct {
	// Sealed is the secret sealed under the instance key.
	Sealed []byte
	// Confirmed tells whether the user proved they scanned the secret.
	Confirmed bool
	// LastStep is the last time step a code was accepted for, so no code works twice.
	LastStep int64
}

// Store persists second factors.
type Store interface {
	// Factor returns the authenticator app of a user, or [ErrNotEnrolled].
	Factor(ctx context.Context, userID uuid.UUID) (Factor, error)
	// SaveFactor stores a new unconfirmed secret, replacing any earlier one.
	SaveFactor(ctx context.Context, userID uuid.UUID, sealed []byte) error
	// ConfirmFactor marks the secret confirmed at a step and stores the hashes of the recovery codes, in one step.
	ConfirmFactor(ctx context.Context, userID uuid.UUID, step int64, recovery [][]byte) error
	// AdvanceStep records a step as used and reports whether it was newer than the last one, so a code is accepted once even under
	// concurrent requests.
	AdvanceStep(ctx context.Context, userID uuid.UUID, step int64) (bool, error)
	// DeleteFactor removes the authenticator app of a user.
	DeleteFactor(ctx context.Context, userID uuid.UUID) error
	// ReplaceRecovery stores a new set of recovery code hashes, unused, in place of the old one; an empty set removes them all.
	ReplaceRecovery(ctx context.Context, userID uuid.UUID, recovery [][]byte) error
	// UseRecovery marks the recovery code with that hash used and reports whether it was there and unused.
	UseRecovery(ctx context.Context, userID uuid.UUID, hash []byte) (bool, error)
	// RecoveryLeft counts the unused recovery codes of a user.
	RecoveryLeft(ctx context.Context, userID uuid.UUID) (int, error)
}

// Service implements the second factor use cases.
type Service struct {
	store  Store
	key    []byte
	random io.Reader
	clock  ports.Clock
}

// NewService returns a service whose secrets and challenges are protected with keys derived from instanceKey.
func NewService(store Store, instanceKey []byte, random io.Reader, clock ports.Clock) *Service {
	return &Service{store: store, key: instanceKey, random: random, clock: clock}
}

// derive returns a key for one purpose from the instance key.
func (s *Service) derive(purpose string) []byte {
	key, _ := hkdf.Key(sha256.New, s.key, nil, "sentinel/mfa/v1/"+purpose, 32)
	return key
}

// seal encrypts a TOTP secret for a user with AES-256-GCM, prefixing a random nonce.
func (s *Service) seal(userID uuid.UUID, secret []byte) ([]byte, error) {
	block, _ := aes.NewCipher(s.derive("totp"))
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(s.random, nonce); err != nil {
		return nil, fmt.Errorf("reading nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, secret, []byte(userID.String())), nil
}

// open decrypts a sealed TOTP secret of a user.
func (s *Service) open(userID uuid.UUID, sealed []byte) ([]byte, error) {
	block, _ := aes.NewCipher(s.derive("totp"))
	aead, _ := cipher.NewGCM(block)
	if len(sealed) < aead.NonceSize() {
		return nil, ErrInvalidCode
	}
	secret, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(userID.String()))
	if err != nil {
		return nil, fmt.Errorf("opening the secret: %w", err)
	}
	return secret, nil
}

// Enabled reports whether a user must give a second factor to sign in.
func (s *Service) Enabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	f, err := s.store.Factor(ctx, userID)
	if errors.Is(err, ErrNotEnrolled) {
		return false, nil
	}
	return f.Confirmed, err
}

// Status describes the second factor of a user.
type Status struct {
	// TOTP tells whether an authenticator app is enrolled.
	TOTP bool
	// RecoveryLeft is how many recovery codes are still unused.
	RecoveryLeft int
}

// Status returns the state of the second factor of a user. Recovery codes belong to the account, not to the app, so they are counted
// whenever they exist: a user with only passkeys has them too.
func (s *Service) Status(ctx context.Context, userID uuid.UUID) (Status, error) {
	enabled, err := s.Enabled(ctx, userID)
	if err != nil {
		return Status{}, err
	}
	left, err := s.store.RecoveryLeft(ctx, userID)
	return Status{TOTP: enabled, RecoveryLeft: left}, err
}

// BeginTOTP creates a secret for the user and returns it as base32 with the address an authenticator app scans. It is not active until
// [Service.ConfirmTOTP]; starting again replaces it.
func (s *Service) BeginTOTP(ctx context.Context, userID uuid.UUID, account string) (string, string, error) {
	secret := make([]byte, SecretLength)
	if _, err := io.ReadFull(s.random, secret); err != nil {
		return "", "", fmt.Errorf("generating the secret: %w", err)
	}
	sealed, err := s.seal(userID, secret)
	if err != nil {
		return "", "", err
	}
	if err := s.store.SaveFactor(ctx, userID, sealed); err != nil {
		return "", "", err
	}
	return EncodeSecret(secret), URI(issuer, account, secret), nil
}

// ConfirmTOTP activates the authenticator app when the code proves it was set up, and returns the recovery codes, shown once.
func (s *Service) ConfirmTOTP(ctx context.Context, userID uuid.UUID, given string) ([]string, error) {
	f, err := s.store.Factor(ctx, userID)
	if errors.Is(err, ErrNotEnrolled) || (err == nil && f.Confirmed) {
		return nil, ErrNoSetup
	}
	if err != nil {
		return nil, err
	}
	secret, err := s.open(userID, f.Sealed)
	if err != nil {
		return nil, err
	}
	step, ok := Verify(secret, given, s.clock.Now())
	if !ok {
		return nil, ErrInvalidCode
	}
	plain, hashes, err := s.newRecovery()
	if err != nil {
		return nil, err
	}
	if err := s.store.ConfirmFactor(ctx, userID, step, hashes); err != nil {
		return nil, err
	}
	return plain, nil
}

// Disable removes the authenticator app of a user. Their recovery codes stay: see [Service.ClearRecovery].
func (s *Service) Disable(ctx context.Context, userID uuid.UUID) error {
	return s.store.DeleteFactor(ctx, userID)
}

// ClearRecovery removes the recovery codes of a user, for when their last second factor was removed.
func (s *Service) ClearRecovery(ctx context.Context, userID uuid.UUID) error {
	return s.store.ReplaceRecovery(ctx, userID, nil)
}

// RegenerateRecovery replaces the recovery codes of a user with new ones, shown once. The caller checks that the user has a second factor.
func (s *Service) RegenerateRecovery(ctx context.Context, userID uuid.UUID) ([]string, error) {
	plain, hashes, err := s.newRecovery()
	if err != nil {
		return nil, err
	}
	return plain, s.store.ReplaceRecovery(ctx, userID, hashes)
}

// EnsureRecovery creates recovery codes for a user who has none, returning them, and returns nothing for one who already has some. It is
// used when a user gets a second factor other than the app, so losing it never locks them out.
func (s *Service) EnsureRecovery(ctx context.Context, userID uuid.UUID) ([]string, error) {
	left, err := s.store.RecoveryLeft(ctx, userID)
	if err != nil || left > 0 {
		return nil, err
	}
	return s.RegenerateRecovery(ctx, userID)
}

// recoveryEncoding renders recovery codes with letters and digits that are hard to mix up.
var recoveryEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newRecovery creates the recovery codes as typed by the user and the hashes to store.
func (s *Service) newRecovery() ([]string, [][]byte, error) {
	plain := make([]string, 0, RecoveryCodes)
	hashes := make([][]byte, 0, RecoveryCodes)
	for range RecoveryCodes {
		raw := make([]byte, 7)
		if _, err := io.ReadFull(s.random, raw); err != nil {
			return nil, nil, fmt.Errorf("generating a recovery code: %w", err)
		}
		text := recoveryEncoding.EncodeToString(raw)[:10]
		plain = append(plain, text[:5]+"-"+text[5:])
		hashes = append(hashes, hashRecovery(text))
	}
	return plain, hashes, nil
}

// hashRecovery returns the stored form of a recovery code, given without dashes in upper case.
func hashRecovery(text string) []byte {
	sum := sha256.Sum256([]byte("sentinel/mfa/recovery/v1|" + text))
	return sum[:]
}

// VerifyCode checks the second step of signing in: a code from the authenticator app, or a recovery code. A code works once.
func (s *Service) VerifyCode(ctx context.Context, userID uuid.UUID, given string) error {
	clean := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(given))
	if len(clean) == digits {
		f, err := s.store.Factor(ctx, userID)
		if errors.Is(err, ErrNotEnrolled) || (err == nil && !f.Confirmed) {
			return ErrNotEnrolled
		}
		if err != nil {
			return err
		}
		secret, err := s.open(userID, f.Sealed)
		if err != nil {
			return err
		}
		step, ok := Verify(secret, clean, s.clock.Now())
		if !ok || step <= f.LastStep {
			return ErrInvalidCode
		}
		advanced, err := s.store.AdvanceStep(ctx, userID, step)
		if err != nil {
			return err
		}
		if !advanced {
			return ErrInvalidCode
		}
		return nil
	}
	used, err := s.store.UseRecovery(ctx, userID, hashRecovery(clean))
	if err != nil {
		return err
	}
	if !used {
		return ErrInvalidCode
	}
	return nil
}

// Challenge returns the token that proves the password of a user was right, valid for [ChallengeTTL]. It carries no secret and has no
// server state: the user and the expiry are signed under the instance key.
func (s *Service) Challenge(userID uuid.UUID) string {
	payload := make([]byte, 24)
	copy(payload, userID[:])
	binary.BigEndian.PutUint64(payload[16:], uint64(s.clock.Now().Add(ChallengeTTL).Unix())) //nolint:gosec // times are positive.
	mac := hmac.New(sha256.New, s.derive("challenge"))
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...))
}

// OpenChallenge returns the user of a challenge, or [ErrInvalidChallenge] when it is malformed, forged or expired.
func (s *Service) OpenChallenge(token string) (uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 24+sha256.Size {
		return uuid.Nil, ErrInvalidChallenge
	}
	mac := hmac.New(sha256.New, s.derive("challenge"))
	mac.Write(raw[:24])
	if !hmac.Equal(mac.Sum(nil), raw[24:]) {
		return uuid.Nil, ErrInvalidChallenge
	}
	if s.clock.Now().Unix() >= int64(binary.BigEndian.Uint64(raw[16:24])) { //nolint:gosec // signed by this service.
		return uuid.Nil, ErrInvalidChallenge
	}
	var id uuid.UUID
	copy(id[:], raw[:16])
	return id, nil
}
