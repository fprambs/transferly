package auth

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/fprambs/transferly/internal/core/vault"
)

const (
	hashLength    = 32
	maxHashMemory = 1 << 20
	maxThreads    = 64
	argonVersion  = 19
)

var hashEncoding = base64.RawStdEncoding

// Deriver computes Argon2id keys; [vault.KDF] implements it and bounds concurrent derivations.
type Deriver interface {
	// Derive returns a key of 32 bytes for password and salt under p.
	Derive(ctx context.Context, password string, salt []byte, p vault.Params) ([]byte, error)
}

// Hasher hashes and verifies login passwords with Argon2id in the PHC string format.
type Hasher struct {
	deriver Deriver
	params  vault.Params
	random  io.Reader
}

// NewHasher returns a hasher that derives with deriver under params and draws salts from random.
func NewHasher(deriver Deriver, params vault.Params, random io.Reader) *Hasher {
	return &Hasher{deriver: deriver, params: params, random: random}
}

// Hash returns the encoded hash of password with a fresh random salt.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, vault.SaltLength)
	if _, err := io.ReadFull(h.random, salt); err != nil {
		return "", fmt.Errorf("reading salt: %w", err)
	}
	key, err := h.deriver.Derive(ctx, password, salt, h.params)
	if err != nil {
		return "", err
	}
	return encodeHash(h.params, salt, key), nil
}

// Verify reports whether password matches encoded, comparing in constant time.
// needsRehash is true when the stored parameters differ from the current ones.
func (h *Hasher) Verify(ctx context.Context, password, encoded string) (ok, needsRehash bool, err error) {
	params, salt, want, err := decodeHash(encoded)
	if err != nil {
		return false, false, err
	}
	got, err := h.deriver.Derive(ctx, password, salt, params)
	if err != nil {
		return false, false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, params != h.params, nil
}

// placeholder returns a well-formed hash under the current parameters that no password matches.
// Verifying against it costs the same as a real verification, which hides whether an account exists.
func (h *Hasher) placeholder() string {
	return encodeHash(h.params, make([]byte, vault.SaltLength), make([]byte, hashLength))
}

// encodeHash renders the PHC string $argon2id$v=19$m=...,t=...,p=...$salt$hash.
func encodeHash(p vault.Params, salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argonVersion, p.MemoryKiB, p.Time, p.Threads, hashEncoding.EncodeToString(salt), hashEncoding.EncodeToString(key))
}

// decodeHash parses a PHC string, rejecting anything that is not a bounded Argon2id hash.
func decodeHash(encoded string) (vault.Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return vault.Params{}, nil, nil, ErrMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argonVersion {
		return vault.Params{}, nil, nil, ErrMalformedHash
	}
	var p vault.Params
	var threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &threads); err != nil {
		return vault.Params{}, nil, nil, ErrMalformedHash
	}
	if p.MemoryKiB == 0 || p.MemoryKiB > maxHashMemory || p.Time == 0 || p.Time > 64 || threads == 0 || threads > maxThreads {
		return vault.Params{}, nil, nil, ErrMalformedHash
	}
	p.Threads = uint8(threads)
	salt, err := hashEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return vault.Params{}, nil, nil, ErrMalformedHash
	}
	key, err := hashEncoding.DecodeString(parts[5])
	if err != nil || len(key) != hashLength {
		return vault.Params{}, nil, nil, ErrMalformedHash
	}
	return p, salt, key, nil
}
