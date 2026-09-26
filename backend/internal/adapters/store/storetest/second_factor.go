package storetest

import (
	"bytes"
	"errors"
	"testing"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/mfa"
)

// FactorStore is the persistence an implementation offers to the mfa package, plus user creation.
type FactorStore interface {
	mfa.Store
	auth.UserStore
}

// FactorFactory returns a new, empty store for one test.
type FactorFactory func(t *testing.T) FactorStore

// RunFactor executes the second factor store suite against stores built by newStore.
//
// Scenarios:
//   - a user without a factor is ErrNotEnrolled, a saved secret is unconfirmed, and confirming keeps the step and stores the recovery codes
//   - saving again replaces the secret, resets the state and forgets the recovery codes
//   - a step is advanced only forward, so the same code cannot be used twice, and the first of two equal requests wins
//   - a recovery code is used once, unknown codes are not found, replacing gives a full new set, and the unused ones are counted
//   - deleting removes the secret and the recovery codes, and factors of other users are untouched
func RunFactor(t *testing.T, newStore FactorFactory) {
	t.Helper()
	ctx := t.Context()
	store := newStore(t)
	alice, bob := newUser("alice@example.com"), newUser("bob@example.com")
	for _, u := range []auth.User{alice, bob} {
		if err := store.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	code := func(tag byte) []byte { return bytes.Repeat([]byte{tag}, 32) }

	if _, err := store.Factor(ctx, alice.ID); !errors.Is(err, mfa.ErrNotEnrolled) {
		t.Fatalf("Factor() = %v; want ErrNotEnrolled", err)
	}
	if err := store.SaveFactor(ctx, alice.ID, []byte("sealed-1")); err != nil {
		t.Fatalf("SaveFactor() = %v", err)
	}
	if f, err := store.Factor(ctx, alice.ID); err != nil || string(f.Sealed) != "sealed-1" || f.Confirmed || f.LastStep != 0 {
		t.Fatalf("Factor() = %+v, %v; want an unconfirmed secret", f, err)
	}
	if err := store.ConfirmFactor(ctx, alice.ID, 100, [][]byte{code(1), code(2), code(3)}); err != nil {
		t.Fatalf("ConfirmFactor() = %v", err)
	}
	if f, _ := store.Factor(ctx, alice.ID); !f.Confirmed || f.LastStep != 100 {
		t.Fatalf("Factor() after confirming = %+v", f)
	}
	if n, _ := store.RecoveryLeft(ctx, alice.ID); n != 3 {
		t.Fatalf("RecoveryLeft() = %d; want 3", n)
	}

	if advanced, err := store.AdvanceStep(ctx, alice.ID, 100); err != nil || advanced {
		t.Fatalf("AdvanceStep() to the same step = %v, %v; want false", advanced, err)
	}
	if advanced, err := store.AdvanceStep(ctx, alice.ID, 101); err != nil || !advanced {
		t.Fatalf("AdvanceStep() forward = %v, %v; want true", advanced, err)
	}
	if advanced, _ := store.AdvanceStep(ctx, alice.ID, 99); advanced {
		t.Fatal("AdvanceStep() went back")
	}

	if used, err := store.UseRecovery(ctx, alice.ID, code(1)); err != nil || !used {
		t.Fatalf("UseRecovery() = %v, %v; want it used", used, err)
	}
	if used, _ := store.UseRecovery(ctx, alice.ID, code(1)); used {
		t.Fatal("a recovery code was used twice")
	}
	if used, _ := store.UseRecovery(ctx, alice.ID, code(9)); used {
		t.Fatal("an unknown recovery code was used")
	}
	if used, _ := store.UseRecovery(ctx, bob.ID, code(2)); used {
		t.Fatal("a recovery code of another user was used")
	}
	if n, _ := store.RecoveryLeft(ctx, alice.ID); n != 2 {
		t.Fatalf("RecoveryLeft() = %d; want 2", n)
	}
	if err := store.ReplaceRecovery(ctx, alice.ID, [][]byte{code(7), code(8)}); err != nil {
		t.Fatalf("ReplaceRecovery() = %v", err)
	}
	if used, _ := store.UseRecovery(ctx, alice.ID, code(2)); used {
		t.Fatal("an old recovery code survived the replacement")
	}
	if n, _ := store.RecoveryLeft(ctx, alice.ID); n != 2 {
		t.Fatalf("RecoveryLeft() after replacing = %d; want 2", n)
	}

	if err := store.SaveFactor(ctx, bob.ID, []byte("bob")); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFactor(ctx, alice.ID, []byte("sealed-2")); err != nil {
		t.Fatalf("second SaveFactor() = %v", err)
	}
	if f, _ := store.Factor(ctx, alice.ID); string(f.Sealed) != "sealed-2" || f.Confirmed || f.LastStep != 0 {
		t.Fatalf("Factor() after saving again = %+v; want a fresh unconfirmed secret", f)
	}
	if n, _ := store.RecoveryLeft(ctx, alice.ID); n != 0 {
		t.Fatalf("RecoveryLeft() after saving again = %d; want none", n)
	}

	if err := store.DeleteFactor(ctx, alice.ID); err != nil {
		t.Fatalf("DeleteFactor() = %v", err)
	}
	if _, err := store.Factor(ctx, alice.ID); !errors.Is(err, mfa.ErrNotEnrolled) {
		t.Fatalf("Factor() after deleting = %v; want ErrNotEnrolled", err)
	}
	if f, err := store.Factor(ctx, bob.ID); err != nil || string(f.Sealed) != "bob" {
		t.Fatalf("the factor of another user = %+v, %v; want it untouched", f, err)
	}
	if err := store.DeleteFactor(ctx, alice.ID); err != nil {
		t.Fatalf("deleting nothing = %v", err)
	}
}
