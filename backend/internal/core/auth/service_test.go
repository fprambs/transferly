package auth_test

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/vault"
)

const (
	testEmail    = "admin@example.com"
	testPassword = "a long enough password"
)

// signedIn creates an administrator and signs them in.
func signedIn(t *testing.T, h *harness) (string, auth.Principal) {
	t.Helper()
	ctx := context.Background()
	if _, err := h.svc.Setup(ctx, testEmail, testPassword); err != nil {
		t.Fatalf("Setup() = %v", err)
	}
	token, principal, err := h.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{UserAgent: "test", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Login() = %v", err)
	}
	return token, principal
}

// TestSetupAndRegister verifies account creation.
//
// Scenarios:
//   - setup is required until the first administrator exists, who gets the admin role and default preferences
//   - a second setup is refused
//   - emails are normalized and invalid ones, weak passwords and duplicates are rejected
//   - regular accounts can be registered
func TestSetupAndRegister(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()

	if required, err := h.svc.SetupRequired(ctx); !required || err != nil {
		t.Fatalf("SetupRequired() = %v, %v, want true", required, err)
	}
	admin, err := h.svc.Setup(ctx, "  Admin@Example.COM ", testPassword)
	if err != nil {
		t.Fatalf("Setup() = %v", err)
	}
	if admin.Email != testEmail || admin.Role != auth.RoleAdmin || admin.Locale != "en" || admin.Timezone != "UTC" {
		t.Errorf("admin = %+v, want a normalized admin with default preferences", admin)
	}
	if strings.Contains(admin.PasswordHash, testPassword) || !strings.HasPrefix(admin.PasswordHash, "$argon2id$") {
		t.Error("the password hash is not an Argon2id hash")
	}
	if required, _ := h.svc.SetupRequired(ctx); required {
		t.Error("SetupRequired() = true after the first administrator")
	}
	if _, err := h.svc.Setup(ctx, "second@example.com", testPassword); !errors.Is(err, auth.ErrSetupComplete) {
		t.Errorf("second Setup() = %v, want ErrSetupComplete", err)
	}

	user, err := h.svc.Register(ctx, "User@Example.com", testPassword)
	if err != nil || user.Role != auth.RoleUser {
		t.Fatalf("Register() = %+v, %v, want a regular user", user, err)
	}
	if _, err := h.svc.Register(ctx, "user@example.com", testPassword); !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("duplicate Register() = %v, want ErrEmailTaken", err)
	}
}

// TestValidation verifies input validation.
//
// Scenarios:
//   - malformed emails, display names, empty and oversized addresses are rejected
//   - passwords shorter than 12 characters or longer than 1024 bytes are rejected
//   - multi-byte characters count as characters, so 12 of them are enough
func TestValidation(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()

	for _, email := range []string{"", "no-at-sign", "a@", "@b.com", "Name <a@b.com>", strings.Repeat("a", 250) + "@b.com", "a b@c.com"} {
		if _, err := h.svc.Register(ctx, email, testPassword); !errors.Is(err, auth.ErrInvalidEmail) {
			t.Errorf("Register(%q) = %v, want ErrInvalidEmail", email, err)
		}
	}
	for _, password := range []string{"", "short", "elevenchars", strings.Repeat("x", 1025)} {
		if _, err := h.svc.Register(ctx, "ok@example.com", password); !errors.Is(err, auth.ErrWeakPassword) {
			t.Errorf("Register(password %d bytes) = %v, want ErrWeakPassword", len(password), err)
		}
	}
	if _, err := h.svc.Register(ctx, "unicode@example.com", strings.Repeat("ñ", 12)); err != nil {
		t.Errorf("Register(12 multi-byte characters) = %v, want success", err)
	}
}

// TestLogin verifies sign-in.
//
// Scenarios:
//   - correct credentials open a session whose token is never stored, only its hash
//   - the session records the client and expires at the absolute timeout
//   - a wrong password and an unknown account give the same error and the same amount of hashing work
//   - a disabled account is refused after the password check
//   - each sign-in gets a different token
func TestLogin(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	token, principal := signedIn(t, h)

	if token == "" || len(token) < 40 {
		t.Errorf("token = %q, want a long random token", token)
	}
	for _, s := range h.store.sessions {
		if string(s.TokenHash) == token || strings.Contains(string(s.TokenHash), token) {
			t.Error("the session token is stored in clear text")
		}
	}
	session := principal.Session
	if session.UserAgent != "test" || session.IP != "10.0.0.1" || !session.ExpiresAt.Equal(h.clock.Now().Add(8*time.Hour)) {
		t.Errorf("session = %+v, want the client and an 8 hour lifetime", session)
	}

	before := h.deriver.count()
	_, _, wrong := h.svc.Login(ctx, testEmail, "not the password!!", auth.ClientInfo{})
	afterWrong := h.deriver.count()
	_, _, unknown := h.svc.Login(ctx, "nobody@example.com", "not the password!!", auth.ClientInfo{})
	afterUnknown := h.deriver.count()
	if !errors.Is(wrong, auth.ErrInvalidCredentials) || !errors.Is(unknown, auth.ErrInvalidCredentials) {
		t.Errorf("errors = %v and %v, want ErrInvalidCredentials for both", wrong, unknown)
	}
	if afterWrong-before != 1 || afterUnknown-afterWrong != 1 {
		t.Errorf("hash work = %d and %d derivations, want 1 for each", afterWrong-before, afterUnknown-afterWrong)
	}

	again, _, _ := h.svc.Login(ctx, " ADMIN@example.com ", testPassword, auth.ClientInfo{})
	if again == token {
		t.Error("two sign-ins share a token")
	}

	user := h.store.users[principal.User.ID]
	user.Disabled = true
	h.store.users[user.ID] = user
	if _, _, err := h.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{}); !errors.Is(err, auth.ErrUserDisabled) {
		t.Errorf("Login() of a disabled account = %v, want ErrUserDisabled", err)
	}
	if _, _, err := h.svc.Login(ctx, testEmail, "wrong password here", auth.ClientInfo{}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("Login() of a disabled account with a wrong password = %v, want ErrInvalidCredentials", err)
	}
}

// TestLogin_UpgradesWeakHashes verifies that a hash made under older parameters is replaced after sign-in.
func TestLogin_UpgradesWeakHashes(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	old := auth.NewHasher(vault.NewKDF(1), vault.Params{MemoryKiB: 32, Time: 1, Threads: 1}, rand.Reader)
	hash, _ := old.Hash(ctx, testPassword)
	id := uuid.New()
	h.store.users[id] = auth.User{ID: id, Email: testEmail, PasswordHash: hash, Role: auth.RoleUser}

	if _, _, err := h.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{}); err != nil {
		t.Fatalf("Login() = %v", err)
	}
	if !strings.Contains(h.store.users[id].PasswordHash, "m=64,t=1,p=1") {
		t.Errorf("hash after sign-in = %q, want the current parameters", h.store.users[id].PasswordHash)
	}
}

// TestAuthenticate verifies session validation.
//
// Scenarios:
//   - a valid token returns its principal
//   - unknown and empty tokens are rejected
//   - sessions past the absolute end or idle for too long are rejected and deleted
//   - use is recorded at most once a minute, and activity keeps a session alive past the idle limit
//   - a disabled account is refused
func TestAuthenticate(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	token, principal := signedIn(t, h)

	got, err := h.svc.Authenticate(ctx, token)
	if err != nil || got.User.ID != principal.User.ID || got.Session.ID != principal.Session.ID {
		t.Fatalf("Authenticate() = %+v, %v, want the principal", got, err)
	}
	for _, bad := range []string{"", "unknown-token"} {
		if _, err := h.svc.Authenticate(ctx, bad); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Errorf("Authenticate(%q) = %v, want ErrUnauthenticated", bad, err)
		}
	}

	h.clock.Advance(30 * time.Second)
	if _, err := h.svc.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	if !h.store.sessions[principal.Session.ID].LastSeenAt.Equal(principal.Session.LastSeenAt) {
		t.Error("use was recorded again within a minute")
	}
	for range 4 {
		h.clock.Advance(20 * time.Minute)
		if _, err := h.svc.Authenticate(ctx, token); err != nil {
			t.Fatalf("active session rejected: %v", err)
		}
	}

	h.clock.Advance(31 * time.Minute)
	if _, err := h.svc.Authenticate(ctx, token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("idle session = %v, want ErrUnauthenticated", err)
	}
	if len(h.store.sessions) != 0 {
		t.Error("the idle session was not deleted")
	}

	fresh, _, _ := h.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{})
	for range 30 {
		h.clock.Advance(20 * time.Minute)
		if _, err := h.svc.Authenticate(ctx, fresh); err != nil {
			break
		}
	}
	if _, err := h.svc.Authenticate(ctx, fresh); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("session past the absolute timeout = %v, want ErrUnauthenticated", err)
	}

	again, _, _ := h.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{})
	user := h.store.users[principal.User.ID]
	user.Disabled = true
	h.store.users[user.ID] = user
	if _, err := h.svc.Authenticate(ctx, again); !errors.Is(err, auth.ErrUserDisabled) {
		t.Errorf("Authenticate() of a disabled account = %v, want ErrUserDisabled", err)
	}
}

// TestLogoutAndPurge verifies ending sessions.
//
// Scenarios:
//   - Logout ends one session and ignores unknown tokens
//   - LogoutAll ends every session of a user
//   - PurgeExpired removes only sessions that are expired or idle
func TestLogoutAndPurge(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	first, principal := signedIn(t, h)
	second, _, _ := h.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{})

	if err := h.svc.Logout(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Authenticate(ctx, first); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("Authenticate() after Logout = %v, want ErrUnauthenticated", err)
	}
	if err := h.svc.Logout(ctx, "unknown"); err != nil {
		t.Errorf("Logout(unknown) = %v, want nil", err)
	}
	if _, err := h.svc.Authenticate(ctx, second); err != nil {
		t.Errorf("the other session was ended: %v", err)
	}

	if err := h.svc.LogoutAll(ctx, principal.User.ID); err != nil {
		t.Fatal(err)
	}
	if len(h.store.sessions) != 0 {
		t.Errorf("%d sessions remain after LogoutAll", len(h.store.sessions))
	}

	_, _, _ = h.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{})
	h.clock.Advance(20 * time.Minute)
	_, _, _ = h.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{})
	h.clock.Advance(15 * time.Minute)
	if n, err := h.svc.PurgeExpired(ctx); err != nil || n != 1 {
		t.Errorf("PurgeExpired() = %d, %v, want 1 idle session removed", n, err)
	}
}

// TestUpdatePreferences verifies preference updates.
//
// Scenarios:
//   - a valid language tag, IANA zone, theme and double click action are stored, the tag in canonical form, and new users start on the
//     default theme and download on double click
//   - empty values keep the current setting
//   - invalid tags, unknown zones, the Local pseudo-zone, fixed offsets, malformed theme identifiers and unknown double click actions are rejected
//   - a failing store is reported
func TestUpdatePreferences(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	_, principal := signedIn(t, h)

	if principal.User.Theme != "transferly-dark" || principal.User.FileDoubleClick != auth.FileDoubleClickDownload {
		t.Errorf("a new user has the theme %q and double click %q; want transferly-dark and download", principal.User.Theme, principal.User.FileDoubleClick)
	}
	updated, err := h.svc.UpdatePreferences(ctx, principal.User, auth.Preferences{Locale: "es-cl", Timezone: "America/Santiago", Theme: "monokai", FileDoubleClick: auth.FileDoubleClickEdit})
	if err != nil || updated.Locale != "es-CL" || updated.Timezone != "America/Santiago" || updated.Theme != "monokai" || updated.FileDoubleClick != auth.FileDoubleClickEdit {
		t.Fatalf("UpdatePreferences() = %+v, %v", updated, err)
	}
	if stored := h.store.users[principal.User.ID]; stored.Locale != "es-CL" || stored.Timezone != "America/Santiago" || stored.Theme != "monokai" || stored.FileDoubleClick != auth.FileDoubleClickEdit {
		t.Errorf("stored user = %+v", stored)
	}
	kept, err := h.svc.UpdatePreferences(ctx, updated, auth.Preferences{})
	if err != nil || kept.Locale != "es-CL" || kept.Timezone != "America/Santiago" || kept.Theme != "monokai" || kept.FileDoubleClick != auth.FileDoubleClickEdit {
		t.Errorf("empty update = %+v, %v, want the current settings", kept, err)
	}
	if previews, err := h.svc.UpdatePreferences(ctx, kept, auth.Preferences{FileDoubleClick: auth.FileDoubleClickPreview}); err != nil || previews.FileDoubleClick != auth.FileDoubleClickPreview {
		t.Errorf("double click to preview = %+v, %v", previews, err)
	}

	for _, locale := range []string{"not a tag!", "x"} {
		if _, err := h.svc.UpdatePreferences(ctx, updated, auth.Preferences{Locale: locale}); !errors.Is(err, auth.ErrInvalidLocale) {
			t.Errorf("locale %q = %v, want ErrInvalidLocale", locale, err)
		}
	}
	for _, zone := range []string{"Mars/Olympus", "Local", "+01:00", "../etc/passwd"} {
		if _, err := h.svc.UpdatePreferences(ctx, updated, auth.Preferences{Timezone: zone}); !errors.Is(err, auth.ErrInvalidTimezone) {
			t.Errorf("time zone %q = %v, want ErrInvalidTimezone", zone, err)
		}
	}
	for _, theme := range []string{"Monokai", "with space", "-dash", "", "a/b", strings.Repeat("x", 41)} {
		if theme == "" {
			continue
		}
		if _, err := h.svc.UpdatePreferences(ctx, updated, auth.Preferences{Theme: theme}); !errors.Is(err, auth.ErrInvalidTheme) {
			t.Errorf("theme %q = %v, want ErrInvalidTheme", theme, err)
		}
	}
	if _, err := h.svc.UpdatePreferences(ctx, updated, auth.Preferences{FileDoubleClick: "open"}); !errors.Is(err, auth.ErrInvalidFileDoubleClick) {
		t.Errorf("double click %q = %v, want ErrInvalidFileDoubleClick", "open", err)
	}
	h.store.failing("UpdatePreferences")
	if _, err := h.svc.UpdatePreferences(ctx, updated, auth.Preferences{Locale: "en", Timezone: "UTC", Theme: "nord"}); !errors.Is(err, errBoom) {
		t.Errorf("UpdatePreferences() with a failing store = %v", err)
	}
}

// TestStoreFailures verifies that store and entropy failures surface as errors on every path.
func TestStoreFailures(t *testing.T) {
	ctx := context.Background()
	prepare := func(method string) (*harness, string, auth.Principal) {
		h := newHarness(t, nil)
		token, principal := signedIn(t, h)
		h.store.failing(method)
		return h, token, principal
	}

	for method, call := range map[string]func(h *harness, token string, p auth.Principal) error{
		"CountUsers": func(h *harness, _ string, _ auth.Principal) error { _, err := h.svc.SetupRequired(ctx); return err },
		"UserByEmail": func(h *harness, _ string, _ auth.Principal) error {
			_, _, err := h.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{})
			return err
		},
		"CreateSession": func(h *harness, _ string, _ auth.Principal) error {
			_, _, err := h.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{})
			return err
		},
		"SessionByTokenHash": func(h *harness, token string, _ auth.Principal) error {
			_, err := h.svc.Authenticate(ctx, token)
			return err
		},
		"UserByID": func(h *harness, token string, _ auth.Principal) error {
			_, err := h.svc.Authenticate(ctx, token)
			return err
		},
		"TouchSession": func(h *harness, token string, _ auth.Principal) error {
			h.clock.Advance(2 * time.Minute)
			_, err := h.svc.Authenticate(ctx, token)
			return err
		},
		"DeleteSession": func(h *harness, token string, _ auth.Principal) error {
			h.clock.Advance(9 * time.Hour)
			_, err := h.svc.Authenticate(ctx, token)
			return err
		},
		"DeleteUserSessions":    func(h *harness, _ string, p auth.Principal) error { return h.svc.LogoutAll(ctx, p.User.ID) },
		"DeleteExpiredSessions": func(h *harness, _ string, _ auth.Principal) error { _, err := h.svc.PurgeExpired(ctx); return err },
	} {
		h, token, principal := prepare(method)
		if err := call(h, token, principal); !errors.Is(err, errBoom) {
			t.Errorf("%s failure = %v, want the store error", method, err)
		}
	}

	h, token, _ := prepare("SessionByTokenHash")
	if err := h.svc.Logout(ctx, token); !errors.Is(err, errBoom) {
		t.Errorf("Logout() with a failing lookup = %v, want the store error", err)
	}
	h, token, _ = prepare("DeleteSession")
	if err := h.svc.Logout(ctx, token); !errors.Is(err, errBoom) {
		t.Errorf("Logout() with a failing delete = %v, want the store error", err)
	}

	fresh := newHarness(t, newFakeStore().failing("CreateFirstUser"))
	if _, err := fresh.svc.Setup(ctx, testEmail, testPassword); !errors.Is(err, errBoom) {
		t.Errorf("Setup() with a failing store = %v", err)
	}
	fresh = newHarness(t, newFakeStore().failing("CreateUser"))
	if _, err := fresh.svc.Register(ctx, testEmail, testPassword); !errors.Is(err, errBoom) {
		t.Errorf("Register() with a failing store = %v", err)
	}

	upgrade := newHarness(t, nil)
	old := auth.NewHasher(vault.NewKDF(1), vault.Params{MemoryKiB: 32, Time: 1, Threads: 1}, rand.Reader)
	hash, _ := old.Hash(ctx, testPassword)
	id := uuid.New()
	upgrade.store.users[id] = auth.User{ID: id, Email: testEmail, PasswordHash: hash}
	entropyless := auth.NewService(upgrade.store, upgrade.store, upgrade.store, auth.NewHasher(vault.NewKDF(1), fastParams, failingReader{}), upgrade.clock, rand.Reader,
		auth.Config{IdleTimeout: time.Hour, AbsoluteTimeout: time.Hour})
	if _, _, err := entropyless.Login(ctx, testEmail, testPassword, auth.ClientInfo{}); !errors.Is(err, errBoom) {
		t.Errorf("Login() with a hash upgrade lacking entropy = %v, want the entropy error", err)
	}
	upgrade.store.failing("UpdatePasswordHash")
	if _, _, err := upgrade.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{}); !errors.Is(err, errBoom) {
		t.Errorf("Login() with a failing hash upgrade = %v, want the store error", err)
	}
}

// TestGenerationFailures verifies failures of the identifier generator, entropy source and hashing.
func TestGenerationFailures(t *testing.T) {
	ctx := context.Background()
	broken := func() (uuid.UUID, error) { return uuid.Nil, errBoom }

	h := newHarness(t, nil)
	h.svc.SetIDGenerator(broken)
	if _, err := h.svc.Setup(ctx, testEmail, testPassword); !errors.Is(err, errBoom) {
		t.Errorf("Setup() with a failing id generator = %v", err)
	}

	h = newHarness(t, nil)
	signedIn(t, h)
	h.svc.SetIDGenerator(broken)
	if _, _, err := h.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{}); !errors.Is(err, errBoom) {
		t.Errorf("Login() with a failing id generator = %v", err)
	}

	store := newFakeStore()
	deriver := &countingDeriver{inner: vault.NewKDF(1)}
	hasher := auth.NewHasher(deriver, fastParams, rand.Reader)
	noEntropy := auth.NewService(store, store, store, hasher, h.clock, failingReader{}, auth.Config{IdleTimeout: time.Hour, AbsoluteTimeout: time.Hour})
	if _, err := noEntropy.Setup(ctx, testEmail, testPassword); err != nil {
		t.Fatal(err)
	}
	if _, _, err := noEntropy.Login(ctx, testEmail, testPassword, auth.ClientInfo{}); !errors.Is(err, errBoom) {
		t.Errorf("Login() without entropy = %v, want the entropy error", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	healthy := newHarness(t, nil)
	if _, err := healthy.svc.Setup(canceled, "x@example.com", testPassword); !errors.Is(err, context.Canceled) {
		t.Errorf("Setup() with a canceled context = %v", err)
	}
	if _, _, err := healthy.svc.Login(canceled, testEmail, testPassword, auth.ClientInfo{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Login() with a canceled context = %v", err)
	}

	corrupt := newHarness(t, nil)
	id := uuid.New()
	corrupt.store.users[id] = auth.User{ID: id, Email: testEmail, PasswordHash: "garbage"}
	if _, _, err := corrupt.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{}); !errors.Is(err, auth.ErrMalformedHash) {
		t.Errorf("Login() with a corrupt stored hash = %v, want ErrMalformedHash", err)
	}
}

// TestVerify verifies validation that does not count as use.
//
// Scenarios:
//   - a valid token returns its principal, and unknown tokens are rejected
//   - verifying does not record use, so a session idle for too long is rejected however often it is verified
//   - expired sessions are deleted, and a disabled account is refused
func TestVerify(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	token, principal := signedIn(t, h)

	got, err := h.svc.Verify(ctx, token)
	if err != nil || got.User.ID != principal.User.ID {
		t.Fatalf("Verify() = %+v, %v", got, err)
	}
	if _, err := h.svc.Verify(ctx, "unknown-token"); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("Verify(unknown) = %v", err)
	}

	for range 3 {
		h.clock.Advance(20 * time.Minute)
		_, err = h.svc.Verify(ctx, token)
	}
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("Verify() of a session that only a background check kept = %v; want ErrUnauthenticated", err)
	}
	if len(h.store.sessions) != 0 {
		t.Error("the idle session was not deleted")
	}

	again, _, _ := h.svc.Login(ctx, testEmail, testPassword, auth.ClientInfo{})
	user := h.store.users[principal.User.ID]
	user.Disabled = true
	h.store.users[user.ID] = user
	if _, err := h.svc.Verify(ctx, again); !errors.Is(err, auth.ErrUserDisabled) {
		t.Errorf("Verify() of a disabled account = %v", err)
	}
}

// TestCheckPasswordAndVerify verifies the password checks that do not open a session.
//
// Scenarios:
//   - correct credentials return the account and store no session, and wrong ones and unknown accounts are the same refusal
//   - a signed-in user is asked for their password again: the right one passes, a wrong one is refused
//   - accounts are found by identifier, and unknown ones are ErrNotFound
func TestCheckPasswordAndVerify(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	_, principal := signedIn(t, h)
	sessions := len(h.store.sessions)

	user, err := h.svc.CheckPassword(ctx, testEmail, testPassword)
	if err != nil || user.ID != principal.User.ID || len(h.store.sessions) != sessions {
		t.Fatalf("CheckPassword() = %+v, %v with %d sessions; want the account and no new session", user, err, len(h.store.sessions))
	}
	if _, err := h.svc.CheckPassword(ctx, testEmail, "not the password!!"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("wrong password = %v; want ErrInvalidCredentials", err)
	}
	if err := h.svc.VerifyPassword(ctx, user, testPassword); err != nil {
		t.Errorf("VerifyPassword() = %v", err)
	}
	if err := h.svc.VerifyPassword(ctx, user, "not the password!!"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("VerifyPassword() with a wrong password = %v; want ErrInvalidCredentials", err)
	}
	broken := user
	broken.PasswordHash = "not a hash"
	if err := h.svc.VerifyPassword(ctx, broken, testPassword); err == nil || errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("VerifyPassword() with a corrupt hash = %v; want the error of the hasher", err)
	}
	if found, err := h.svc.UserByID(ctx, user.ID); err != nil || found.Email != testEmail {
		t.Errorf("UserByID() = %+v, %v", found, err)
	}
	if _, err := h.svc.UserByID(ctx, uuid.New()); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("UserByID(unknown) = %v; want ErrNotFound", err)
	}
}
