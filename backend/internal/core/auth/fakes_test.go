package auth_test

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/vault"
	"github.com/fprambs/transferly/internal/testutil"
)

// errBoom is the error injected by the fakes.
var errBoom = errors.New("boom")

// fastParams keep hashing quick in tests.
var fastParams = vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}

// countingDeriver counts derivations, so tests can compare the work done for different inputs.
type countingDeriver struct {
	inner auth.Deriver
	mu    sync.Mutex
	calls int
}

// Derive delegates to the wrapped deriver and counts the call.
func (c *countingDeriver) Derive(ctx context.Context, password string, salt []byte, p vault.Params) ([]byte, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.inner.Derive(ctx, password, salt, p)
}

// count returns the number of derivations so far.
func (c *countingDeriver) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// fakeStore is an in-memory implementation of both stores with per-method failure injection.
type fakeStore struct {
	mu       sync.Mutex
	users    map[uuid.UUID]auth.User
	sessions map[uuid.UUID]auth.Session
	fail     map[string]error
}

// newFakeStore returns an empty store.
func newFakeStore() *fakeStore {
	return &fakeStore{users: map[uuid.UUID]auth.User{}, sessions: map[uuid.UUID]auth.Session{}, fail: map[string]error{}}
}

// failing makes the named method return errBoom.
func (f *fakeStore) failing(method string) *fakeStore {
	f.fail[method] = errBoom
	return f
}

// CreateFirstUser stores u when the store is empty.
func (f *fakeStore) CreateFirstUser(_ context.Context, u auth.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["CreateFirstUser"]; err != nil {
		return err
	}
	if len(f.users) > 0 {
		return auth.ErrSetupComplete
	}
	f.users[u.ID] = u
	return nil
}

// CreateUser stores u unless its email exists.
func (f *fakeStore) CreateUser(_ context.Context, u auth.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["CreateUser"]; err != nil {
		return err
	}
	for _, existing := range f.users {
		if existing.Email == u.Email {
			return auth.ErrEmailTaken
		}
	}
	f.users[u.ID] = u
	return nil
}

// UserByEmail finds a user by email.
func (f *fakeStore) UserByEmail(_ context.Context, email string) (auth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["UserByEmail"]; err != nil {
		return auth.User{}, err
	}
	for _, u := range f.users {
		if u.Email == email {
			return u, nil
		}
	}
	return auth.User{}, auth.ErrNotFound
}

// UserByID finds a user by id.
func (f *fakeStore) UserByID(_ context.Context, id uuid.UUID) (auth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["UserByID"]; err != nil {
		return auth.User{}, err
	}
	u, ok := f.users[id]
	if !ok {
		return auth.User{}, auth.ErrNotFound
	}
	return u, nil
}

// CountUsers counts users.
func (f *fakeStore) CountUsers(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.users), f.fail["CountUsers"]
}

// UpdatePasswordHash replaces a stored hash.
func (f *fakeStore) UpdatePasswordHash(_ context.Context, id uuid.UUID, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["UpdatePasswordHash"]; err != nil {
		return err
	}
	u := f.users[id]
	u.PasswordHash = hash
	f.users[id] = u
	return nil
}

// UpdatePreferences changes the preferences.
func (f *fakeStore) UpdatePreferences(_ context.Context, id uuid.UUID, p auth.Preferences) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := f.users[id]
	u.Locale, u.Timezone, u.Theme, u.FileDoubleClick = p.Locale, p.Timezone, p.Theme, p.FileDoubleClick
	f.users[id] = u
	return f.fail["UpdatePreferences"]
}

// CreateSession stores a session.
func (f *fakeStore) CreateSession(_ context.Context, s auth.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["CreateSession"]; err != nil {
		return err
	}
	f.sessions[s.ID] = s
	return nil
}

// SessionByTokenHash finds a session by token hash.
func (f *fakeStore) SessionByTokenHash(_ context.Context, hash []byte) (auth.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["SessionByTokenHash"]; err != nil {
		return auth.Session{}, err
	}
	for _, s := range f.sessions {
		if string(s.TokenHash) == string(hash) {
			return s, nil
		}
	}
	return auth.Session{}, auth.ErrNotFound
}

// TouchSession records a use.
func (f *fakeStore) TouchSession(_ context.Context, id uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["TouchSession"]; err != nil {
		return err
	}
	s := f.sessions[id]
	s.LastSeenAt = at
	f.sessions[id] = s
	return nil
}

// DeleteSession removes a session.
func (f *fakeStore) DeleteSession(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["DeleteSession"]; err != nil {
		return err
	}
	delete(f.sessions, id)
	return nil
}

// DeleteUserSessions removes every session of a user.
func (f *fakeStore) DeleteUserSessions(_ context.Context, userID uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["DeleteUserSessions"]; err != nil {
		return 0, err
	}
	n := 0
	for id, s := range f.sessions {
		if s.UserID == userID {
			delete(f.sessions, id)
			n++
		}
	}
	return n, nil
}

// ListUserSessions returns the sessions of a kind of a user, most recently used first.
func (f *fakeStore) ListUserSessions(_ context.Context, userID uuid.UUID, kind auth.SessionKind) ([]auth.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["ListUserSessions"]; err != nil {
		return nil, err
	}
	var list []auth.Session
	for _, s := range f.sessions {
		if s.UserID == userID && s.Kind == kind {
			list = append(list, s)
		}
	}
	slices.SortFunc(list, func(a, b auth.Session) int { return b.LastSeenAt.Compare(a.LastSeenAt) })
	return list, nil
}

// DeleteUserSession removes one session of a user.
func (f *fakeStore) DeleteUserSession(_ context.Context, userID, id uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["DeleteUserSession"]; err != nil {
		return false, err
	}
	s, ok := f.sessions[id]
	if !ok || s.UserID != userID {
		return false, nil
	}
	delete(f.sessions, id)
	return true, nil
}

// DeleteExpiredSessions removes expired sessions and sessions idle before the cutoff of their kind.
func (f *fakeStore) DeleteExpiredSessions(_ context.Context, now time.Time, idle auth.IdleCutoffs) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["DeleteExpiredSessions"]; err != nil {
		return 0, err
	}
	n := 0
	for id, s := range f.sessions {
		idleBefore := idle.Browser
		if s.Kind == auth.SessionCLI {
			idleBefore = idle.CLI
		}
		if !now.Before(s.ExpiresAt) || s.LastSeenAt.Before(idleBefore) {
			delete(f.sessions, id)
			n++
		}
	}
	return n, nil
}

// harness bundles a service with the parts tests need to inspect.
type harness struct {
	svc      *auth.Service
	store    *fakeStore
	clock    *testutil.FakeClock
	deriver  *countingDeriver
	hasher   *auth.Hasher
	instance []byte
}

// newHarness builds a service over a fresh fake store with fast hashing.
func newHarness(t *testing.T, store *fakeStore) *harness {
	t.Helper()
	if store == nil {
		store = newFakeStore()
	}
	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	deriver := &countingDeriver{inner: vault.NewKDF(2)}
	hasher := auth.NewHasher(deriver, fastParams, rand.Reader)
	config := auth.Config{IdleTimeout: 30 * time.Minute, AbsoluteTimeout: 8 * time.Hour}
	return &harness{
		svc: auth.NewService(store, store, hasher, clock, rand.Reader, config), store: store, clock: clock,
		deriver: deriver, hasher: hasher, instance: []byte("0123456789abcdef0123456789abcdef"),
	}
}
