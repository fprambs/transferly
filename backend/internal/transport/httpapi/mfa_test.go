package httpapi_test

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/core/mfa"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// mfaUser is an account with its session, for the second factor tests.
type mfaUser struct {
	a      *api
	email  string
	id     uuid.UUID
	cookie *http.Cookie
	csrf   string
	now    func() time.Time
}

// newMFAUser registers an account and signs it in.
func newMFAUser(t *testing.T, tweaks ...func(*httpapi.Deps)) *mfaUser {
	t.Helper()
	a := newAPI(t, tweaks...)
	cookie, user := a.signIn("mfa@example.com")
	return &mfaUser{a: a, email: user.Email, id: user.ID, cookie: cookie, csrf: csrf(a, cookie), now: a.clock.Now}
}

// call sends a call of the signed-in account.
func (u *mfaUser) call(method, path string, body any) reply {
	u.a.t.Helper()
	return u.a.send(method, path, body, u.cookie, u.csrf)
}

// enroll enables the authenticator app and returns its secret and the recovery codes.
func (u *mfaUser) enroll(t *testing.T) ([]byte, []string) {
	t.Helper()
	begin := u.call(http.MethodPost, "/api/v1/me/totp", nil)
	var setup httpapi.TotpSetup
	mustDecode(t, begin.Body, &setup)
	if begin.Status != http.StatusOK || setup.Uri == "" {
		t.Fatalf("begin = %d %s", begin.Status, begin.Body)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Secret)
	if err != nil {
		t.Fatal(err)
	}
	confirm := u.call(http.MethodPost, "/api/v1/me/totp/confirm", map[string]string{"code": mfa.Code(secret, u.now())})
	var codes httpapi.RecoveryCodes
	mustDecode(t, confirm.Body, &codes)
	if confirm.Status != http.StatusOK || len(codes.Codes) != 10 {
		t.Fatalf("confirm = %d %s", confirm.Status, confirm.Body)
	}
	return secret, codes.Codes
}

// login runs the password step and returns the reply.
func (u *mfaUser) login() reply {
	return u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": u.email, "password": adminPassword}})
}

// challenge runs the password step and returns the challenge of the reply.
func (u *mfaUser) challenge(t *testing.T) string {
	t.Helper()
	r := u.login()
	var c httpapi.MfaChallenge
	mustDecode(t, r.Body, &c)
	if r.Status != http.StatusAccepted || c.Challenge == "" || r.sessionCookie() != nil {
		t.Fatalf("login = %d %s; want a challenge and no session", r.Status, r.Body)
	}
	if len(c.Methods) != 2 {
		t.Fatalf("methods = %v", c.Methods)
	}
	return c.Challenge
}

// freshChallenge makes a challenge without the password step, so a test can try many codes without spending sign-in attempts.
func (u *mfaUser) freshChallenge() string {
	return mfa.NewService(nil, instanceKey, rand.Reader, u.a.clock).Challenge(u.id)
}

// verify sends the second step.
func (u *mfaUser) verify(challenge, code string) reply {
	return u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/mfa", body: map[string]string{"challenge": challenge, "code": code}})
}

// TestMFA_Lifecycle verifies enrolling, signing in with and removing the second factor.
//
// Scenarios:
//   - a new account has no second factor; beginning does not enable it, a wrong or early code is refused and a right one enables it and
//     gives ten recovery codes
//   - signing in then answers 202 with a challenge and no session, and the code opens the session; the same code twice, a wrong one and a
//     forged or expired challenge are refused, and the next code works after 30 seconds
//   - a recovery code works once, and new recovery codes replace the old ones
//   - disabling and replacing recovery codes need the password again, and after disabling signing in gives a session at once
func TestMFA_Lifecycle(t *testing.T) {
	t.Parallel()
	u := newMFAUser(t)
	status := func() httpapi.SecurityStatus {
		var s httpapi.SecurityStatus
		mustDecode(t, u.a.do(request{method: http.MethodGet, path: "/api/v1/me/security", cookie: u.cookie}).Body, &s)
		return s
	}
	if s := status(); s.Totp || s.RecoveryCodesLeft != 0 {
		t.Fatalf("status = %+v; want nothing enabled", s)
	}
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/totp/confirm", map[string]string{"code": "123456"}), 409, "totp_not_started")
	u.call(http.MethodPost, "/api/v1/me/totp", nil)
	if s := status(); s.Totp {
		t.Fatal("beginning enabled the factor")
	}
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/totp/confirm", map[string]string{"code": "000000"}), 422, "invalid_code")
	secret, recovery := u.enroll(t)
	if s := status(); !s.Totp || s.RecoveryCodesLeft != 10 {
		t.Fatalf("status = %+v; want the factor and ten codes", s)
	}

	challenge := u.challenge(t)
	u.a.clock.Advance(mfa.Period * time.Second)
	code := mfa.Code(secret, u.a.clock.Now())
	expectProblem(t, u.verify(challenge, "000000"), 401, "invalid_code")
	ok := u.verify(challenge, code)
	if ok.Status != http.StatusOK || ok.sessionCookie() == nil {
		t.Fatalf("verify = %d %s", ok.Status, ok.Body)
	}
	if me := u.a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: ok.sessionCookie()}); me.Status != http.StatusOK {
		t.Errorf("session after the second step = %d", me.Status)
	}
	expectProblem(t, u.verify(challenge, code), 401, "invalid_code")
	expectProblem(t, u.verify("forged", code), 401, "invalid_challenge")
	u.a.clock.Advance(mfa.Period * time.Second)
	if r := u.verify(challenge, mfa.Code(secret, u.a.clock.Now())); r.Status != http.StatusOK {
		t.Errorf("the next code = %d %s", r.Status, r.Body)
	}
	u.a.clock.Advance(mfa.ChallengeTTL)
	expectProblem(t, u.verify(challenge, mfa.Code(secret, u.a.clock.Now())), 401, "invalid_challenge")

	fresh := u.challenge(t)
	if r := u.verify(fresh, recovery[0]); r.Status != http.StatusOK {
		t.Fatalf("a recovery code = %d %s", r.Status, r.Body)
	}
	expectProblem(t, u.verify(fresh, recovery[0]), 401, "invalid_code")
	if s := status(); s.RecoveryCodesLeft != 9 {
		t.Errorf("codes left = %d; want 9", s.RecoveryCodesLeft)
	}
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/recovery-codes", map[string]string{"password": "not the password!!"}), 403, "invalid_password")
	regenerated := u.call(http.MethodPost, "/api/v1/me/recovery-codes", map[string]string{"password": adminPassword})
	var codes httpapi.RecoveryCodes
	mustDecode(t, regenerated.Body, &codes)
	if regenerated.Status != http.StatusOK || len(codes.Codes) != 10 || codes.Codes[0] == recovery[1] {
		t.Fatalf("regenerated = %d %s", regenerated.Status, regenerated.Body)
	}
	expectProblem(t, u.verify(u.challenge(t), recovery[1]), 401, "invalid_code")

	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/totp/disable", map[string]string{"password": "not the password!!"}), 403, "invalid_password")
	if r := u.call(http.MethodPost, "/api/v1/me/totp/disable", map[string]string{"password": adminPassword}); r.Status != http.StatusNoContent {
		t.Fatalf("disable = %d %s", r.Status, r.Body)
	}
	if r := u.login(); r.Status != http.StatusOK || r.sessionCookie() == nil {
		t.Errorf("login after disabling = %d %s; want a session", r.Status, r.Body)
	}
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/recovery-codes", map[string]string{"password": adminPassword}), 409, "totp_not_enabled")
}

// TestMFA_Protection verifies who may call the endpoints and how attempts are limited.
//
// Scenarios:
//   - anonymous callers get 401 on the account endpoints, writers without the anti-CSRF token get 403 csrf_invalid, and the second step
//     needs no session but refuses a foreign origin
//   - too many wrong codes from one address, and from one account, are rate limited, and the challenge of a disabled account is refused
func TestMFA_Protection(t *testing.T) {
	t.Parallel()
	u := newMFAUser(t)
	for _, path := range []string{"/api/v1/me/totp", "/api/v1/me/totp/confirm", "/api/v1/me/totp/disable", "/api/v1/me/recovery-codes"} {
		expectProblem(t, u.a.do(request{method: http.MethodPost, path: path, body: map[string]string{"code": "1", "password": "x"}}), 401, "unauthenticated")
		expectProblem(t, u.a.do(request{method: http.MethodPost, path: path, body: map[string]string{"code": "1", "password": "x"}, cookie: u.cookie}), 403, "csrf_invalid")
	}
	expectProblem(t, u.a.do(request{method: http.MethodGet, path: "/api/v1/me/security"}), 401, "unauthenticated")
	foreign := u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/mfa", body: map[string]string{"challenge": "x", "code": "1"}, headers: map[string]string{"Origin": "https://evil.example"}})
	expectProblem(t, foreign, 403, "cross_site_request")

	secret, _ := u.enroll(t)
	for range 5 {
		expectProblem(t, u.verify(u.freshChallenge(), "000000"), 401, "invalid_code")
	}
	expectProblem(t, u.verify(u.freshChallenge(), mfa.Code(secret, u.a.clock.Now())), 429, "rate_limited")
	expectProblem(t, u.verify("anything", "000000"), 429, "rate_limited")

	other := newMFAUser(t)
	otherSecret, _ := other.enroll(t)
	challenge := other.challenge(t)
	if _, err := other.a.store.DB().Exec(context.Background(), "UPDATE users SET status = 'disabled' WHERE id = $1", other.id); err != nil {
		t.Fatal(err)
	}
	other.a.clock.Advance(mfa.Period * time.Second)
	expectProblem(t, other.verify(challenge, mfa.Code(otherSecret, other.a.clock.Now())), 403, "user_disabled")
}

// brokenFactors is a store whose second factor operations fail on demand.
type brokenFactors struct {
	*postgres.Store
	fail map[string]bool
}

// Factor fails when configured to.
func (b brokenFactors) Factor(ctx context.Context, id uuid.UUID) (mfa.Factor, error) {
	if b.fail["factor"] {
		return mfa.Factor{}, errInjected
	}
	return b.Store.Factor(ctx, id)
}

// SaveFactor fails when configured to.
func (b brokenFactors) SaveFactor(ctx context.Context, id uuid.UUID, sealed []byte) error {
	if b.fail["save"] {
		return errInjected
	}
	return b.Store.SaveFactor(ctx, id, sealed)
}

// RecoveryLeft fails when configured to.
func (b brokenFactors) RecoveryLeft(ctx context.Context, id uuid.UUID) (int, error) {
	if b.fail["left"] {
		return 0, errInjected
	}
	return b.Store.RecoveryLeft(ctx, id)
}

// DeleteFactor fails when configured to.
func (b brokenFactors) DeleteFactor(ctx context.Context, id uuid.UUID) error {
	if b.fail["delete"] {
		return errInjected
	}
	return b.Store.DeleteFactor(ctx, id)
}

// ReplaceRecovery fails when configured to.
func (b brokenFactors) ReplaceRecovery(ctx context.Context, id uuid.UUID, hashes [][]byte) error {
	if b.fail["replace"] {
		return errInjected
	}
	return b.Store.ReplaceRecovery(ctx, id, hashes)
}

// TestMFA_Failures verifies that store failures inside the second factor handlers are 500 problems.
//
// Scenarios:
//   - reading the status, beginning, confirming, disabling, replacing the codes, signing in and finishing the second step each answer 500 when the store fails
func TestMFA_Failures(t *testing.T) {
	t.Parallel()
	fail := map[string]bool{}
	u := newMFAUser(t, func(d *httpapi.Deps) {
		d.MFA = mfa.NewService(brokenFactors{d.Readiness.(*postgres.Store), fail}, instanceKey, rand.Reader, realClock{})
	})
	u.now = time.Now
	u.enroll(t)
	challenge := mfa.NewService(nil, instanceKey, rand.Reader, realClock{}).Challenge(u.id)

	fail["factor"] = true
	expectProblem(t, u.a.do(request{method: http.MethodGet, path: "/api/v1/me/security", cookie: u.cookie}), 500, "internal_error")
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/totp/confirm", map[string]string{"code": "123456"}), 500, "internal_error")
	expectProblem(t, u.login(), 500, "internal_error")
	expectProblem(t, u.verify(challenge, "123456"), 500, "internal_error")
	fail["factor"] = false
	fail["left"] = true
	expectProblem(t, u.a.do(request{method: http.MethodGet, path: "/api/v1/me/security", cookie: u.cookie}), 500, "internal_error")
	fail["left"] = false
	fail["save"] = true
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/totp", nil), 500, "internal_error")
	fail["save"] = false
	fail["delete"] = true
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/totp/disable", map[string]string{"password": adminPassword}), 500, "internal_error")
	fail["delete"] = false
	fail["replace"] = true
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/recovery-codes", map[string]string{"password": adminPassword}), 500, "internal_error")
}

// TestMFA_SecondStepFailures verifies the failures after a right code, and the limits of the sensitive changes.
//
// Scenarios:
//   - a right code followed by a failing account lookup, personal workspace or session is a 500 problem
//   - too many confirmations, and too many password attempts to disable or replace codes, are rate limited
//   - a password that cannot be checked is a 500 problem
func TestMFA_SecondStepFailures(t *testing.T) {
	t.Parallel()
	var flaky *flakyStore
	var workspaces *failingWorkspaces
	u := newMFAUser(t, withFailingWorkspaces(func(f *failingWorkspaces) { workspaces = f }), func(d *httpapi.Deps) {
		withFlaky(func(f *flakyStore) { flaky = f })(d)
		d.MFA = mfa.NewService(d.Readiness.(*postgres.Store), instanceKey, rand.Reader, realClock{})
	})
	u.now = time.Now
	_, recovery := u.enroll(t)
	challenge := func() string { return mfa.NewService(nil, instanceKey, rand.Reader, realClock{}).Challenge(u.id) }

	flaky.failUserByID = true
	expectProblem(t, u.verify(challenge(), recovery[0]), 500, "internal_error")
	flaky.failUserByID = false
	workspaces.failPersonal = true
	expectProblem(t, u.verify(challenge(), recovery[1]), 500, "internal_error")
	workspaces.failPersonal = false
	flaky.failCreateSession = true
	expectProblem(t, u.verify(challenge(), recovery[2]), 500, "internal_error")
	flaky.failCreateSession = false

	for range 2 {
		expectProblem(t, u.call(http.MethodPost, "/api/v1/me/totp/confirm", map[string]string{"code": "000000"}), 409, "totp_not_started")
	}
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/totp/confirm", map[string]string{"code": "000000"}), 429, "rate_limited")

	guesser := newMFAUser(t)
	guesser.enroll(t)
	for range 6 {
		expectProblem(t, guesser.call(http.MethodPost, "/api/v1/me/totp/disable", map[string]string{"password": "not the password!!"}), 403, "invalid_password")
	}
	expectProblem(t, guesser.call(http.MethodPost, "/api/v1/me/totp/disable", map[string]string{"password": adminPassword}), 429, "rate_limited")
	expectProblem(t, guesser.call(http.MethodPost, "/api/v1/me/recovery-codes", map[string]string{"password": adminPassword}), 429, "rate_limited")

	broken := newMFAUser(t)
	broken.enroll(t)
	if _, err := broken.a.store.DB().Exec(context.Background(), "UPDATE users SET password_hash = 'garbage' WHERE id = $1", broken.id); err != nil {
		t.Fatal(err)
	}
	expectProblem(t, broken.call(http.MethodPost, "/api/v1/me/totp/disable", map[string]string{"password": adminPassword}), 500, "internal_error")
	expectProblem(t, broken.call(http.MethodPost, "/api/v1/me/recovery-codes", map[string]string{"password": adminPassword}), 500, "internal_error")
}
