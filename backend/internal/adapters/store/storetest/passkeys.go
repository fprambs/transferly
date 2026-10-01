package storetest

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/passkey"
)

// PasskeyStore is the persistence an implementation offers to the passkey package, plus user creation.
type PasskeyStore interface {
	passkey.Store
	auth.UserStore
}

// PasskeyFactory returns a new, empty store for one test.
type PasskeyFactory func(t *testing.T) PasskeyStore

// RunPasskey executes the passkey store suite against stores built by newStore.
//
// Scenarios:
//   - passkeys are listed oldest first with their name, time, credential and no last use, and only for their owner
//   - touching a passkey stores the new credential, with its counter, and the time of use in UTC, and touching or deleting an unknown one is ErrNotFound
//   - the credential identifier is unique, and deleting removes one passkey of one user only
func RunPasskey(t *testing.T, newStore PasskeyFactory) {
	t.Helper()
	ctx := t.Context()
	store := newStore(t)
	alice, bob := newUser("alice@example.com"), newUser("bob@example.com")
	for _, u := range []auth.User{alice, bob} {
		if err := store.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	made := func(name string, tag byte, at time.Duration) passkey.Passkey {
		return passkey.Passkey{
			ID: uuid.New(), Name: name, CreatedAt: now.Add(at),
			Credential: webauthn.Credential{ID: bytes.Repeat([]byte{tag}, 32), PublicKey: bytes.Repeat([]byte{tag + 1}, 65), Authenticator: webauthn.Authenticator{SignCount: 3}},
		}
	}
	laptop, key := made("Laptop", 1, 0), made("Key", 5, time.Minute)
	for _, p := range []passkey.Passkey{key, laptop} {
		if err := store.AddPasskey(ctx, alice.ID, p); err != nil {
			t.Fatalf("AddPasskey() = %v", err)
		}
	}
	list, err := store.Passkeys(ctx, alice.ID)
	if err != nil || len(list) != 2 || list[0].ID != laptop.ID || list[1].ID != key.ID {
		t.Fatalf("Passkeys() = %+v, %v; want the laptop then the key", list, err)
	}
	if got := list[0]; got.Name != "Laptop" || !got.CreatedAt.Equal(now) || got.CreatedAt.Location() != time.UTC || !got.LastUsedAt.IsZero() || !bytes.Equal(got.Credential.ID, laptop.Credential.ID) || got.Credential.Authenticator.SignCount != 3 {
		t.Fatalf("the first passkey = %+v; want every field intact", got)
	}
	if other, _ := store.Passkeys(ctx, bob.ID); len(other) != 0 {
		t.Fatalf("Passkeys() of another user = %+v; want none", other)
	}
	if err := store.AddPasskey(ctx, bob.ID, made("Clone", 1, 0)); err == nil {
		t.Fatal("a credential identifier was stored twice")
	}

	updated := laptop.Credential
	updated.Authenticator.SignCount = 9
	when := now.Add(time.Hour)
	if err := store.TouchPasskey(ctx, alice.ID, laptop.ID, updated, when); err != nil {
		t.Fatalf("TouchPasskey() = %v", err)
	}
	list, _ = store.Passkeys(ctx, alice.ID)
	if list[0].Credential.Authenticator.SignCount != 9 || !list[0].LastUsedAt.Equal(when) || list[0].LastUsedAt.Location() != time.UTC {
		t.Fatalf("after touching = %+v; want the counter and the time stored", list[0])
	}
	if err := store.TouchPasskey(ctx, bob.ID, laptop.ID, updated, when); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("TouchPasskey() of another user = %v; want ErrNotFound", err)
	}
	if err := store.DeletePasskey(ctx, bob.ID, laptop.ID); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("DeletePasskey() of another user = %v; want ErrNotFound", err)
	}
	if err := store.DeletePasskey(ctx, alice.ID, laptop.ID); err != nil {
		t.Fatalf("DeletePasskey() = %v", err)
	}
	if err := store.DeletePasskey(ctx, alice.ID, laptop.ID); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("second DeletePasskey() = %v; want ErrNotFound", err)
	}
	if list, _ = store.Passkeys(ctx, alice.ID); len(list) != 1 || list[0].ID != key.ID {
		t.Fatalf("Passkeys() after deleting = %+v; want the key only", list)
	}
}
