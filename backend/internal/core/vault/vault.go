// Package vault implements the cryptography of the credential vault.
//
// A master password derives a key-encryption key with Argon2id. That key wraps a random
// data-encryption key (DEK), which seals every stored secret with AES-256-GCM. A recovery kit
// wraps the same DEK, so a lost master password does not lose the secrets.
package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	// KeyLength is the size in bytes of every key handled by the vault.
	KeyLength = 32
	// SaltLength is the size in bytes of a key derivation salt.
	SaltLength  = 16
	nonceLength = 12
	recoveryLen = 32
)

// Errors returned by the vault.
var (
	// ErrWrongSecret means the master password or recovery kit does not open the vault.
	ErrWrongSecret = errors.New("wrong master password or recovery kit")
	// ErrCorrupt means sealed data failed authentication or is malformed.
	ErrCorrupt = errors.New("sealed data is corrupt or was tampered with")
	// ErrInvalidParams means the key derivation parameters are unusable.
	ErrInvalidParams = errors.New("invalid key derivation parameters")
)

// Params are the Argon2id cost parameters.
type Params struct {
	// MemoryKiB is the memory cost in KiB.
	MemoryKiB uint32
	// Time is the number of passes over memory.
	Time uint32
	// Threads is the degree of parallelism.
	Threads uint8
}

// DefaultParams is the RFC 9106 low-memory recommendation: 3 passes, 64 MiB and 4 lanes.
var DefaultParams = Params{MemoryKiB: 64 * 1024, Time: 3, Threads: 4}

// validate rejects parameters that would make the derivation trivial or impossible.
func (p Params) validate() error {
	if p.MemoryKiB < 8*uint32(max(p.Threads, 1)) || p.Time == 0 || p.Threads == 0 {
		return ErrInvalidParams
	}
	return nil
}

// Enrollment is the persisted state of a user's vault. It holds no secret in clear text.
type Enrollment struct {
	// Params are the key derivation parameters used for the master password.
	Params Params
	// Salt is the key derivation salt of the master password.
	Salt []byte
	// WrappedDEK is the DEK sealed with the key derived from the master password.
	WrappedDEK []byte
	// RecoveryWrappedDEK is the DEK sealed with the recovery key.
	RecoveryWrappedDEK []byte
}

// Vault performs the vault operations.
type Vault struct {
	kdf    *KDF
	params Params
	random io.Reader
}

// New returns a vault that derives keys with kdf using params and draws randomness from random.
func New(kdf *KDF, params Params, random io.Reader) (*Vault, error) {
	if err := params.validate(); err != nil {
		return nil, err
	}
	return &Vault{kdf: kdf, params: params, random: random}, nil
}

// NewDefault returns a vault that uses [DefaultParams], which are always valid.
func NewDefault(kdf *KDF, random io.Reader) *Vault {
	return &Vault{kdf: kdf, params: DefaultParams, random: random}
}

// KDF derives keys with Argon2id while bounding how many derivations run at once,
// because each one allocates its full memory cost.
type KDF struct {
	slots chan struct{}
}

// NewKDF returns a derivation function that allows at most maxConcurrent simultaneous derivations.
func NewKDF(maxConcurrent int) *KDF {
	return &KDF{slots: make(chan struct{}, max(maxConcurrent, 1))}
}

// Derive returns a key of [KeyLength] bytes from password and salt.
// It waits for a free slot and gives up when ctx ends.
func (k *KDF) Derive(ctx context.Context, password string, salt []byte, p Params) ([]byte, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case k.slots <- struct{}{}:
		defer func() { <-k.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, KeyLength), nil
}

// Setup creates the vault of a new user.
//
// It returns the enrollment to persist, the DEK for the current session and the recovery kit,
// which must be shown to the user once and never stored.
func (v *Vault) Setup(ctx context.Context, password string) (Enrollment, []byte, string, error) {
	dek, err := v.randomBytes(KeyLength)
	if err != nil {
		return Enrollment{}, nil, "", err
	}
	salt, err := v.randomBytes(SaltLength)
	if err != nil {
		return Enrollment{}, nil, "", err
	}
	enrollment := Enrollment{Params: v.params, Salt: salt}
	if err := v.wrapWithPassword(ctx, &enrollment, password, dek); err != nil {
		return Enrollment{}, nil, "", err
	}
	kit, err := v.wrapWithRecovery(&enrollment, dek)
	if err != nil {
		return Enrollment{}, nil, "", err
	}
	return enrollment, dek, kit, nil
}

// Unlock returns the DEK opened with the master password, or [ErrWrongSecret].
func (v *Vault) Unlock(ctx context.Context, password string, e Enrollment) ([]byte, error) {
	kek, err := v.kdf.Derive(ctx, password, e.Salt, e.Params)
	if err != nil {
		return nil, err
	}
	defer Wipe(kek)
	dek, err := open(kek, e.WrappedDEK, wrapAAD("password"))
	if err != nil {
		return nil, ErrWrongSecret
	}
	return dek, nil
}

// Recover returns the DEK opened with the recovery kit, or [ErrWrongSecret].
func (v *Vault) Recover(kit string, e Enrollment) ([]byte, error) {
	key, err := ParseRecoveryKit(kit)
	if err != nil {
		return nil, ErrWrongSecret
	}
	defer Wipe(key)
	dek, err := open(key, e.RecoveryWrappedDEK, wrapAAD("recovery"))
	if err != nil {
		return nil, ErrWrongSecret
	}
	return dek, nil
}

// ChangePassword re-wraps the DEK under a new master password with a fresh salt.
// The DEK is the one returned by [Vault.Unlock] or [Vault.Recover]; the recovery wrap is untouched.
func (v *Vault) ChangePassword(ctx context.Context, dek []byte, newPassword string, e Enrollment) (Enrollment, error) {
	salt, err := v.randomBytes(SaltLength)
	if err != nil {
		return Enrollment{}, err
	}
	next := Enrollment{Params: v.params, Salt: salt, RecoveryWrappedDEK: e.RecoveryWrappedDEK}
	if err := v.wrapWithPassword(ctx, &next, newPassword, dek); err != nil {
		return Enrollment{}, err
	}
	return next, nil
}

// RegenerateRecovery replaces the recovery kit and returns the new one.
func (v *Vault) RegenerateRecovery(dek []byte, e Enrollment) (Enrollment, string, error) {
	kit, err := v.wrapWithRecovery(&e, dek)
	if err != nil {
		return Enrollment{}, "", err
	}
	return e, kit, nil
}

// wrapWithPassword seals the DEK under the key derived from password and stores it in e.
func (v *Vault) wrapWithPassword(ctx context.Context, e *Enrollment, password string, dek []byte) error {
	kek, err := v.kdf.Derive(ctx, password, e.Salt, e.Params)
	if err != nil {
		return err
	}
	defer Wipe(kek)
	e.WrappedDEK, err = seal(v.random, kek, dek, wrapAAD("password"))
	return err
}

// wrapWithRecovery seals the DEK under a new random recovery key, stores it in e and returns the kit.
func (v *Vault) wrapWithRecovery(e *Enrollment, dek []byte) (string, error) {
	key, err := v.randomBytes(recoveryLen)
	if err != nil {
		return "", err
	}
	defer Wipe(key)
	e.RecoveryWrappedDEK, err = seal(v.random, key, dek, wrapAAD("recovery"))
	if err != nil {
		return "", err
	}
	return EncodeRecoveryKit(key), nil
}

// randomBytes returns n bytes from the vault's random source.
func (v *Vault) randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(v.random, b); err != nil {
		return nil, fmt.Errorf("reading random bytes: %w", err)
	}
	return b, nil
}

// wrapAAD is the associated data that binds a wrapped DEK to its purpose.
func wrapAAD(kind string) []byte {
	return []byte("sentinel/dek-wrap/v1|" + kind)
}

var recoveryEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// EncodeRecoveryKit renders a recovery key as groups of four base32 characters separated by dashes.
func EncodeRecoveryKit(key []byte) string {
	raw := recoveryEncoding.EncodeToString(key)
	groups := make([]string, 0, len(raw)/4+1)
	for len(raw) > 0 {
		n := min(4, len(raw))
		groups = append(groups, raw[:n])
		raw = raw[n:]
	}
	return strings.Join(groups, "-")
}

// ParseRecoveryKit decodes a recovery kit, ignoring case, dashes and spaces.
func ParseRecoveryKit(kit string) ([]byte, error) {
	clean := strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\n", "").Replace(kit))
	key, err := recoveryEncoding.DecodeString(clean)
	if err != nil || len(key) != recoveryLen {
		return nil, ErrWrongSecret
	}
	return key, nil
}

// Wipe overwrites b with zeros. It is a best-effort defense against secrets lingering in memory.
func Wipe(b []byte) {
	clear(b)
}

// seal encrypts plaintext with AES-256-GCM under key, prefixing a random nonce to the result.
func seal(random io.Reader, key, plaintext, aad []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLength)
	if _, err := io.ReadFull(random, nonce); err != nil {
		return nil, fmt.Errorf("reading nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, plaintext, aad), nil
}

// open reverses [seal], returning [ErrCorrupt] when authentication fails.
func open(key, sealed, aad []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < nonceLength+aead.Overhead() {
		return nil, ErrCorrupt
	}
	plaintext, err := aead.Open(nil, sealed[:nonceLength], sealed[nonceLength:], aad)
	if err != nil {
		return nil, ErrCorrupt
	}
	return plaintext, nil
}

// newAEAD builds an AES-256-GCM cipher, rejecting keys of the wrong size.
func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != KeyLength {
		return nil, fmt.Errorf("key must be %d bytes, got %d", KeyLength, len(key))
	}
	block, _ := aes.NewCipher(key)
	return cipher.NewGCM(block)
}
