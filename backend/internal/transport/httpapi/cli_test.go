package httpapi_test

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/vault"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// cliVerifier is the PKCE verifier the tests use.
const cliVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

// cliRedirect is the loopback callback the tests use.
const cliRedirect = "http://127.0.0.1:49152/callback"

// cliChallenge is the S256 challenge of cliVerifier.
func cliChallenge() string {
	sum := sha256.Sum256([]byte(cliVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// approval is the body of a valid approval with a loopback redirect, changed by the given function.
func approval(change func(map[string]string)) map[string]string {
	body := map[string]string{"codeChallenge": cliChallenge(), "redirectUri": cliRedirect, "clientName": "transferly-tui", "clientHost": "laptop", "password": adminPassword}
	if change != nil {
		change(body)
	}
	return body
}

// approveCli approves a sign-in of the terminal client from the browser session and returns the reply.
func (a *api) approveCli(cookie *http.Cookie, csrf string, body map[string]string) reply {
	a.t.Helper()
	return a.do(request{method: http.MethodPost, path: "/api/v1/cli/authorizations", cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf}, body: body})
}

// exchangeCli exchanges a code with the test verifier and redirect and returns the reply.
func (a *api) exchangeCli(code, verifier string) reply {
	a.t.Helper()
	return a.do(request{method: http.MethodPost, path: "/api/v1/cli/token", body: map[string]string{"code": code, "codeVerifier": verifier, "redirectUri": cliRedirect}})
}

// cliToken approves and exchanges a sign-in and returns the bearer token.
func (a *api) cliToken(cookie *http.Cookie, csrf string) httpapi.CliToken {
	a.t.Helper()
	approved := a.approveCli(cookie, csrf, approval(nil))
	var code httpapi.CliApprovalCode
	if approved.Status != http.StatusCreated || json.Unmarshal(approved.Body, &code) != nil {
		a.t.Fatalf("approve = %d %s", approved.Status, approved.Body)
	}
	exchanged := a.exchangeCli(code.Code, cliVerifier)
	var token httpapi.CliToken
	if exchanged.Status != http.StatusOK || json.Unmarshal(exchanged.Body, &token) != nil {
		a.t.Fatalf("exchange = %d %s", exchanged.Status, exchanged.Body)
	}
	return token
}

// bearer returns the headers that carry a bearer token.
func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// TestCLISignIn verifies the approval in the browser and the exchange for a session of the terminal client (ADR-0029).
//
// Scenarios:
//   - approving needs a browser session, the anti-CSRF token and the password; a malformed redirect or request is refused
//   - a wrong verifier spends the code; the right one afterwards is invalid_grant too
//   - a good exchange answers the token, its latest end and the account, and sets no cookie
func TestCLISignIn(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()

	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/cli/authorizations", body: approval(nil)}), 401, "unauthenticated")
	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/cli/authorizations", cookie: cookie, body: approval(nil)}), 403, "csrf_invalid")
	expectProblem(t, a.approveCli(cookie, csrf, approval(func(b map[string]string) { b["password"] = "wrong password!!" })), 403, "invalid_password")
	expectProblem(t, a.approveCli(cookie, csrf, approval(func(b map[string]string) { b["redirectUri"] = "http://localhost:8000/callback" })), 400, "invalid_redirect")
	expectProblem(t, a.approveCli(cookie, csrf, approval(func(b map[string]string) { b["codeChallenge"] = "short" })), 400, "invalid_cli_request")

	approved := a.approveCli(cookie, csrf, approval(nil))
	var code httpapi.CliApprovalCode
	mustDecode(t, approved.Body, &code)
	if approved.Status != http.StatusCreated || len(code.Code) < 40 {
		t.Fatalf("approve = %d %s", approved.Status, approved.Body)
	}
	expectProblem(t, a.exchangeCli(code.Code, "wrong-verifier-wrong-verifier-wrong-verifier"), 400, "invalid_grant")
	expectProblem(t, a.exchangeCli(code.Code, cliVerifier), 400, "invalid_grant")

	token := a.cliToken(cookie, csrf)
	if token.Token == "" || token.User.Email != adminEmail || !token.ExpiresAt.Equal(a.clock.Now().Add(30*24*time.Hour)) {
		t.Errorf("token = %+v, want a token of the admin that lasts 30 days", token)
	}
	approved = a.approveCli(cookie, csrf, approval(nil))
	mustDecode(t, approved.Body, &code)
	if exchanged := a.exchangeCli(code.Code, cliVerifier); len(exchanged.Cookies) != 0 {
		t.Errorf("exchange set cookies %v, want none", exchanged.Cookies)
	}
}

// TestCLIBearerSessions verifies how the token of the terminal client is carried and what it may do.
//
// Scenarios:
//   - the bearer token signs requests in, with any capitalization of the scheme, and needs no anti-CSRF token to change state
//   - the terminal token as a cookie and the browser cookie as a bearer token are anonymous
//   - a request with both credentials is ambiguous_credentials; another Authorization scheme is ignored
//   - a terminal session cannot approve another sign-in
func TestCLIBearerSessions(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	token := a.cliToken(cookie, csrf).Token

	for _, scheme := range []string{"Bearer ", "bearer "} {
		if me := a.do(request{method: http.MethodGet, path: "/api/v1/me", headers: map[string]string{"Authorization": scheme + token}}); me.Status != 200 || me.session(t).User.Email != adminEmail {
			t.Errorf("me with %q = %d %s", scheme, me.Status, me.Body)
		}
	}
	if changed := a.do(request{method: http.MethodPatch, path: "/api/v1/me", headers: bearer(token), body: map[string]string{"theme": "nord"}}); changed.Status != 200 {
		t.Errorf("PATCH me with a bearer token and no anti-CSRF token = %d %s, want 200", changed.Status, changed.Body)
	}

	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: &http.Cookie{Name: "transferly_session", Value: token}}), 401, "unauthenticated")
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me", headers: bearer(cookie.Value)}), 401, "unauthenticated")
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie, headers: bearer(token)}), 400, "ambiguous_credentials")
	if basic := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie, headers: map[string]string{"Authorization": "Basic dTpw"}}); basic.Status != 200 {
		t.Errorf("me with the cookie and a Basic header = %d, want the cookie to count", basic.Status)
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me", headers: map[string]string{"Authorization": "Basic dTpw"}}), 401, "unauthenticated")

	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/cli/authorizations", headers: bearer(token), body: approval(nil)}), 403, "browser_session_required")
}

// TestCLISessionsList verifies listing and revoking terminal sessions.
//
// Scenarios:
//   - the list holds the terminal sessions only, with name, host, address and agent, and marks the one asking as current
//   - revoking an unknown session is session_not_found; revoking from the browser ends the terminal session at once
func TestCLISessionsList(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	token := a.cliToken(cookie, csrf).Token

	var fromTerminal, fromBrowser []httpapi.CliSession
	mustDecode(t, a.do(request{method: http.MethodGet, path: "/api/v1/me/sessions", headers: bearer(token)}).Body, &fromTerminal)
	mustDecode(t, a.do(request{method: http.MethodGet, path: "/api/v1/me/sessions", cookie: cookie}).Body, &fromBrowser)
	if len(fromTerminal) != 1 || !fromTerminal[0].Current || fromTerminal[0].Name != "transferly-tui" || fromTerminal[0].Host != "laptop" || fromTerminal[0].Ip == "" {
		t.Errorf("sessions from the terminal = %+v", fromTerminal)
	}
	if len(fromBrowser) != 1 || fromBrowser[0].Current {
		t.Errorf("sessions from the browser = %+v, want the terminal session, not current", fromBrowser)
	}

	revoke := func(id string) reply {
		return a.do(request{method: http.MethodDelete, path: "/api/v1/me/sessions/" + id, cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf}})
	}
	expectProblem(t, revoke(uuid.NewString()), 404, "session_not_found")
	if r := revoke(fromBrowser[0].Id.String()); r.Status != 204 {
		t.Fatalf("revoke = %d %s", r.Status, r.Body)
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me", headers: bearer(token)}), 401, "unauthenticated")
}

// TestCLILimitsAndSwitch verifies the rate limits and the switch of the sign-in of the terminal client.
//
// Scenarios:
//   - exchanges are limited per client address and approvals share the limit of password confirmations
//   - with TRANSFERLY_CLI_LOGIN off both are cli_login_disabled
func TestCLILimitsAndSwitch(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	var last reply
	for range 6 {
		last = a.exchangeCli("unknown", cliVerifier)
	}
	expectProblem(t, last, 429, "rate_limited")
	for range 7 {
		last = a.approveCli(cookie, csrf, approval(func(b map[string]string) { b["password"] = "wrong password!!" }))
	}
	expectProblem(t, last, 429, "rate_limited")

	off := newAPI(t, func(d *httpapi.Deps) {
		store := d.Readiness.(*postgres.Store)
		hasher := auth.NewHasher(vault.NewKDF(2), vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}, rand.Reader)
		d.Auth = auth.NewService(store, store, store, hasher, realClock{}, rand.Reader, auth.Config{IdleTimeout: time.Hour, AbsoluteTimeout: time.Hour})
	})
	cookie, csrf = off.setup()
	expectProblem(t, off.approveCli(cookie, csrf, approval(nil)), 403, "cli_login_disabled")
	expectProblem(t, off.exchangeCli("any", cliVerifier), 403, "cli_login_disabled")
}

// TestCLIFailures verifies that failures of the store are 500 problems without details.
func TestCLIFailures(t *testing.T) {
	t.Parallel()
	b := newAPI(t, withFlaky(func(f *flakyStore) { f.failCLI = true }))
	cookie, csrf := b.setup()
	for name, r := range map[string]reply{
		"approve":  b.approveCli(cookie, csrf, approval(nil)),
		"exchange": b.exchangeCli("any", cliVerifier),
		"list":     b.do(request{method: http.MethodGet, path: "/api/v1/me/sessions", cookie: cookie}),
		"revoke":   b.do(request{method: http.MethodDelete, path: "/api/v1/me/sessions/" + uuid.NewString(), cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf}}),
	} {
		if r.Status != http.StatusInternalServerError {
			t.Errorf("%s with a failing store = %d %s, want 500", name, r.Status, r.Body)
		}
	}
}
