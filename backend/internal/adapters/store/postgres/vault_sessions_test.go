package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/vault"
)

// TestVaultSessionStore verifies the vault keys kept for sessions (ADR-0028).
//
// Use cases:
//   - a session has no key at first; a kept key is read back, and keeping another replaces it
//   - deleting the key, or the session, forgets it; deleting what is not there succeeds
//   - a session that is not a UUID has no key and deletes nothing; keeping a key for it fails
//   - every operation returns the database error on a closed pool
func TestVaultSessionStore(t *testing.T) {
	ctx := context.Background()
	store := openMigrated(t)
	at := time.Now().UTC()
	user := auth.User{ID: uuid.New(), Email: "vault-sessions@example.com", Role: auth.RoleUser, CreatedAt: at}
	if err := store.CreateFirstUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	newSession := func() string {
		t.Helper()
		session := auth.Session{ID: uuid.New(), UserID: user.ID, TokenHash: []byte(uuid.NewString()), CreatedAt: at, LastSeenAt: at, ExpiresAt: at.Add(time.Hour)}
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}
		return session.ID.String()
	}
	session := newSession()
	if _, err := store.VaultSession(ctx, session); !errors.Is(err, vault.ErrNoSession) {
		t.Fatalf("VaultSession() at first = %v; want ErrNoSession", err)
	}
	for _, sealed := range []string{"first", "second"} {
		if err := store.SaveVaultSession(ctx, session, []byte(sealed)); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := store.VaultSession(ctx, session); err != nil || string(got) != "second" {
		t.Errorf("VaultSession() = %q, %v; want the last key kept", got, err)
	}
	if err := store.DeleteVaultSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VaultSession(ctx, session); !errors.Is(err, vault.ErrNoSession) {
		t.Errorf("VaultSession() after deleting = %v", err)
	}
	if err := store.DeleteVaultSession(ctx, session); err != nil {
		t.Errorf("DeleteVaultSession() of nothing = %v", err)
	}

	ended := newSession()
	if err := store.SaveVaultSession(ctx, ended, []byte("kept")); err != nil {
		t.Fatal(err)
	}
	id, _ := uuid.Parse(ended)
	if err := store.DeleteSession(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VaultSession(ctx, ended); !errors.Is(err, vault.ErrNoSession) {
		t.Errorf("VaultSession() of an ended session = %v; want ErrNoSession", err)
	}

	if _, err := store.VaultSession(ctx, "not-a-uuid"); !errors.Is(err, vault.ErrNoSession) {
		t.Errorf("VaultSession(not a UUID) = %v", err)
	}
	if err := store.DeleteVaultSession(ctx, "not-a-uuid"); err != nil {
		t.Errorf("DeleteVaultSession(not a UUID) = %v", err)
	}
	if err := store.SaveVaultSession(ctx, "not-a-uuid", []byte("x")); err == nil {
		t.Error("SaveVaultSession(not a UUID) succeeded")
	}

	store.Close()
	if _, err := store.VaultSession(ctx, session); err == nil || errors.Is(err, vault.ErrNoSession) {
		t.Errorf("VaultSession() on a closed pool = %v; want the database error", err)
	}
	if err := store.SaveVaultSession(ctx, session, []byte("x")); err == nil {
		t.Error("SaveVaultSession() hid the database error")
	}
	if err := store.DeleteVaultSession(ctx, session); err == nil {
		t.Error("DeleteVaultSession() hid the database error")
	}
}
