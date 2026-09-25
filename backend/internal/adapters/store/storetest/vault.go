package storetest

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/vault"
)

// VaultStore is the persistence an implementation offers to the vault package, plus user creation.
type VaultStore interface {
	vault.Store
	auth.UserStore
}

// VaultFactory returns a new, empty store for one test.
type VaultFactory func(t *testing.T) VaultStore

// enrollment returns an enrollment whose fields are distinguishable from each other.
func enrollment(tag byte) vault.Enrollment {
	return vault.Enrollment{
		Params:     vault.Params{MemoryKiB: 64, Time: 2, Threads: 3},
		Salt:       bytes.Repeat([]byte{tag}, 16),
		WrappedDEK: bytes.Repeat([]byte{tag + 1}, 60), RecoveryWrappedDEK: bytes.Repeat([]byte{tag + 2}, 60),
	}
}

// RunVault executes the vault store suite against stores built by newStore.
//
// Scenarios:
//   - an enrollment round trips with every field intact
//   - a second vault for the same user is refused with ErrVaultExists
//   - reading a missing vault is ErrNoVault
//   - updating replaces every field, and updating a missing vault is ErrNoVault
//   - vaults of different users are independent
func RunVault(t *testing.T, newStore VaultFactory) {
	t.Helper()
	ctx := t.Context()

	setup := func(t *testing.T) (VaultStore, uuid.UUID, uuid.UUID) {
		t.Helper()
		store := newStore(t)
		alice, bob := newUser("alice@example.com"), newUser("bob@example.com")
		for _, u := range []auth.User{alice, bob} {
			if err := store.CreateUser(ctx, u); err != nil {
				t.Fatalf("CreateUser() = %v", err)
			}
		}
		return store, alice.ID, bob.ID
	}
	same := func(a, b vault.Enrollment) bool {
		return a.Params == b.Params && bytes.Equal(a.Salt, b.Salt) && bytes.Equal(a.WrappedDEK, b.WrappedDEK) &&
			bytes.Equal(a.RecoveryWrappedDEK, b.RecoveryWrappedDEK)
	}

	t.Run("round trip", func(t *testing.T) {
		store, alice, _ := setup(t)
		want := enrollment(1)
		if err := store.CreateVault(ctx, alice, want); err != nil {
			t.Fatalf("CreateVault() = %v", err)
		}
		if got, err := store.Vault(ctx, alice); err != nil || !same(got, want) {
			t.Fatalf("Vault() = %+v, %v; want %+v", got, err, want)
		}
	})

	t.Run("duplicate", func(t *testing.T) {
		store, alice, _ := setup(t)
		if err := store.CreateVault(ctx, alice, enrollment(1)); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateVault(ctx, alice, enrollment(2)); !errors.Is(err, vault.ErrVaultExists) {
			t.Fatalf("second CreateVault() = %v; want ErrVaultExists", err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		store, alice, _ := setup(t)
		if _, err := store.Vault(ctx, alice); !errors.Is(err, vault.ErrNoVault) {
			t.Fatalf("Vault() = %v; want ErrNoVault", err)
		}
		if err := store.UpdateVault(ctx, alice, enrollment(1)); !errors.Is(err, vault.ErrNoVault) {
			t.Fatalf("UpdateVault() = %v; want ErrNoVault", err)
		}
	})

	t.Run("update and isolation", func(t *testing.T) {
		store, alice, bob := setup(t)
		for i, id := range []uuid.UUID{alice, bob} {
			if err := store.CreateVault(ctx, id, enrollment(byte(10*(i+1)))); err != nil {
				t.Fatal(err)
			}
		}
		next := enrollment(50)
		if err := store.UpdateVault(ctx, alice, next); err != nil {
			t.Fatalf("UpdateVault() = %v", err)
		}
		if got, _ := store.Vault(ctx, alice); !same(got, next) {
			t.Errorf("alice after update = %+v; want %+v", got, next)
		}
		if got, _ := store.Vault(ctx, bob); !same(got, enrollment(20)) {
			t.Errorf("bob changed by alice's update: %+v", got)
		}
	})
}
