// Package storetest is the conformance suite every store implementation must pass.
package storetest

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
)

// AuthStore is the persistence an implementation offers to the auth package.
type AuthStore interface {
	auth.UserStore
	auth.SessionStore
}

// AuthFactory returns a new, empty store for one test.
type AuthFactory func(t *testing.T) AuthStore

// now is a fixed instant with microsecond precision, the resolution of Postgres timestamps.
var now = time.Date(2026, 3, 8, 12, 30, 15, 123456000, time.UTC)

// newUser returns a valid user with the given email.
func newUser(email string) auth.User {
	return auth.User{
		ID: uuid.New(), Email: email, PasswordHash: "$argon2id$hash", Role: auth.RoleUser,
		Locale: "es", Timezone: "America/Santiago", Theme: "monokai", FileDoubleClick: auth.FileDoubleClickEdit, CreatedAt: now,
	}
}

// newSession returns a session of a user with a distinct token hash.
func newSession(userID uuid.UUID, tokenHash string) auth.Session {
	return auth.Session{
		ID: uuid.New(), UserID: userID, TokenHash: []byte(tokenHash), UserAgent: "agent", IP: "10.0.0.7",
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(8 * time.Hour),
	}
}

// RunAuth executes the authentication store suite against stores built by newStore.
//
// Scenarios:
//   - users: round trip of every field with UTC times, lookup by email and id, missing users, duplicate emails,
//     disabled status, password and preference updates, counting
//   - first user: created once, refused afterwards, and exactly one of many concurrent attempts wins
//   - sessions: round trip, lookup by token hash, missing sessions, touch, delete, delete by user, expiry purge
//   - referential integrity: a session cannot exist without its user, and deleting nothing is not an error
func RunAuth(t *testing.T, newStore AuthFactory) {
	t.Helper()
	tests := []struct {
		name string
		run  func(t *testing.T, s AuthStore)
	}{
		{"users round trip and lookups", testUsers},
		{"duplicate emails are refused", testDuplicateEmail},
		{"disabled status persists", testDisabled},
		{"updates change only their fields", testUpdates},
		{"the first user is created once", testFirstUser},
		{"concurrent first users produce exactly one winner", testFirstUserRace},
		{"sessions round trip, touch and delete", testSessions},
		{"deleting by user and by expiry", testSessionCleanup},
		{"sessions need an existing user and unique tokens", testSessionIntegrity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.run(t, newStore(t)) })
	}
}

// testUsers checks the round trip of a user and the lookups.
func testUsers(t *testing.T, s AuthStore) {
	ctx := t.Context()
	u := newUser("a@example.com")
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser() = %v", err)
	}

	byEmail, err := s.UserByEmail(ctx, "a@example.com")
	if err != nil || byEmail != u {
		t.Errorf("UserByEmail() = %+v, %v, want %+v", byEmail, err, u)
	}
	byID, err := s.UserByID(ctx, u.ID)
	if err != nil || byID != u {
		t.Errorf("UserByID() = %+v, %v, want %+v", byID, err, u)
	}
	if byID.CreatedAt.Location() != time.UTC {
		t.Error("CreatedAt is not in UTC")
	}
	if _, err := s.UserByEmail(ctx, "missing@example.com"); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("UserByEmail(missing) = %v, want ErrNotFound", err)
	}
	if _, err := s.UserByID(ctx, uuid.New()); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("UserByID(missing) = %v, want ErrNotFound", err)
	}
	if n, err := s.CountUsers(ctx); err != nil || n != 1 {
		t.Errorf("CountUsers() = %d, %v, want 1", n, err)
	}
}

// testDuplicateEmail checks that emails are unique.
func testDuplicateEmail(t *testing.T, s AuthStore) {
	ctx := t.Context()
	if err := s.CreateUser(ctx, newUser("dup@example.com")); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser(ctx, newUser("dup@example.com")); !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("duplicate CreateUser() = %v, want ErrEmailTaken", err)
	}
}

// testDisabled checks that the disabled flag round trips.
func testDisabled(t *testing.T, s AuthStore) {
	ctx := t.Context()
	u := newUser("off@example.com")
	u.Disabled = true
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.UserByID(ctx, u.ID); !got.Disabled {
		t.Error("the disabled flag was lost")
	}
}

// testUpdates checks the update operations.
func testUpdates(t *testing.T, s AuthStore) {
	ctx := t.Context()
	u := newUser("up@example.com")
	other := newUser("other@example.com")
	_ = s.CreateUser(ctx, u)
	_ = s.CreateUser(ctx, other)

	if err := s.UpdatePasswordHash(ctx, u.ID, "$argon2id$new"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePreferences(ctx, u.ID, auth.Preferences{Locale: "en", Timezone: "UTC", Theme: "nord", FileDoubleClick: auth.FileDoubleClickPreview}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.UserByID(ctx, u.ID)
	if got.PasswordHash != "$argon2id$new" || got.Locale != "en" || got.Timezone != "UTC" || got.Theme != "nord" || got.FileDoubleClick != auth.FileDoubleClickPreview || got.Email != u.Email {
		t.Errorf("after updates = %+v", got)
	}
	if untouched, _ := s.UserByID(ctx, other.ID); untouched != other {
		t.Error("an update changed another user")
	}
}

// testFirstUser checks CreateFirstUser.
func testFirstUser(t *testing.T, s AuthStore) {
	ctx := t.Context()
	first := newUser("first@example.com")
	first.Role = auth.RoleAdmin

	if err := s.CreateFirstUser(ctx, first); err != nil {
		t.Fatalf("CreateFirstUser() = %v", err)
	}
	if err := s.CreateFirstUser(ctx, newUser("second@example.com")); !errors.Is(err, auth.ErrSetupComplete) {
		t.Errorf("second CreateFirstUser() = %v, want ErrSetupComplete", err)
	}
	if got, _ := s.UserByID(ctx, first.ID); got.Role != auth.RoleAdmin {
		t.Error("the first user is not an administrator")
	}
}

// testFirstUserRace checks that simultaneous first-user attempts create exactly one account.
func testFirstUserRace(t *testing.T, s AuthStore) {
	ctx := t.Context()
	const attempts = 12
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u := newUser("racer" + string(rune('a'+i)) + "@example.com")
			results <- s.CreateFirstUser(ctx, u)
		}()
	}
	wg.Wait()
	close(results)

	winners, refused := 0, 0
	for err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, auth.ErrSetupComplete):
			refused++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if winners != 1 || refused != attempts-1 {
		t.Errorf("winners=%d refused=%d, want 1 and %d", winners, refused, attempts-1)
	}
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Errorf("CountUsers() = %d, want exactly 1", n)
	}
}

// testSessions checks the session round trip, touch and delete.
func testSessions(t *testing.T, s AuthStore) {
	ctx := t.Context()
	u := newUser("s@example.com")
	_ = s.CreateUser(ctx, u)
	session := newSession(u.ID, "hash-1")

	if err := s.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() = %v", err)
	}
	got, err := s.SessionByTokenHash(ctx, []byte("hash-1"))
	if err != nil || got.ID != session.ID || got.UserID != u.ID || !bytes.Equal(got.TokenHash, session.TokenHash) ||
		got.UserAgent != "agent" || got.IP != "10.0.0.7" || !got.CreatedAt.Equal(now) || !got.ExpiresAt.Equal(session.ExpiresAt) {
		t.Errorf("SessionByTokenHash() = %+v, %v, want %+v", got, err, session)
	}
	if got.LastSeenAt.Location() != time.UTC {
		t.Error("session times are not in UTC")
	}
	if _, err := s.SessionByTokenHash(ctx, []byte("unknown")); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("SessionByTokenHash(unknown) = %v, want ErrNotFound", err)
	}

	later := now.Add(10 * time.Minute)
	if err := s.TouchSession(ctx, session.ID, later); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SessionByTokenHash(ctx, []byte("hash-1")); !got.LastSeenAt.Equal(later) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, later)
	}
	if err := s.DeleteSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, session.ID); err != nil {
		t.Errorf("deleting a missing session = %v, want nil", err)
	}
	if _, err := s.SessionByTokenHash(ctx, []byte("hash-1")); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("session after delete = %v, want ErrNotFound", err)
	}
}

// testSessionCleanup checks deletion by user and by expiry.
func testSessionCleanup(t *testing.T, s AuthStore) {
	ctx := t.Context()
	a, b := newUser("a@example.com"), newUser("b@example.com")
	_ = s.CreateUser(ctx, a)
	_ = s.CreateUser(ctx, b)
	for _, hash := range []string{"a1", "a2"} {
		_ = s.CreateSession(ctx, newSession(a.ID, hash))
	}
	_ = s.CreateSession(ctx, newSession(b.ID, "b1"))

	if n, err := s.DeleteUserSessions(ctx, a.ID); err != nil || n != 2 {
		t.Errorf("DeleteUserSessions() = %d, %v, want 2", n, err)
	}
	if _, err := s.SessionByTokenHash(ctx, []byte("b1")); err != nil {
		t.Errorf("another user's session was deleted: %v", err)
	}
	if n, err := s.DeleteUserSessions(ctx, a.ID); err != nil || n != 0 {
		t.Errorf("DeleteUserSessions() again = %d, %v, want 0", n, err)
	}

	expired := newSession(b.ID, "expired")
	expired.ExpiresAt = now.Add(-time.Minute)
	idle := newSession(b.ID, "idle")
	idle.LastSeenAt = now.Add(-2 * time.Hour)
	_ = s.CreateSession(ctx, expired)
	_ = s.CreateSession(ctx, idle)
	if n, err := s.DeleteExpiredSessions(ctx, now, now.Add(-time.Hour)); err != nil || n != 2 {
		t.Errorf("DeleteExpiredSessions() = %d, %v, want the expired and the idle session", n, err)
	}
	if _, err := s.SessionByTokenHash(ctx, []byte("b1")); err != nil {
		t.Errorf("a live session was purged: %v", err)
	}
}

// testSessionIntegrity checks the constraints of the session table.
func testSessionIntegrity(t *testing.T, s AuthStore) {
	ctx := t.Context()
	if err := s.CreateSession(ctx, newSession(uuid.New(), "orphan")); err == nil {
		t.Error("a session without a user was accepted")
	}
	u := newUser("i@example.com")
	_ = s.CreateUser(ctx, u)
	_ = s.CreateSession(ctx, newSession(u.ID, "same"))
	if err := s.CreateSession(ctx, newSession(u.ID, "same")); err == nil {
		t.Error("a duplicate token hash was accepted")
	}
}
