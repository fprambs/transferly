package vault_test

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/vault"
	"github.com/fprambs/transferly/internal/testutil"
)

// sessionStore is a [vault.SessionStore] in memory whose operations can fail.
type sessionStore struct {
	sealed   map[string][]byte
	failSave bool
	failRead bool
	saves    int
}

// SaveVaultSession keeps a sealed key, failing when told to.
func (m *sessionStore) SaveVaultSession(_ context.Context, session string, sealed []byte) error {
	m.saves++
	if m.failSave {
		return errors.New("database down")
	}
	m.sealed[session] = sealed
	return nil
}

// VaultSession returns a sealed key, failing when told to.
func (m *sessionStore) VaultSession(_ context.Context, session string) ([]byte, error) {
	if m.failRead {
		return nil, errors.New("database down")
	}
	sealed, ok := m.sealed[session]
	if !ok {
		return nil, vault.ErrNoSession
	}
	return sealed, nil
}

// DeleteVaultSession forgets a sealed key.
func (m *sessionStore) DeleteVaultSession(_ context.Context, session string) error {
	delete(m.sealed, session)
	return nil
}

// TestService_Sessions verifies the vault of a session while the session is in use (ADR-0028).
//
// Use cases:
//   - without remembering sessions, a request keeps an unlocked vault unlocked past the idle time of its key, and a vault the
//     server lost stays locked
//   - remembering sessions, the key of an unlocked session is kept once, sealed; a new server over the same store unlocks the
//     vault of the session again with its token, and a wrong token, a session with no key kept or a store that fails leave it locked
//   - a key that could not be kept is kept on a later request, and one that cannot be sealed is not kept; locking the vault forgets
//     the key kept for it
func TestService_Sessions(t *testing.T) {
	ctx := context.Background()
	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	v, err := vault.New(vault.NewKDF(2), vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enrollments := &memStore{vaults: map[uuid.UUID]vault.Enrollment{}}
	user, session, token := uuid.New(), uuid.NewString(), "token-of-the-session"
	plain := vault.NewService(v, enrollments, vault.NewKeyRing(clock, 10*time.Minute))
	if _, err := plain.Setup(ctx, user, session, master); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		clock.Advance(8 * time.Minute)
		plain.Resume(ctx, session, token)
		plain.Remember(ctx, session, token)
	}
	if _, err := plain.Key(session); err != nil {
		t.Errorf("Key() of a session in use = %v; want the vault unlocked", err)
	}
	plain.Resume(ctx, "lost", token)
	if _, err := plain.Key("lost"); !errors.Is(err, vault.ErrLocked) {
		t.Errorf("Key() of a session the server lost = %v; want ErrLocked", err)
	}

	store := &sessionStore{sealed: map[string][]byte{}, failSave: true}
	first := vault.NewService(v, enrollments, vault.NewKeyRing(clock, 10*time.Minute)).RememberSessions(store, rand.Reader)
	first.Remember(ctx, session, token)
	if err := first.Unlock(ctx, user, session, master); err != nil {
		t.Fatal(err)
	}
	first.Remember(ctx, session, token)
	store.failSave = false
	first.Remember(ctx, session, token)
	first.Remember(ctx, session, token)
	if store.saves != 2 || len(store.sealed) != 1 {
		t.Errorf("saves = %d with %d kept; want a failed save, then one kept once", store.saves, len(store.sealed))
	}
	key, _ := first.Key(session)

	restarted := func() *vault.Service {
		return vault.NewService(v, enrollments, vault.NewKeyRing(clock, 10*time.Minute)).RememberSessions(store, rand.Reader)
	}
	for _, tc := range []struct {
		name, session, token string
		failRead             bool
	}{{"wrong token", session, "stolen-guess", false}, {"no key kept", uuid.NewString(), token, false}, {"store down", session, token, true}} {
		store.failRead = tc.failRead
		s := restarted()
		s.Resume(ctx, tc.session, tc.token)
		if _, err := s.Key(tc.session); !errors.Is(err, vault.ErrLocked) {
			t.Errorf("%s: Key() = %v; want ErrLocked", tc.name, err)
		}
	}
	store.failRead = false
	second := restarted()
	second.Resume(ctx, session, token)
	got, err := second.Key(session)
	if err != nil || string(got) != string(key) {
		t.Fatalf("Key() after a restart = %v; want the key of the session", err)
	}
	saves := store.saves
	second.Remember(ctx, session, token)
	if store.saves != saves {
		t.Error("a key unlocked from the store was kept again")
	}

	second.Lock(session)
	if len(store.sealed) != 0 {
		t.Error("locking the vault did not forget the key kept for it")
	}
	third := restarted()
	third.Resume(ctx, session, token)
	if _, err := third.Key(session); !errors.Is(err, vault.ErrLocked) {
		t.Errorf("Key() after locking and a restart = %v; want ErrLocked", err)
	}
	plain.Lock(session)

	broken := vault.NewService(v, enrollments, vault.NewKeyRing(clock, 10*time.Minute)).RememberSessions(store, failingReader{})
	if err := broken.Unlock(ctx, user, session, master); err != nil {
		t.Fatal(err)
	}
	broken.Remember(ctx, session, token)
	if len(store.sealed) != 0 {
		t.Error("a key was kept although it could not be sealed")
	}
}
