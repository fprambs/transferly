package vault_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/testutil"
)

// fastParams keep tests quick; production uses [vault.DefaultParams].
var fastParams = vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}

// newVault returns a vault with fast parameters and real randomness.
func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	v, err := vault.New(vault.NewKDF(2), fastParams, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// failingReader always fails, standing in for an exhausted entropy source.
type failingReader struct{}

// Read fails with a fixed error.
func (failingReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

// limitedReader serves n bytes of real randomness, then fails.
type limitedReader struct{ left int }

// Read returns random bytes until the budget is spent.
func (r *limitedReader) Read(p []byte) (int, error) {
	if r.left <= 0 {
		return 0, errors.New("no entropy")
	}
	n := min(len(p), r.left)
	r.left -= n
	return rand.Read(p[:n])
}

// TestSetupUnlockAndRecover verifies the vault lifecycle.
//
// Scenarios:
//   - setup returns an enrollment, a DEK and a recovery kit, and unlocking with the password returns the same DEK
//   - a wrong password is rejected
//   - the recovery kit opens the vault, in any case and with or without dashes
//   - a wrong or malformed recovery kit is rejected
//   - no secret appears in clear text in the enrollment
func TestSetupUnlockAndRecover(t *testing.T) {
	v := newVault(t)
	ctx := context.Background()

	enrollment, dek, kit, err := v.Setup(ctx, "correct horse")
	if err != nil {
		t.Fatalf("Setup() = %v", err)
	}
	if len(dek) != vault.KeyLength || kit == "" {
		t.Fatalf("Setup() dek=%d bytes kit=%q", len(dek), kit)
	}

	got, err := v.Unlock(ctx, "correct horse", enrollment)
	if err != nil || !bytes.Equal(got, dek) {
		t.Fatalf("Unlock() = %x, %v, want the DEK", got, err)
	}
	if _, err := v.Unlock(ctx, "wrong", enrollment); !errors.Is(err, vault.ErrWrongSecret) {
		t.Errorf("Unlock(wrong) = %v, want ErrWrongSecret", err)
	}

	for _, form := range []string{kit, strings.ToLower(kit), strings.ReplaceAll(kit, "-", ""), " " + kit + "\n"} {
		if got, err := v.Recover(form, enrollment); err != nil || !bytes.Equal(got, dek) {
			t.Errorf("Recover(%q) = %v, want the DEK", form, err)
		}
	}
	for _, bad := range []string{"", "not a kit", strings.Repeat("A", 52), "AAAA-AAAA"} {
		if _, err := v.Recover(bad, enrollment); !errors.Is(err, vault.ErrWrongSecret) {
			t.Errorf("Recover(%q) = %v, want ErrWrongSecret", bad, err)
		}
	}
	other, _, _, _ := v.Setup(ctx, "other")
	if _, err := v.Recover(kit, other); !errors.Is(err, vault.ErrWrongSecret) {
		t.Errorf("Recover with another user's kit = %v, want ErrWrongSecret", err)
	}
	if bytes.Contains(enrollment.WrappedDEK, dek) || bytes.Contains(enrollment.RecoveryWrappedDEK, dek) {
		t.Error("the DEK appears in clear text in the enrollment")
	}
}

// TestChangePasswordAndRegenerateRecovery verifies key rotation of the wrappers.
//
// Scenarios:
//   - after changing the password the new one opens the vault with the same DEK and the old one does not
//   - the salt changes and the recovery kit keeps working
//   - regenerating the recovery kit invalidates the previous kit and the new one works
func TestChangePasswordAndRegenerateRecovery(t *testing.T) {
	v := newVault(t)
	ctx := context.Background()
	enrollment, dek, kit, _ := v.Setup(ctx, "old password")

	changed, err := v.ChangePassword(ctx, dek, "new password", enrollment)
	if err != nil {
		t.Fatalf("ChangePassword() = %v", err)
	}
	if got, err := v.Unlock(ctx, "new password", changed); err != nil || !bytes.Equal(got, dek) {
		t.Errorf("Unlock(new) = %v, want the same DEK", err)
	}
	if _, err := v.Unlock(ctx, "old password", changed); !errors.Is(err, vault.ErrWrongSecret) {
		t.Errorf("Unlock(old) after change = %v, want ErrWrongSecret", err)
	}
	if bytes.Equal(changed.Salt, enrollment.Salt) {
		t.Error("the salt was reused")
	}
	if got, err := v.Recover(kit, changed); err != nil || !bytes.Equal(got, dek) {
		t.Errorf("Recover() after change = %v, want the DEK", err)
	}

	regenerated, newKit, err := v.RegenerateRecovery(dek, changed)
	if err != nil || newKit == kit {
		t.Fatalf("RegenerateRecovery() = %v, kit changed=%v", err, newKit != kit)
	}
	if _, err := v.Recover(kit, regenerated); !errors.Is(err, vault.ErrWrongSecret) {
		t.Errorf("old kit after regeneration = %v, want ErrWrongSecret", err)
	}
	if got, err := v.Recover(newKit, regenerated); err != nil || !bytes.Equal(got, dek) {
		t.Errorf("new kit = %v, want the DEK", err)
	}
}

// TestCanceledContexts verifies that operations that derive keys stop when their context is canceled,
// and that an enrollment with unusable parameters is rejected.
func TestCanceledContexts(t *testing.T) {
	v := newVault(t)
	enrollment, dek, _, _ := v.Setup(context.Background(), "pw")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, _, err := v.Setup(canceled, "pw"); !errors.Is(err, context.Canceled) {
		t.Errorf("Setup() = %v, want context.Canceled", err)
	}
	if _, err := v.Unlock(canceled, "pw", enrollment); !errors.Is(err, context.Canceled) {
		t.Errorf("Unlock() = %v, want context.Canceled", err)
	}
	if _, err := v.ChangePassword(canceled, dek, "new", enrollment); !errors.Is(err, context.Canceled) {
		t.Errorf("ChangePassword() = %v, want context.Canceled", err)
	}

	broken := enrollment
	broken.Params = vault.Params{}
	if _, err := v.Unlock(context.Background(), "pw", broken); !errors.Is(err, vault.ErrInvalidParams) {
		t.Errorf("Unlock() with invalid params = %v, want ErrInvalidParams", err)
	}
}

// TestRandomnessFailures verifies that an exhausted entropy source aborts every operation.
//
// Scenarios:
//   - Setup fails at each of its random reads: DEK, salt, nonce of each wrap, recovery key
//   - ChangePassword and RegenerateRecovery fail without entropy
func TestRandomnessFailures(t *testing.T) {
	for budget := 0; budget < 32+16+12+32+12; budget += 8 {
		v, err := vault.New(vault.NewKDF(1), fastParams, &limitedReader{left: budget})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := v.Setup(context.Background(), "p"); err == nil {
			t.Errorf("Setup() with %d bytes of entropy = nil error, want error", budget)
		}
	}

	ok := newVault(t)
	enrollment, dek, _, _ := ok.Setup(context.Background(), "p")
	broken, _ := vault.New(vault.NewKDF(1), fastParams, failingReader{})
	if _, err := broken.ChangePassword(context.Background(), dek, "q", enrollment); err == nil {
		t.Error("ChangePassword() without entropy = nil error")
	}
	partial, _ := vault.New(vault.NewKDF(1), fastParams, &limitedReader{left: vault.SaltLength})
	if _, err := partial.ChangePassword(context.Background(), dek, "q", enrollment); err == nil {
		t.Error("ChangePassword() failing at the nonce = nil error")
	}
	if _, _, err := broken.RegenerateRecovery(dek, enrollment); err == nil {
		t.Error("RegenerateRecovery() without entropy = nil error")
	}
	if _, _, err := (func() (int, int, error) {
		late, _ := vault.New(vault.NewKDF(1), fastParams, &limitedReader{left: 32})
		_, _, err := late.RegenerateRecovery(dek, enrollment)
		return 0, 0, err
	})(); err == nil {
		t.Error("RegenerateRecovery() failing at the nonce = nil error")
	}
}

// TestNewRejectsInvalidParams verifies parameter validation.
//
// Scenarios:
//   - zero time, zero threads and too little memory are rejected
//   - the default parameters are accepted
func TestNewRejectsInvalidParams(t *testing.T) {
	for _, p := range []vault.Params{{MemoryKiB: 64, Time: 0, Threads: 1}, {MemoryKiB: 64, Time: 1, Threads: 0}, {MemoryKiB: 1, Time: 1, Threads: 1}} {
		if _, err := vault.New(vault.NewKDF(1), p, rand.Reader); !errors.Is(err, vault.ErrInvalidParams) {
			t.Errorf("New(%+v) = %v, want ErrInvalidParams", p, err)
		}
	}
	if _, err := vault.New(vault.NewKDF(1), vault.DefaultParams, rand.Reader); err != nil {
		t.Errorf("New(DefaultParams) = %v", err)
	}
	if _, err := vault.NewKDF(1).Derive(context.Background(), "p", []byte("salt-salt-salt-1"), vault.Params{}); !errors.Is(err, vault.ErrInvalidParams) {
		t.Errorf("Derive with empty params = %v, want ErrInvalidParams", err)
	}
}

// TestKDF verifies the key derivation function.
//
// Scenarios:
//   - the same inputs give the same 32-byte key, different salts or passwords give different keys
//   - a derivation waiting for a busy slot gives up when its context ends while waiting
func TestKDF(t *testing.T) {
	kdf := vault.NewKDF(1)
	ctx := context.Background()
	salt := []byte("0123456789abcdef")

	a, err := kdf.Derive(ctx, "pw", salt, fastParams)
	if err != nil || len(a) != vault.KeyLength {
		t.Fatalf("Derive() = %d bytes, %v", len(a), err)
	}
	same, _ := kdf.Derive(ctx, "pw", salt, fastParams)
	otherSalt, _ := kdf.Derive(ctx, "pw", []byte("fedcba9876543210"), fastParams)
	otherPw, _ := kdf.Derive(ctx, "px", salt, fastParams)
	if !bytes.Equal(a, same) || bytes.Equal(a, otherSalt) || bytes.Equal(a, otherPw) {
		t.Error("derivation is not deterministic and input-sensitive")
	}

	slow := vault.Params{MemoryKiB: 128 * 1024, Time: 2, Threads: 1}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = kdf.Derive(ctx, "pw", salt, slow)
	}()
	time.Sleep(100 * time.Millisecond)
	waiting, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := kdf.Derive(waiting, "pw", salt, fastParams); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Derive() waiting for a busy slot = %v, want context.DeadlineExceeded", err)
	}
	<-done
}

// TestSecrets verifies sealing of site secrets.
//
// Scenarios:
//   - a secret opens with its key and reference
//   - a different site, kind, key version or key does not open it
//   - flipped bits and truncated data are detected
//   - sealing the same secret twice gives different ciphertexts, and nonces never repeat over many seals
//   - keys of the wrong size are rejected and a failing entropy source aborts sealing
func TestSecrets(t *testing.T) {
	dek := bytes.Repeat([]byte{7}, vault.KeyLength)
	ref := vault.SecretRef{Site: "site-1", Kind: "password", DEKVersion: 1}
	sealed, err := vault.SealSecret(rand.Reader, dek, ref, []byte("hunter2"))
	if err != nil {
		t.Fatal(err)
	}

	if got, err := vault.OpenSecret(dek, ref, sealed); err != nil || string(got) != "hunter2" {
		t.Fatalf("OpenSecret() = %q, %v", got, err)
	}
	for name, other := range map[string]vault.SecretRef{
		"site":    {Site: "site-2", Kind: "password", DEKVersion: 1},
		"kind":    {Site: "site-1", Kind: "passphrase", DEKVersion: 1},
		"version": {Site: "site-1", Kind: "password", DEKVersion: 2},
	} {
		if _, err := vault.OpenSecret(dek, other, sealed); !errors.Is(err, vault.ErrCorrupt) {
			t.Errorf("OpenSecret with another %s = %v, want ErrCorrupt", name, err)
		}
	}
	if _, err := vault.OpenSecret(bytes.Repeat([]byte{8}, vault.KeyLength), ref, sealed); !errors.Is(err, vault.ErrCorrupt) {
		t.Errorf("OpenSecret with another key = %v, want ErrCorrupt", err)
	}

	flipped := bytes.Clone(sealed)
	flipped[len(flipped)-1] ^= 1
	if _, err := vault.OpenSecret(dek, ref, flipped); !errors.Is(err, vault.ErrCorrupt) {
		t.Errorf("OpenSecret with a flipped bit = %v, want ErrCorrupt", err)
	}
	if _, err := vault.OpenSecret(dek, ref, sealed[:10]); !errors.Is(err, vault.ErrCorrupt) {
		t.Errorf("OpenSecret with truncated data = %v, want ErrCorrupt", err)
	}

	again, _ := vault.SealSecret(rand.Reader, dek, ref, []byte("hunter2"))
	if bytes.Equal(sealed, again) {
		t.Error("two seals of the same secret are identical")
	}
	seen := map[string]bool{}
	for range 20000 {
		s, _ := vault.SealSecret(rand.Reader, dek, ref, []byte("x"))
		nonce := string(s[:12])
		if seen[nonce] {
			t.Fatal("a nonce was reused")
		}
		seen[nonce] = true
	}

	if _, err := vault.SealSecret(rand.Reader, []byte("short"), ref, []byte("x")); err == nil {
		t.Error("SealSecret() with a short key = nil error")
	}
	if _, err := vault.OpenSecret([]byte("short"), ref, sealed); err == nil {
		t.Error("OpenSecret() with a short key = nil error")
	}
	if _, err := vault.SealSecret(failingReader{}, dek, ref, []byte("x")); err == nil {
		t.Error("SealSecret() without entropy = nil error")
	}
}

// TestSealedLayoutMatchesAESGCM verifies the documented layout, nonce then ciphertext and tag,
// by decrypting a sealed secret with the standard library directly.
func TestSealedLayoutMatchesAESGCM(t *testing.T) {
	dek := bytes.Repeat([]byte{3}, vault.KeyLength)
	ref := vault.SecretRef{Site: "s", Kind: "password", DEKVersion: 4}
	sealed, _ := vault.SealSecret(rand.Reader, dek, ref, []byte("layout"))

	block, _ := aes.NewCipher(dek)
	aead, _ := cipher.NewGCM(block)
	plaintext, err := aead.Open(nil, sealed[:12], sealed[12:], []byte("sentinel/secret/v1|s|password|4"))
	if err != nil || string(plaintext) != "layout" {
		t.Errorf("standard library open = %q, %v, want layout", plaintext, err)
	}
}

// TestRecoveryKitEncoding verifies the kit format and its round trip.
func TestRecoveryKitEncoding(t *testing.T) {
	key := make([]byte, 32)
	_, _ = io.ReadFull(rand.Reader, key)

	kit := vault.EncodeRecoveryKit(key)
	if strings.Count(kit, "-") != 12 || len(kit) != 52+12 {
		t.Errorf("kit %q has an unexpected shape", kit)
	}
	back, err := vault.ParseRecoveryKit(kit)
	if err != nil || !bytes.Equal(back, key) {
		t.Errorf("round trip = %v, %v", back, err)
	}
	if vault.EncodeRecoveryKit(nil) != "" {
		t.Error("an empty key must encode to an empty kit")
	}
}

// TestWipe verifies that Wipe zeroes a buffer.
func TestWipe(t *testing.T) {
	b := []byte{1, 2, 3}

	vault.Wipe(b)

	if !bytes.Equal(b, []byte{0, 0, 0}) {
		t.Errorf("after Wipe = %v, want zeros", b)
	}
}

// TestKeyRing verifies the in-memory key ring.
//
// Scenarios:
//   - a stored key is returned as a copy and mutating the copy does not affect the ring
//   - use extends the idle time, while a key left alone expires and is wiped
//   - Lock removes a key, Put replaces one, and Sweep removes only expired keys
func TestKeyRing(t *testing.T) {
	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	ring := vault.NewKeyRing(clock, 10*time.Minute)

	original := []byte{1, 2, 3}
	ring.Put("a", original)
	original[0] = 99
	got, ok := ring.Get("a")
	if !ok || got[0] != 1 {
		t.Fatalf("Get() = %v, %v, want a copy of the stored key", got, ok)
	}
	got[0] = 42
	if again, _ := ring.Get("a"); again[0] != 1 {
		t.Error("mutating a returned copy changed the ring")
	}

	clock.Advance(9 * time.Minute)
	if _, ok := ring.Get("a"); !ok {
		t.Error("key expired although it was used within the idle time")
	}
	clock.Advance(9 * time.Minute)
	if _, ok := ring.Get("a"); !ok {
		t.Error("use did not extend the idle time")
	}
	clock.Advance(11 * time.Minute)
	if _, ok := ring.Get("a"); ok {
		t.Error("key still available after the idle time")
	}
	if _, ok := ring.Get("missing"); ok {
		t.Error("Get() of an unknown session = true")
	}

	ring.Put("b", []byte{1})
	ring.Put("b", []byte{2})
	if k, _ := ring.Get("b"); k[0] != 2 {
		t.Error("Put() did not replace the key")
	}
	ring.Lock("b")
	if _, ok := ring.Get("b"); ok {
		t.Error("key available after Lock")
	}

	ring.Put("old", []byte{1})
	clock.Advance(11 * time.Minute)
	ring.Put("fresh", []byte{2})
	if removed := ring.Sweep(); removed != 1 {
		t.Errorf("Sweep() = %d, want 1", removed)
	}
	if _, ok := ring.Get("fresh"); !ok {
		t.Error("Sweep() removed a live key")
	}
}
