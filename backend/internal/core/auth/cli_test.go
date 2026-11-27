package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/vault"
)

// testVerifier is a well-formed PKCE verifier of 43 characters.
const testVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

// testRedirect is a loopback callback of the terminal client.
const testRedirect = "http://127.0.0.1:49152/callback"

// challengeOf returns the S256 challenge of a verifier.
func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// cliRequest returns a valid request with a loopback redirect.
func cliRequest() auth.CLIRequest {
	return auth.CLIRequest{CodeChallenge: challengeOf(testVerifier), RedirectURI: testRedirect, ClientName: " transferly-tui ", ClientHost: "laptop"}
}

// terminal is the client information of the terminal client.
var terminal = auth.ClientInfo{UserAgent: "transferly-tui/1.4.0", IP: "10.0.0.9"}

// approved signs in an administrator and approves a request, returning the principal and the code.
func approved(t *testing.T, h *harness, req auth.CLIRequest) (auth.Principal, string) {
	t.Helper()
	_, principal := signedIn(t, h)
	return principal, approve(t, h, principal.User, req)
}

// approve approves a request of user from the browser at 10.0.0.1 and returns the code.
func approve(t *testing.T, h *harness, user auth.User, req auth.CLIRequest) string {
	t.Helper()
	code, err := h.svc.ApproveCLI(context.Background(), user, testPassword, req, auth.ClientInfo{IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("ApproveCLI() = %v", err)
	}
	return code
}

// TestCLISignIn verifies the approval in the browser and the exchange of its code for a session of the terminal client (ADR-0029).
//
// Scenarios:
//   - an approval with a loopback redirect is exchanged for a terminal session that carries the trimmed name and the host, the
//     terminal's address and agent, and lasts 30 days; its token authenticates
//   - an approval without a redirect, for the person to paste the code, is exchanged with no redirect
//   - the approval records the address of the browser and expires after 5 minutes
func TestCLISignIn(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	principal, code := approved(t, h, cliRequest())
	for _, a := range h.store.authorizations {
		if a.IP != "10.0.0.1" || a.ClientName != "transferly-tui" || !a.ExpiresAt.Equal(h.clock.Now().Add(5*time.Minute)) {
			t.Errorf("stored approval = %+v, want the browser address, the trimmed name and 5 minutes", a)
		}
		if string(a.CodeHash) == code {
			t.Error("the approval stores the code itself, want only its hash")
		}
	}

	token, session, err := h.svc.ExchangeCLI(ctx, auth.CLIExchange{Code: code, CodeVerifier: testVerifier, RedirectURI: testRedirect}, terminal)
	if err != nil {
		t.Fatalf("ExchangeCLI() = %v", err)
	}
	s := session.Session
	if session.User.ID != principal.User.ID || s.Kind != auth.SessionCLI || s.Name != "transferly-tui" || s.Host != "laptop" ||
		s.IP != "10.0.0.9" || s.UserAgent != "transferly-tui/1.4.0" || !s.ExpiresAt.Equal(h.clock.Now().Add(30*24*time.Hour)) {
		t.Errorf("ExchangeCLI() session = %+v", s)
	}
	if got, err := h.svc.Authenticate(ctx, token); err != nil || got.Session.ID != s.ID {
		t.Errorf("Authenticate(terminal token) = %+v, %v", got.Session, err)
	}

	pasted := cliRequest()
	pasted.RedirectURI = ""
	code, err = h.svc.ApproveCLI(ctx, principal.User, testPassword, pasted, auth.ClientInfo{})
	if err != nil {
		t.Fatalf("ApproveCLI(pasted) = %v", err)
	}
	if _, _, err := h.svc.ExchangeCLI(ctx, auth.CLIExchange{Code: code, CodeVerifier: testVerifier}, terminal); err != nil {
		t.Errorf("ExchangeCLI(pasted) = %v", err)
	}
}

// TestCLIRequestValidation verifies what an approval accepts.
//
// Scenarios:
//   - the redirect may be 127.0.0.1 or [::1] with any port from 1 to 65535
//   - other redirects are invalid_redirect: localhost, another loopback address, https, another path, a query, an empty query, a
//     fragment, a user, an encoded path, no port, port 0, a port above 65535, a port with a leading zero, a zone, not a URL
//   - a challenge that is not 43 characters of base64url of 32 bytes, an empty or too long name, a too long host, or names with
//     control characters are invalid requests; an empty host is accepted
//   - a wrong password is refused after the request is valid, and nothing is stored
func TestCLIRequestValidation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	_, principal := signedIn(t, h)
	approve := func(change func(*auth.CLIRequest)) error {
		req := cliRequest()
		change(&req)
		_, err := h.svc.ApproveCLI(ctx, principal.User, testPassword, req, auth.ClientInfo{})
		return err
	}

	for _, redirect := range []string{"http://127.0.0.1:1/callback", "http://[::1]:65535/callback"} {
		if err := approve(func(r *auth.CLIRequest) { r.RedirectURI = redirect }); err != nil {
			t.Errorf("redirect %q = %v, want accepted", redirect, err)
		}
	}
	for _, redirect := range []string{
		"http://localhost:8000/callback", "http://127.0.0.2:8000/callback", "https://127.0.0.1:8000/callback",
		"http://127.0.0.1:8000/other", "http://127.0.0.1:8000/callback?x=1", "http://127.0.0.1:8000/callback?",
		"http://127.0.0.1:8000/callback#x", "http://u@127.0.0.1:8000/callback", "http://127.0.0.1:8000/call%62ack",
		"http://127.0.0.1/callback", "http://127.0.0.1:0/callback", "http://127.0.0.1:65536/callback", "http://127.0.0.1:080/callback",
		"http://[::1%25eth0]:8000/callback", "http://[::1:8000/callback", "evil.example.com",
	} {
		if err := approve(func(r *auth.CLIRequest) { r.RedirectURI = redirect }); !errors.Is(err, auth.ErrInvalidRedirect) {
			t.Errorf("redirect %q = %v, want ErrInvalidRedirect", redirect, err)
		}
	}

	for name, change := range map[string]func(*auth.CLIRequest){
		"short challenge":         func(r *auth.CLIRequest) { r.CodeChallenge = "abc" },
		"challenge not base64url": func(r *auth.CLIRequest) { r.CodeChallenge = strings.Repeat("+", 43) },
		"empty name":              func(r *auth.CLIRequest) { r.ClientName = "  " },
		"long name":               func(r *auth.CLIRequest) { r.ClientName = strings.Repeat("n", 65) },
		"name with a newline":     func(r *auth.CLIRequest) { r.ClientName = "transferly\ntui" },
		"long host":               func(r *auth.CLIRequest) { r.ClientHost = strings.Repeat("h", 254) },
		"host not UTF-8":          func(r *auth.CLIRequest) { r.ClientHost = "\xff" },
	} {
		if err := approve(change); !errors.Is(err, auth.ErrInvalidCLIRequest) {
			t.Errorf("%s = %v, want ErrInvalidCLIRequest", name, err)
		}
	}
	if err := approve(func(r *auth.CLIRequest) { r.ClientHost = "" }); err != nil {
		t.Errorf("empty host = %v, want accepted", err)
	}

	stored := len(h.store.authorizations)
	if _, err := h.svc.ApproveCLI(ctx, principal.User, "wrong password", cliRequest(), auth.ClientInfo{}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("ApproveCLI(wrong password) = %v, want ErrInvalidCredentials", err)
	}
	if len(h.store.authorizations) != stored {
		t.Error("a refused approval was stored")
	}
}

// TestCLIExchangeRefusals verifies that every reason to refuse an exchange is the same invalid_grant.
//
// Scenarios:
//   - an unknown code, a code used once already, a code exchanged at its expiry, another redirect, a verifier of another
//     challenge, too short, too long or with a character outside the PKCE alphabet
//   - the account was disabled or deleted after the approval
//   - a failed exchange spends the code: the right verifier afterwards is refused too
func TestCLIExchangeRefusals(t *testing.T) {
	ctx := context.Background()
	exchange := func(h *harness, ex auth.CLIExchange) error {
		_, _, err := h.svc.ExchangeCLI(ctx, ex, terminal)
		return err
	}
	good := func(code string) auth.CLIExchange {
		return auth.CLIExchange{Code: code, CodeVerifier: testVerifier, RedirectURI: testRedirect}
	}

	h := newHarness(t, nil)
	_, code := approved(t, h, cliRequest())
	if err := exchange(h, good("unknown")); !errors.Is(err, auth.ErrInvalidGrant) {
		t.Errorf("unknown code = %v", err)
	}
	if err := exchange(h, good(code)); err != nil {
		t.Fatalf("first exchange = %v", err)
	}
	if err := exchange(h, good(code)); !errors.Is(err, auth.ErrInvalidGrant) {
		t.Errorf("second exchange = %v", err)
	}

	for name, change := range map[string]func(*auth.CLIExchange){
		"other redirect":        func(e *auth.CLIExchange) { e.RedirectURI = "http://127.0.0.1:49153/callback" },
		"no redirect":           func(e *auth.CLIExchange) { e.RedirectURI = "" },
		"verifier of another":   func(e *auth.CLIExchange) { e.CodeVerifier = strings.Repeat("a", 43) },
		"short verifier":        func(e *auth.CLIExchange) { e.CodeVerifier = testVerifier[:42] },
		"long verifier":         func(e *auth.CLIExchange) { e.CodeVerifier = strings.Repeat("a", 129) },
		"verifier with a space": func(e *auth.CLIExchange) { e.CodeVerifier = testVerifier[:42] + " " },
	} {
		h := newHarness(t, nil)
		_, code := approved(t, h, cliRequest())
		ex := good(code)
		change(&ex)
		if err := exchange(h, ex); !errors.Is(err, auth.ErrInvalidGrant) {
			t.Errorf("%s = %v, want ErrInvalidGrant", name, err)
		}
		if err := exchange(h, good(code)); !errors.Is(err, auth.ErrInvalidGrant) {
			t.Errorf("%s, then the right exchange = %v, want the code spent", name, err)
		}
	}

	h = newHarness(t, nil)
	_, code = approved(t, h, cliRequest())
	h.clock.Advance(5 * time.Minute)
	if err := exchange(h, good(code)); !errors.Is(err, auth.ErrInvalidGrant) {
		t.Errorf("exchange at expiry = %v", err)
	}

	h = newHarness(t, nil)
	principal, code := approved(t, h, cliRequest())
	disabled := h.store.users[principal.User.ID]
	disabled.Disabled = true
	h.store.users[principal.User.ID] = disabled
	if err := exchange(h, good(code)); !errors.Is(err, auth.ErrInvalidGrant) {
		t.Errorf("exchange for a disabled account = %v", err)
	}

	h = newHarness(t, nil)
	principal, code = approved(t, h, cliRequest())
	delete(h.store.users, principal.User.ID)
	if err := exchange(h, good(code)); !errors.Is(err, auth.ErrInvalidGrant) {
		t.Errorf("exchange for a deleted account = %v", err)
	}
}

// TestCLISessionLifetimes verifies that each kind of session idles out on its own time and that the purge follows it.
//
// Scenarios:
//   - after 2 hours unused a terminal session still authenticates while a browser session is gone
//   - a terminal session unused for 7 days is gone; one used every 6 days ends after 30 days anyway
//   - the purge removes terminal sessions unused for 7 days and keeps the others
func TestCLISessionLifetimes(t *testing.T) {
	ctx := context.Background()
	newTerminal := func(h *harness, user auth.User) string {
		code := approve(t, h, user, cliRequest())
		token, _, err := h.svc.ExchangeCLI(ctx, auth.CLIExchange{Code: code, CodeVerifier: testVerifier, RedirectURI: testRedirect}, terminal)
		if err != nil {
			t.Fatalf("ExchangeCLI() = %v", err)
		}
		return token
	}

	h := newHarness(t, nil)
	browserToken, principal := signedIn(t, h)
	cliToken := newTerminal(h, principal.User)
	h.clock.Advance(2 * time.Hour)
	if _, err := h.svc.Authenticate(ctx, cliToken); err != nil {
		t.Errorf("terminal session after 2 idle hours = %v, want alive", err)
	}
	if _, err := h.svc.Authenticate(ctx, browserToken); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("browser session after 2 idle hours = %v, want ErrUnauthenticated", err)
	}
	h.clock.Advance(7 * 24 * time.Hour)
	if _, err := h.svc.Authenticate(ctx, cliToken); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("terminal session after 7 idle days = %v, want ErrUnauthenticated", err)
	}

	h = newHarness(t, nil)
	_, principal = signedIn(t, h)
	cliToken = newTerminal(h, principal.User)
	for range 4 {
		h.clock.Advance(6 * 24 * time.Hour)
		if _, err := h.svc.Authenticate(ctx, cliToken); err != nil {
			t.Fatalf("terminal session used every 6 days = %v, want alive", err)
		}
	}
	h.clock.Advance(6 * 24 * time.Hour)
	if _, err := h.svc.Authenticate(ctx, cliToken); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("terminal session after 30 days = %v, want ErrUnauthenticated", err)
	}

	h = newHarness(t, nil)
	_, principal = signedIn(t, h)
	stale := newTerminal(h, principal.User)
	h.clock.Advance(7*24*time.Hour + time.Minute)
	fresh := newTerminal(h, principal.User)
	if n, err := h.svc.PurgeExpired(ctx); err != nil || n != 2 {
		t.Errorf("PurgeExpired() = %d, %v, want the stale terminal session and the idle browser session", n, err)
	}
	if _, err := h.svc.Authenticate(ctx, fresh); err != nil {
		t.Errorf("fresh terminal session after the purge = %v", err)
	}
	if _, err := h.svc.Authenticate(ctx, stale); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("stale terminal session after the purge = %v", err)
	}
}

// TestCLISessionManagement verifies listing and revoking terminal sessions, purging approvals and turning the sign-in off.
//
// Scenarios:
//   - a user lists their terminal sessions only, not their browser sessions
//   - revoking ends the session; revoking it again, or a session of another user, is not found
//   - the purge of approvals removes expired ones only
//   - with the sign-in turned off, approving and exchanging are refused
func TestCLISessionManagement(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	principal, code := approved(t, h, cliRequest())
	token, session, err := h.svc.ExchangeCLI(ctx, auth.CLIExchange{Code: code, CodeVerifier: testVerifier, RedirectURI: testRedirect}, terminal)
	if err != nil {
		t.Fatal(err)
	}
	list, err := h.svc.CLISessions(ctx, principal.User.ID)
	if err != nil || len(list) != 1 || list[0].ID != session.Session.ID {
		t.Errorf("CLISessions() = %+v, %v, want the terminal session only", list, err)
	}
	if err := h.svc.RevokeSession(ctx, uuid.New(), session.Session.ID); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("RevokeSession(another user) = %v, want ErrNotFound", err)
	}
	if err := h.svc.RevokeSession(ctx, principal.User.ID, session.Session.ID); err != nil {
		t.Errorf("RevokeSession() = %v", err)
	}
	if _, err := h.svc.Authenticate(ctx, token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("Authenticate(revoked) = %v", err)
	}
	if err := h.svc.RevokeSession(ctx, principal.User.ID, session.Session.ID); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("RevokeSession() again = %v, want ErrNotFound", err)
	}

	if _, err := h.svc.ApproveCLI(ctx, principal.User, testPassword, cliRequest(), auth.ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	if n, err := h.svc.PurgeCLIAuthorizations(ctx); err != nil || n != 0 {
		t.Errorf("PurgeCLIAuthorizations(before expiry) = %d, %v, want 0", n, err)
	}
	h.clock.Advance(5 * time.Minute)
	if n, err := h.svc.PurgeCLIAuthorizations(ctx); err != nil || n != 2 {
		t.Errorf("PurgeCLIAuthorizations(at expiry) = %d, %v, want 2", n, err)
	}

	store := newFakeStore()
	off := auth.NewService(store, store, store, h.hasher, h.clock, rand.Reader, auth.Config{IdleTimeout: time.Hour, AbsoluteTimeout: time.Hour})
	if _, err := off.ApproveCLI(ctx, principal.User, testPassword, cliRequest(), auth.ClientInfo{}); !errors.Is(err, auth.ErrCLILoginDisabled) {
		t.Errorf("ApproveCLI(turned off) = %v", err)
	}
	if _, _, err := off.ExchangeCLI(ctx, auth.CLIExchange{}, terminal); !errors.Is(err, auth.ErrCLILoginDisabled) {
		t.Errorf("ExchangeCLI(turned off) = %v", err)
	}
}

// TestCLIFailures verifies that failures of the stores, the identifier generator and the entropy source reach the caller.
//
// Scenarios:
//   - approving: storing the approval, generating its id, reading its code, hashing the password
//   - exchanging: using the approval, reading the account, storing the session
//   - listing, revoking and purging
func TestCLIFailures(t *testing.T) {
	ctx := context.Background()
	good := func(code string) auth.CLIExchange {
		return auth.CLIExchange{Code: code, CodeVerifier: testVerifier, RedirectURI: testRedirect}
	}

	h := newHarness(t, nil)
	_, principal := signedIn(t, h)
	for _, method := range []string{"UseCLIAuthorization", "UserByID", "CreateSession"} {
		h := newHarness(t, nil)
		_, code := approved(t, h, cliRequest())
		h.store.failing(method)
		if _, _, err := h.svc.ExchangeCLI(ctx, good(code), terminal); !errors.Is(err, errBoom) {
			t.Errorf("ExchangeCLI() with a failing %s = %v, want the store error", method, err)
		}
	}
	h.store.failing("CreateCLIAuthorization").failing("ListUserSessions").failing("DeleteUserSession").failing("DeleteExpiredCLIAuthorizations")
	if _, err := h.svc.ApproveCLI(ctx, principal.User, testPassword, cliRequest(), auth.ClientInfo{}); !errors.Is(err, errBoom) {
		t.Errorf("ApproveCLI() with a failing store = %v", err)
	}
	if _, err := h.svc.CLISessions(ctx, principal.User.ID); !errors.Is(err, errBoom) {
		t.Errorf("CLISessions() with a failing store = %v", err)
	}
	if err := h.svc.RevokeSession(ctx, principal.User.ID, uuid.New()); !errors.Is(err, errBoom) {
		t.Errorf("RevokeSession() with a failing store = %v", err)
	}
	if _, err := h.svc.PurgeCLIAuthorizations(ctx); !errors.Is(err, errBoom) {
		t.Errorf("PurgeCLIAuthorizations() with a failing store = %v", err)
	}
	h = newHarness(t, nil)
	_, principal = signedIn(t, h)
	h.svc.SetIDGenerator(func() (uuid.UUID, error) { return uuid.Nil, errBoom })
	if _, err := h.svc.ApproveCLI(ctx, principal.User, testPassword, cliRequest(), auth.ClientInfo{}); !errors.Is(err, errBoom) {
		t.Errorf("ApproveCLI() with a failing id generator = %v", err)
	}

	store := newFakeStore()
	config := auth.Config{IdleTimeout: time.Hour, AbsoluteTimeout: time.Hour, CLILogin: true}
	noEntropy := auth.NewService(store, store, store, h.hasher, h.clock, failingReader{}, config)
	if _, err := noEntropy.ApproveCLI(ctx, principal.User, testPassword, cliRequest(), auth.ClientInfo{}); !errors.Is(err, errBoom) {
		t.Errorf("ApproveCLI() without entropy = %v, want the entropy error", err)
	}
	brokenHash := auth.NewService(store, store, store, auth.NewHasher(vault.NewKDF(1), fastParams, rand.Reader), h.clock, rand.Reader, config)
	if _, err := brokenHash.ApproveCLI(ctx, auth.User{PasswordHash: "not a hash"}, testPassword, cliRequest(), auth.ClientInfo{}); !errors.Is(err, auth.ErrMalformedHash) {
		t.Errorf("ApproveCLI() with a malformed stored hash = %v, want ErrMalformedHash", err)
	}
}
