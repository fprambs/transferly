package credentials_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/credentials"
	"github.com/fprambs/sentinel/internal/testutil"
)

// lifetime is the longest a session lasts in these tests.
const lifetime = 12 * time.Hour

// newStore returns a store on a fake clock, and the clock.
func newStore() (*credentials.Store, *testutil.FakeClock) {
	clock := testutil.NewFakeClock(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	return credentials.New(clock, lifetime), clock
}

// keyOf builds the key of a session for a site of a user.
func keyOf(session string, user, site uuid.UUID) credentials.Key {
	return credentials.Key{Session: session, UserID: user, WorkspaceID: uuid.MustParse("00000000-0000-0000-0000-0000000000aa"), SiteID: site}
}

// never reports no connection open to any site.
func never(uuid.UUID, uuid.UUID, uuid.UUID) bool { return false }

// always reports a connection open to every site.
func always(uuid.UUID, uuid.UUID, uuid.UUID) bool { return true }

// TestStore_PutAndPassword covers [credentials.Store.Put] and [credentials.Store.Password].
//
// Use cases:
//   - a password is handed back as a copy, so wiping the copy leaves the held one intact
//   - the store keeps its own copy, so wiping what was put leaves the held one intact
//   - a second password for the same key replaces the first
//   - nothing is held for another session or another site
func TestStore_PutAndPassword(t *testing.T) {
	t.Parallel()
	store, _ := newStore()
	user, site := uuid.New(), uuid.New()
	key := keyOf("s1", user, site)
	typed := []byte("secret")
	store.Put(key, typed, true)
	clear(typed)

	got, ok := store.Password(key)
	if !ok || string(got) != "secret" {
		t.Fatalf("Password = %q, %v; want secret, true", got, ok)
	}
	clear(got)
	if again, _ := store.Password(key); string(again) != "secret" {
		t.Fatalf("wiping the copy changed the held password: %q", again)
	}

	store.Put(key, []byte("other"), true)
	if got, _ := store.Password(key); string(got) != "other" {
		t.Fatalf("Password after replacing = %q, want other", got)
	}
	if _, ok := store.Password(keyOf("s2", user, site)); ok {
		t.Fatal("another session got the password")
	}
	if _, ok := store.Password(keyOf("s1", user, uuid.New())); ok {
		t.Fatal("another site got the password")
	}
}

// TestStore_Admits covers [credentials.Store.Admits] and [credentials.Store.MarkAsk].
//
// Use cases:
//   - a site nobody marked as asking admits every session
//   - once a site asks, only a session holding its own password is admitted
//   - putting a password marks the site as asking too
func TestStore_Admits(t *testing.T) {
	t.Parallel()
	store, _ := newStore()
	user, site := uuid.New(), uuid.New()
	if !store.Admits(keyOf("s1", user, site)) {
		t.Fatal("a site that does not ask refused a session")
	}
	store.MarkAsk(site)
	if store.Admits(keyOf("s1", user, site)) {
		t.Fatal("a session with no password was admitted to a site that asks")
	}

	other := uuid.New()
	store.Put(keyOf("s1", user, other), []byte("pw"), true)
	if !store.Admits(keyOf("s1", user, other)) {
		t.Fatal("the session that typed the password was refused")
	}
	if store.Admits(keyOf("s2", user, other)) {
		t.Fatal("another session of the same user was admitted")
	}
}

// TestStore_Forgetting covers [credentials.Store.Forget], [credentials.Store.ForgetSite],
// [credentials.Store.CloseSession] and [credentials.Store.CloseUser].
//
// Use cases:
//   - a refused password is forgotten for its key only
//   - a changed site loses every password and stops asking until marked again
//   - an ended session, or a locked vault, loses every password typed in it and no other
//   - a disabled user loses every password of every session
func TestStore_Forgetting(t *testing.T) {
	t.Parallel()
	store, _ := newStore()
	ana, bob := uuid.New(), uuid.New()
	siteA, siteB := uuid.New(), uuid.New()
	for _, key := range []credentials.Key{keyOf("a1", ana, siteA), keyOf("a1", ana, siteB), keyOf("a2", ana, siteA), keyOf("b1", bob, siteA)} {
		store.Put(key, []byte("pw"), true)
	}

	store.Forget(keyOf("a2", ana, siteA))
	held := func(key credentials.Key) bool { _, ok := store.Password(key); return ok }
	if held(keyOf("a2", ana, siteA)) || !held(keyOf("a1", ana, siteA)) {
		t.Fatal("Forget did not drop exactly its key")
	}

	store.ForgetSite(siteB)
	if held(keyOf("a1", ana, siteB)) {
		t.Fatal("ForgetSite kept a password of the site")
	}
	if !store.Admits(keyOf("x", ana, siteB)) {
		t.Fatal("a forgotten site still asks")
	}

	store.CloseSession("a1")
	if held(keyOf("a1", ana, siteA)) || !held(keyOf("b1", bob, siteA)) {
		t.Fatal("CloseSession did not drop exactly the passwords of the session")
	}

	store.Put(keyOf("b2", bob, siteB), []byte("pw"), true)
	store.CloseUser(bob)
	if held(keyOf("b1", bob, siteA)) || held(keyOf("b2", bob, siteB)) {
		t.Fatal("CloseUser kept a password of the user")
	}
}

// TestStore_Sweep covers [credentials.Store.Sweep].
//
// Use cases:
//   - a password to remember stays while its session can exist, and goes when the longest session is over
//   - a password not to remember stays for a minute even with no connection open, so the connection has time to open
//   - after that minute it stays while a connection to the site is open and goes once none is
func TestStore_Sweep(t *testing.T) {
	t.Parallel()
	store, clock := newStore()
	user, site := uuid.New(), uuid.New()
	kept, once := keyOf("s1", user, site), keyOf("s2", user, site)
	store.Put(kept, []byte("pw"), true)
	store.Put(once, []byte("pw"), false)

	if n := store.Sweep(never); n != 0 {
		t.Fatalf("Sweep right away dropped %d, want 0", n)
	}
	clock.Advance(time.Minute)
	if n := store.Sweep(always); n != 0 {
		t.Fatalf("Sweep with the connection open dropped %d, want 0", n)
	}
	if n := store.Sweep(never); n != 1 {
		t.Fatalf("Sweep once the connection closed dropped %d, want 1", n)
	}
	if _, ok := store.Password(once); ok {
		t.Fatal("the password not to remember survived")
	}
	clock.Advance(lifetime)
	if n := store.Sweep(always); n != 1 {
		t.Fatalf("Sweep past the session lifetime dropped %d, want 1", n)
	}
	if _, ok := store.Password(kept); ok {
		t.Fatal("the password outlived the longest session")
	}
}
