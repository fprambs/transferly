package httpapi_test

import (
	"crypto/rand"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/ratelimit"
	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

const (
	masterPassword = "correct horse battery"
	newMaster      = "another long password"
)

// newTestVault returns a vault service over the store with a fast key derivation.
func newTestVault(store *postgres.Store, clock ports.Clock) *vault.Service {
	v, err := vault.New(vault.NewKDF(2), vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}, rand.Reader)
	if err != nil {
		panic(err)
	}
	return vault.NewService(v, store, vault.NewKeyRing(clock, time.Hour))
}

// vaultCall posts a JSON body to a vault endpoint with the session and its anti-CSRF token.
func (a *api) vaultCall(path string, body any, cookie *http.Cookie, csrf string) reply {
	a.t.Helper()
	return a.do(request{method: http.MethodPost, path: "/api/v1/vault" + path, body: body, cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf}})
}

// vaultState reads the state of the vault in a session.
func (a *api) vaultState(cookie *http.Cookie) (initialized, unlocked bool) {
	a.t.Helper()
	r := a.do(request{method: http.MethodGet, path: "/api/v1/vault", cookie: cookie})
	var state httpapi.VaultStatus
	if r.Status != http.StatusOK {
		a.t.Fatalf("GET vault = %d %s", r.Status, r.Body)
	}
	mustDecode(a.t, r.Body, &state)
	return state.Initialized, state.Unlocked
}

// TestVault_Lifecycle verifies the vault from creation to recovery.
//
// Scenarios:
//   - a new account has no vault; setup creates it, unlocks the session and returns a recovery kit; a second setup is 409
//   - a weak master password is 422
//   - locking and unlocking work, a wrong master password is 403 invalid_secret
//   - the master password changes with the current one only; the old one stops working
//   - a second session starts locked and unlocks independently; logging out locks the session's key
//   - recovery with the kit sets a new master password; a wrong kit is 403
//   - regenerating the kit needs the master password and invalidates the old kit
//   - operations before setup are 409 vault_not_initialized
func TestVault_Lifecycle(t *testing.T) {
	a := newAPI(t, func(d *httpapi.Deps) {
		d.Limits.VaultAttempts = ratelimit.New(realClock{}, ratelimit.Rule{Burst: 100, Every: time.Second}, 10)
	})
	cookie, csrf := a.setup()

	if init, _ := a.vaultState(cookie); init {
		t.Fatal("a new account already has a vault")
	}
	expectProblem(t, a.vaultCall("/unlock", map[string]string{"masterPassword": masterPassword}, cookie, csrf), 409, "vault_not_initialized")
	expectProblem(t, a.vaultCall("/password", map[string]string{"currentPassword": masterPassword, "newPassword": newMaster}, cookie, csrf), 409, "vault_not_initialized")
	expectProblem(t, a.vaultCall("/recover", map[string]string{"recoveryKit": "x", "newPassword": newMaster}, cookie, csrf), 409, "vault_not_initialized")
	expectProblem(t, a.vaultCall("/recovery-kit", map[string]string{"masterPassword": masterPassword}, cookie, csrf), 409, "vault_not_initialized")

	expectProblem(t, a.vaultCall("/setup", map[string]string{"masterPassword": "short"}, cookie, csrf), 422, "weak_password")
	created := a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, cookie, csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("setup = %d %s", created.Status, created.Body)
	}
	var kit httpapi.RecoveryKit
	mustDecode(t, created.Body, &kit)
	if kit.RecoveryKit == "" {
		t.Fatal("no recovery kit returned")
	}
	if init, unlocked := a.vaultState(cookie); !init || !unlocked {
		t.Errorf("after setup: initialized=%v unlocked=%v", init, unlocked)
	}
	expectProblem(t, a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, cookie, csrf), 409, "vault_exists")

	if r := a.vaultCall("/lock", nil, cookie, csrf); r.Status != http.StatusNoContent {
		t.Fatalf("lock = %d %s", r.Status, r.Body)
	}
	if _, unlocked := a.vaultState(cookie); unlocked {
		t.Error("the vault is still unlocked after locking")
	}
	expectProblem(t, a.vaultCall("/unlock", map[string]string{"masterPassword": "wrong wrong wrong"}, cookie, csrf), 403, "invalid_secret")
	if r := a.vaultCall("/unlock", map[string]string{"masterPassword": masterPassword}, cookie, csrf); r.Status != http.StatusNoContent {
		t.Fatalf("unlock = %d %s", r.Status, r.Body)
	}

	expectProblem(t, a.vaultCall("/password", map[string]string{"currentPassword": "wrong wrong wrong", "newPassword": newMaster}, cookie, csrf), 403, "invalid_secret")
	expectProblem(t, a.vaultCall("/password", map[string]string{"currentPassword": masterPassword, "newPassword": "short"}, cookie, csrf), 422, "weak_password")
	if r := a.vaultCall("/password", map[string]string{"currentPassword": masterPassword, "newPassword": newMaster}, cookie, csrf); r.Status != http.StatusNoContent {
		t.Fatalf("change password = %d %s", r.Status, r.Body)
	}

	second := a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": adminEmail, "password": adminPassword}})
	cookie2, csrf2 := second.sessionCookie(), second.session(t).CsrfToken
	if init, unlocked := a.vaultState(cookie2); !init || unlocked {
		t.Errorf("second session: initialized=%v unlocked=%v; want initialized and locked", init, unlocked)
	}
	expectProblem(t, a.vaultCall("/unlock", map[string]string{"masterPassword": masterPassword}, cookie2, csrf2), 403, "invalid_secret")
	if r := a.vaultCall("/unlock", map[string]string{"masterPassword": newMaster}, cookie2, csrf2); r.Status != http.StatusNoContent {
		t.Fatalf("unlock with the new password = %d %s", r.Status, r.Body)
	}

	expectProblem(t, a.vaultCall("/recover", map[string]string{"recoveryKit": "not a kit", "newPassword": masterPassword}, cookie2, csrf2), 403, "invalid_secret")
	expectProblem(t, a.vaultCall("/recover", map[string]string{"recoveryKit": kit.RecoveryKit, "newPassword": "short"}, cookie2, csrf2), 422, "weak_password")
	if r := a.vaultCall("/recover", map[string]string{"recoveryKit": kit.RecoveryKit, "newPassword": masterPassword}, cookie2, csrf2); r.Status != http.StatusNoContent {
		t.Fatalf("recover = %d %s", r.Status, r.Body)
	}

	expectProblem(t, a.vaultCall("/recovery-kit", map[string]string{"masterPassword": "wrong wrong wrong"}, cookie, csrf), 403, "invalid_secret")
	regenerated := a.vaultCall("/recovery-kit", map[string]string{"masterPassword": masterPassword}, cookie, csrf)
	var next httpapi.RecoveryKit
	mustDecode(t, regenerated.Body, &next)
	if regenerated.Status != http.StatusOK || next.RecoveryKit == "" || next.RecoveryKit == kit.RecoveryKit {
		t.Fatalf("regenerate = %d %s", regenerated.Status, regenerated.Body)
	}
	expectProblem(t, a.vaultCall("/recover", map[string]string{"recoveryKit": kit.RecoveryKit, "newPassword": newMaster}, cookie, csrf), 403, "invalid_secret")

	if r := a.do(request{method: http.MethodPost, path: "/api/v1/auth/logout", cookie: cookie2, headers: map[string]string{"X-CSRF-Token": csrf2}}); r.Status != http.StatusNoContent {
		t.Fatalf("logout = %d", r.Status)
	}
}

// TestVault_Protection verifies the guards around the vault endpoints.
//
// Scenarios:
//   - every endpoint needs a session and the state-changing ones the anti-CSRF token
//   - guessing secrets is rate limited per account with a Retry-After header, on unlock, change, recover and kit regeneration
//   - malformed bodies are 400
func TestVault_Protection(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, cookie, csrf)

	for _, path := range []string{"", "/setup", "/unlock", "/lock", "/password", "/recover", "/recovery-kit"} {
		method := http.MethodPost
		if path == "" {
			method = http.MethodGet
		}
		expectProblem(t, a.do(request{method: method, path: "/api/v1/vault" + path, body: map[string]string{}}), 401, "unauthenticated")
		if method == http.MethodPost {
			expectProblem(t, a.do(request{method: method, path: "/api/v1/vault" + path, cookie: cookie, body: map[string]string{}}), 403, "csrf_invalid")
		}
	}
	expectProblem(t, a.vaultCall("/unlock", "{not json", cookie, csrf), 400, "invalid_request")

	guess := map[string]string{"masterPassword": "wrong wrong wrong"}
	for i := 0; i < 4; i++ {
		expectProblem(t, a.vaultCall("/unlock", guess, cookie, csrf), 403, "invalid_secret")
	}
	limited := a.vaultCall("/unlock", guess, cookie, csrf)
	expectProblem(t, limited, 429, "rate_limited")
	if wait, err := strconv.Atoi(limited.Header.Get("Retry-After")); err != nil || wait < 1 {
		t.Errorf("Retry-After = %q; want a positive number of seconds", limited.Header.Get("Retry-After"))
	}
	expectProblem(t, a.vaultCall("/password", map[string]string{"currentPassword": "x", "newPassword": newMaster}, cookie, csrf), 429, "rate_limited")
	expectProblem(t, a.vaultCall("/recover", map[string]string{"recoveryKit": "x", "newPassword": newMaster}, cookie, csrf), 429, "rate_limited")
	expectProblem(t, a.vaultCall("/recovery-kit", guess, cookie, csrf), 429, "rate_limited")
}

// TestConnections_ClosedOnLockAndLogout verifies that a user's connections do not outlive their vault or session.
//
// Scenarios:
//   - locking the vault closes the user's pooled connections
//   - signing out closes them too
//   - connections of other users are left alone
func TestConnections_ClosedOnLockAndLogout(t *testing.T) {
	var pool *connections.Manager
	a := newAPI(t, func(d *httpapi.Deps) { pool = d.Connections })
	cookie, csrf := a.setup()
	otherCookie, other := a.signIn("other@example.com")
	otherCSRF := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: otherCookie}).session(t).CsrfToken
	admin := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}).session(t).User.Id

	pooled := func(user uuid.UUID, c *http.Cookie, token string) connections.Key {
		ws := a.personal(c)
		created := a.send(http.MethodPost, sitesPath(ws), siteBody("Site "+uuid.NewString()), c, token)
		key := connections.Key{UserID: user, WorkspaceID: ws, SiteID: created.site(t).Id}
		lease, err := pool.Acquire(t.Context(), key, "s")
		if err != nil {
			t.Fatal(err)
		}
		lease.Release(nil)
		return key
	}
	mine, theirs := pooled(admin, cookie, csrf), pooled(other.ID, otherCookie, otherCSRF)

	if r := a.vaultCall("/lock", nil, cookie, csrf); r.Status != http.StatusNoContent {
		t.Fatalf("lock = %d %s", r.Status, r.Body)
	}
	if _, total := pool.Stats(mine); total != 0 {
		t.Error("locking the vault left the user's connections open")
	}
	if _, total := pool.Stats(theirs); total != 1 {
		t.Error("locking the vault closed another user's connections")
	}

	mine = pooled(admin, cookie, csrf)
	if r := a.do(request{method: http.MethodPost, path: "/api/v1/auth/logout", cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf}}); r.Status != http.StatusNoContent {
		t.Fatalf("logout = %d", r.Status)
	}
	if _, total := pool.Stats(mine); total != 0 {
		t.Error("signing out left the user's connections open")
	}
	if _, total := pool.Stats(theirs); total != 1 {
		t.Error("signing out closed another user's connections")
	}
}

// TestConnections_ClosedWhenTheSiteChanges verifies that a site does not keep connections made with settings it no longer has.
//
// Scenarios:
//   - editing a site closes its pooled connections, so the next use connects with the new settings, and deleting it closes them too
func TestConnections_ClosedWhenTheSiteChanges(t *testing.T) {
	var pool *connections.Manager
	a := newAPI(t, func(d *httpapi.Deps) { pool = d.Connections })
	cookie, csrf := a.setup()
	user := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}).session(t).User.Id
	ws := a.personal(cookie)
	created := a.send(http.MethodPost, sitesPath(ws), siteBody("Changing"), cookie, csrf)
	id := created.site(t).Id
	key := connections.Key{UserID: user, WorkspaceID: ws, SiteID: id}
	pooled := func() {
		lease, err := pool.Acquire(t.Context(), key, "s")
		if err != nil {
			t.Fatal(err)
		}
		lease.Release(nil)
	}

	pooled()
	body := siteBody("Changing")
	body["listTimezone"] = "Etc/GMT+3"
	if r := a.send(http.MethodPut, sitesPath(ws, id.String()), body, cookie, csrf); r.Status != http.StatusOK {
		t.Fatalf("update = %d %s", r.Status, r.Body)
	}
	if _, total := pool.Stats(key); total != 0 {
		t.Error("editing the site left its connections open")
	}
	pooled()
	if r := a.send(http.MethodDelete, sitesPath(ws, id.String()), nil, cookie, csrf); r.Status != http.StatusNoContent {
		t.Fatalf("delete = %d %s", r.Status, r.Body)
	}
	if _, total := pool.Stats(key); total != 0 {
		t.Error("deleting the site left its connections open")
	}
}
