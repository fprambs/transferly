package auth_test

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/vault"
)

// failingReader always fails.
type failingReader struct{}

// Read fails with errBoom.
func (failingReader) Read([]byte) (int, error) { return 0, errBoom }

// TestHasher_HashAndVerify verifies hashing and verification.
//
// Scenarios:
//   - a password verifies against its own hash and not against another password
//   - hashing twice gives different encoded strings because of the random salt
//   - the encoded form is a PHC Argon2id string that carries its parameters
//   - a hash made under different parameters verifies but is flagged for rehashing
func TestHasher_HashAndVerify(t *testing.T) {
	ctx := context.Background()
	h := auth.NewHasher(vault.NewKDF(1), fastParams, rand.Reader)

	encoded, err := h.Hash(ctx, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Errorf("encoded = %q, want a PHC Argon2id string", encoded)
	}
	if ok, rehash, err := h.Verify(ctx, "correct horse battery", encoded); !ok || rehash || err != nil {
		t.Errorf("Verify(correct) = %v, %v, %v, want a match without rehash", ok, rehash, err)
	}
	if ok, _, err := h.Verify(ctx, "wrong password", encoded); ok || err != nil {
		t.Errorf("Verify(wrong) = %v, %v, want no match and no error", ok, err)
	}
	again, _ := h.Hash(ctx, "correct horse battery")
	if again == encoded {
		t.Error("two hashes of the same password are identical")
	}

	stronger := auth.NewHasher(vault.NewKDF(1), vault.Params{MemoryKiB: 128, Time: 1, Threads: 1}, rand.Reader)
	if ok, rehash, err := stronger.Verify(ctx, "correct horse battery", encoded); !ok || !rehash || err != nil {
		t.Errorf("Verify() under stronger parameters = %v, %v, %v, want a match flagged for rehash", ok, rehash, err)
	}
}

// TestHasher_Failures verifies the error paths.
//
// Scenarios:
//   - an exhausted entropy source aborts hashing
//   - a canceled context aborts hashing and verification
//   - malformed encodings are rejected: wrong shape, algorithm, version, parameters, out-of-bounds cost, salt and hash
func TestHasher_Failures(t *testing.T) {
	ctx := context.Background()
	h := auth.NewHasher(vault.NewKDF(1), fastParams, rand.Reader)
	good, _ := h.Hash(ctx, "some long password")

	if _, err := auth.NewHasher(vault.NewKDF(1), fastParams, failingReader{}).Hash(ctx, "x"); !errors.Is(err, errBoom) {
		t.Errorf("Hash() without entropy = %v, want the entropy error", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := h.Hash(canceled, "x"); !errors.Is(err, context.Canceled) {
		t.Errorf("Hash() with a canceled context = %v, want context.Canceled", err)
	}
	if _, _, err := h.Verify(canceled, "x", good); !errors.Is(err, context.Canceled) {
		t.Errorf("Verify() with a canceled context = %v, want context.Canceled", err)
	}

	salt, key := "c29tZXNhbHRzb21lc2FsdA", strings.Repeat("A", 43)
	tests := map[string]string{
		"empty":                "",
		"not argon2":           "$bcrypt$v=19$m=64,t=1,p=1$" + salt + "$" + key,
		"too few parts":        "$argon2id$v=19$m=64,t=1,p=1$" + salt,
		"no leading dollar":    "argon2id$v=19$m=64,t=1,p=1$" + salt + "$" + key + "$",
		"wrong version":        "$argon2id$v=18$m=64,t=1,p=1$" + salt + "$" + key,
		"version not a number": "$argon2id$v=x$m=64,t=1,p=1$" + salt + "$" + key,
		"parameters garbled":   "$argon2id$v=19$m=x,t=1,p=1$" + salt + "$" + key,
		"zero memory":          "$argon2id$v=19$m=0,t=1,p=1$" + salt + "$" + key,
		"memory too large":     "$argon2id$v=19$m=99999999,t=1,p=1$" + salt + "$" + key,
		"zero time":            "$argon2id$v=19$m=64,t=0,p=1$" + salt + "$" + key,
		"time too large":       "$argon2id$v=19$m=64,t=999,p=1$" + salt + "$" + key,
		"zero threads":         "$argon2id$v=19$m=64,t=1,p=0$" + salt + "$" + key,
		"threads too large":    "$argon2id$v=19$m=64,t=1,p=255$" + salt + "$" + key,
		"salt not base64":      "$argon2id$v=19$m=64,t=1,p=1$!!!$" + key,
		"salt empty":           "$argon2id$v=19$m=64,t=1,p=1$$" + key,
		"hash not base64":      "$argon2id$v=19$m=64,t=1,p=1$" + salt + "$!!!",
		"hash wrong length":    "$argon2id$v=19$m=64,t=1,p=1$" + salt + "$AAAA",
	}
	for name, encoded := range tests {
		if _, _, err := h.Verify(ctx, "x", encoded); !errors.Is(err, auth.ErrMalformedHash) {
			t.Errorf("Verify(%s) = %v, want ErrMalformedHash", name, err)
		}
	}
}
