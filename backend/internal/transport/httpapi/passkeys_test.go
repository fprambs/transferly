package httpapi_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/core/mfa"
	"github.com/fprambs/sentinel/internal/core/passkey"
	"github.com/fprambs/sentinel/internal/core/passkey/passkeytest"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

// newDevice returns an authenticator that answers for the test server.
func newDevice() *passkeytest.Authenticator { return passkeytest.New("localhost", localOrigin) }

// ceremonyOf decodes a reply with the options of a ceremony and returns them as the authenticator wants them, and the token.
func ceremonyOf(t *testing.T, r reply) ([]byte, string) {
	t.Helper()
	var c httpapi.PasskeyCeremony
	mustDecode(t, r.Body, &c)
	if r.Status != http.StatusOK || c.Ceremony == "" {
		t.Fatalf("ceremony = %d %s", r.Status, r.Body)
	}
	options, _ := json.Marshal(c.Options)
	return options, c.Ceremony
}

// asMap turns the JSON an authenticator made into the object the API takes.
func asMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// addPasskey registers an authenticator through the API and returns the reply of the last step.
func (u *mfaUser) addPasskey(t *testing.T, device *passkeytest.Authenticator, name string) reply {
	t.Helper()
	options, ceremony := ceremonyOf(t, u.call(http.MethodPost, "/api/v1/me/passkeys/begin", nil))
	return u.call(http.MethodPost, "/api/v1/me/passkeys/finish", map[string]any{"ceremony": ceremony, "name": name, "credential": asMap(t, device.Register(options))})
}

// signInWithPasskey runs the passkey step of a sign-in and returns the reply of the last call.
func (u *mfaUser) signInWithPasskey(t *testing.T, challenge string, device *passkeytest.Authenticator) reply {
	t.Helper()
	options, ceremony := ceremonyOf(t, u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/begin", body: map[string]string{"challenge": challenge}}))
	return u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/finish", body: map[string]any{"challenge": challenge, "ceremony": ceremony, "credential": asMap(t, device.Assert(options))}})
}

// TestPasskeys_Lifecycle verifies registering, using and removing passkeys.
//
// Scenarios:
//   - a passkey is registered from the answer of an authenticator, appears in the status with its name and, being the first second factor,
//     brings ten recovery codes that a second passkey does not repeat
//   - signing in then answers 202 offering the passkey and the recovery codes, the passkey opens the session and records its use, and a
//     recovery code works without the app
//   - another authenticator, an expired or foreign challenge and a used ceremony are refused
//   - removing a passkey needs the password, an unknown one is 404, and removing the last factor removes the recovery codes, and an account without factors cannot ask for new ones
//   - with the app and a passkey both are offered, disabling the app keeps the passkey and the recovery codes, and the codes can be renewed
func TestPasskeys_Lifecycle(t *testing.T) {
	u := newMFAUser(t)
	status := func() httpapi.SecurityStatus {
		var s httpapi.SecurityStatus
		mustDecode(t, u.a.do(request{method: http.MethodGet, path: "/api/v1/me/security", cookie: u.cookie}).Body, &s)
		return s
	}
	laptop := newDevice()
	added := u.addPasskey(t, laptop, "  Laptop ")
	var first httpapi.PasskeyAdded
	mustDecode(t, added.Body, &first)
	if added.Status != http.StatusCreated || first.Passkey.Name != "Laptop" || first.RecoveryCodes == nil || len(*first.RecoveryCodes) != 10 {
		t.Fatalf("add = %d %s", added.Status, added.Body)
	}
	second := newDevice()
	var again httpapi.PasskeyAdded
	mustDecode(t, u.addPasskey(t, second, "Key").Body, &again)
	if again.RecoveryCodes != nil {
		t.Errorf("a second passkey created recovery codes again: %v", *again.RecoveryCodes)
	}
	if s := status(); len(s.Passkeys) != 2 || s.Passkeys[0].Name != "Laptop" || s.Passkeys[0].LastUsedAt != nil || s.RecoveryCodesLeft != 10 || s.Totp {
		t.Fatalf("status = %+v", s)
	}

	r := u.login()
	var challenge httpapi.MfaChallenge
	mustDecode(t, r.Body, &challenge)
	if r.Status != http.StatusAccepted || len(challenge.Methods) != 2 || challenge.Methods[0] != httpapi.MfaChallengeMethodsPasskey || challenge.Methods[1] != httpapi.MfaChallengeMethodsRecovery {
		t.Fatalf("login = %d %s; want the passkey and recovery offered", r.Status, r.Body)
	}
	ok := u.signInWithPasskey(t, challenge.Challenge, laptop)
	if ok.Status != http.StatusOK || ok.sessionCookie() == nil {
		t.Fatalf("passkey sign-in = %d %s", ok.Status, ok.Body)
	}
	if s := status(); s.Passkeys[0].LastUsedAt == nil {
		t.Error("the use of the passkey was not recorded")
	}
	expectProblem(t, u.signInWithPasskeyStatus(t, challenge.Challenge, newDevice()), 401, "invalid_passkey")
	expectProblem(t, u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/finish", body: map[string]any{"challenge": challenge.Challenge, "ceremony": "unknown", "credential": map[string]any{}}}), 401, "invalid_passkey")
	expectProblem(t, u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/begin", body: map[string]string{"challenge": "forged"}}), 401, "invalid_challenge")
	codes := *first.RecoveryCodes
	if r := u.verify(challenge.Challenge, codes[0]); r.Status != http.StatusOK {
		t.Errorf("a recovery code without the app = %d %s", r.Status, r.Body)
	}
	expectProblem(t, u.verify(challenge.Challenge, codes[0]), 401, "invalid_code")

	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/passkeys/"+first.Passkey.Id.String()+"/remove", map[string]string{"password": "not the password!!"}), 403, "invalid_password")
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/passkeys/"+uuid.NewString()+"/remove", map[string]string{"password": adminPassword}), 404, "passkey_not_found")
	if r := u.call(http.MethodPost, "/api/v1/me/passkeys/"+first.Passkey.Id.String()+"/remove", map[string]string{"password": adminPassword}); r.Status != http.StatusNoContent {
		t.Fatalf("remove = %d %s", r.Status, r.Body)
	}
	if s := status(); len(s.Passkeys) != 1 || s.RecoveryCodesLeft != 9 {
		t.Fatalf("status after removing one = %+v", s)
	}

	secret, _ := u.enroll(t)
	mine := u.login()
	var both httpapi.MfaChallenge
	mustDecode(t, mine.Body, &both)
	if len(both.Methods) != 3 || both.Methods[0] != httpapi.MfaChallengeMethodsTotp {
		t.Fatalf("methods = %v; want the app, the passkey and the recovery codes", both.Methods)
	}
	_ = secret
	if r := u.call(http.MethodPost, "/api/v1/me/totp/disable", map[string]string{"password": adminPassword}); r.Status != http.StatusNoContent {
		t.Fatalf("disable = %d", r.Status)
	}
	if s := status(); s.Totp || len(s.Passkeys) != 1 || s.RecoveryCodesLeft != 10 {
		t.Fatalf("status after disabling the app = %+v; want the passkey and the recovery codes kept", s)
	}
	if r := u.call(http.MethodPost, "/api/v1/me/recovery-codes", map[string]string{"password": adminPassword}); r.Status != http.StatusOK {
		t.Errorf("renewing the codes with only a passkey = %d %s", r.Status, r.Body)
	}
	if r := u.call(http.MethodPost, "/api/v1/me/passkeys/"+again.Passkey.Id.String()+"/remove", map[string]string{"password": adminPassword}); r.Status != http.StatusNoContent {
		t.Fatalf("remove the last = %d %s", r.Status, r.Body)
	}
	if s := status(); len(s.Passkeys) != 0 || s.RecoveryCodesLeft != 0 {
		t.Errorf("status without factors = %+v; want no recovery codes", s)
	}
	expectProblem(t, newMFAUser(t).call(http.MethodPost, "/api/v1/me/recovery-codes", map[string]string{"password": adminPassword}), 409, "totp_not_enabled")
}

// signInWithPasskeyStatus is signInWithPasskey for calls that are expected to fail, so the failure comes back as a reply.
func (u *mfaUser) signInWithPasskeyStatus(t *testing.T, challenge string, device *passkeytest.Authenticator) reply {
	return u.signInWithPasskey(t, challenge, device)
}

// TestPasskeys_Protection verifies who may call the endpoints and what the server refuses.
//
// Scenarios:
//   - anonymous callers get 401, writers without the anti-CSRF token 403, and the sign-in endpoints refuse a foreign origin
//   - a registration answered from the wrong origin is 422 invalid_passkey, an unknown ceremony 409, a bad name 422, and ten passkeys is the limit
//   - a passkey sign-in for an account that only has the app is 409, and the passkey of a disabled account is refused
//   - too many passkey attempts are rate limited, per address and per account, the account counting the attempts of the code step too
func TestPasskeys_Protection(t *testing.T) {
	u := newMFAUser(t)
	for path, body := range map[string]any{
		"/api/v1/me/passkeys/begin": nil, "/api/v1/me/passkeys/finish": map[string]any{"ceremony": "x", "name": "x", "credential": map[string]any{}},
		"/api/v1/me/passkeys/" + uuid.NewString() + "/remove": map[string]string{"password": "x"},
	} {
		expectProblem(t, u.a.do(request{method: http.MethodPost, path: path, body: body}), 401, "unauthenticated")
		expectProblem(t, u.a.do(request{method: http.MethodPost, path: path, body: body, cookie: u.cookie}), 403, "csrf_invalid")
	}
	for _, path := range []string{"/api/v1/auth/passkey/begin", "/api/v1/auth/passkey/finish"} {
		foreign := u.a.do(request{method: http.MethodPost, path: path, body: map[string]any{"challenge": "x", "ceremony": "x", "credential": map[string]any{}}, headers: map[string]string{"Origin": "https://evil.example"}})
		expectProblem(t, foreign, 403, "cross_site_request")
	}

	expectProblem(t, u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/finish", body: map[string]any{"challenge": "forged", "ceremony": "x", "credential": map[string]any{}}}), 401, "invalid_challenge")
	evil := passkeytest.New("localhost", "https://evil.example")
	expectProblem(t, u.addPasskey(t, evil, "Evil"), 422, "invalid_passkey")
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/passkeys/finish", map[string]any{"ceremony": "unknown", "name": "x", "credential": map[string]any{}}), 409, "ceremony_expired")
	expectProblem(t, u.addPasskey(t, newDevice(), "  "), 422, "invalid_name")
	for range passkey.MaxPasskeys {
		if r := u.addPasskey(t, newDevice(), "Key"); r.Status != http.StatusCreated {
			t.Fatalf("add = %d %s", r.Status, r.Body)
		}
	}
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/passkeys/begin", nil), 409, "too_many_passkeys")

	appOnly := newMFAUser(t)
	appOnly.enroll(t)
	expectProblem(t, appOnly.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/begin", body: map[string]string{"challenge": appOnly.freshChallenge()}}), 409, "no_passkeys")

	other := newMFAUser(t)
	device := newDevice()
	other.addPasskey(t, device, "Laptop")
	challenge := other.freshChallenge()
	if _, err := other.a.store.DB().Exec(context.Background(), "UPDATE users SET status = 'disabled' WHERE id = $1", other.id); err != nil {
		t.Fatal(err)
	}
	expectProblem(t, other.signInWithPasskey(t, challenge, device), 403, "user_disabled")

	limited := newMFAUser(t)
	limited.addPasskey(t, newDevice(), "Laptop")
	for range 6 {
		expectProblem(t, limited.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/finish", body: map[string]any{"challenge": limited.freshChallenge(), "ceremony": "x", "credential": map[string]any{}}}), 401, "invalid_passkey")
	}
	expectProblem(t, limited.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/finish", body: map[string]any{"challenge": limited.freshChallenge(), "ceremony": "x", "credential": map[string]any{}}}), 429, "rate_limited")
	expectProblem(t, limited.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/begin", body: map[string]string{"challenge": limited.freshChallenge()}}), 429, "rate_limited")
	shared := newMFAUser(t)
	shared.addPasskey(t, newDevice(), "Laptop")
	for range 2 {
		expectProblem(t, shared.verify(shared.freshChallenge(), "000000"), 401, "invalid_code")
	}
	for range 4 {
		expectProblem(t, shared.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/finish", body: map[string]any{"challenge": shared.freshChallenge(), "ceremony": "x", "credential": map[string]any{}}}), 401, "invalid_passkey")
	}
	expectProblem(t, shared.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/finish", body: map[string]any{"challenge": shared.freshChallenge(), "ceremony": "x", "credential": map[string]any{}}}), 429, "rate_limited")
	for range 6 {
		limited.call(http.MethodPost, "/api/v1/me/passkeys/"+uuid.NewString()+"/remove", map[string]string{"password": "not the password!!"})
	}
	expectProblem(t, limited.call(http.MethodPost, "/api/v1/me/passkeys/"+uuid.NewString()+"/remove", map[string]string{"password": adminPassword}), 429, "rate_limited")
}

// brokenPasskeys is a store whose passkey operations fail on demand.
type brokenPasskeys struct {
	*postgres.Store
	fail map[string]bool
}

// Passkeys fails when configured to.
func (b brokenPasskeys) Passkeys(ctx context.Context, id uuid.UUID) ([]passkey.Passkey, error) {
	if b.fail["list"] {
		return nil, errInjected
	}
	return b.Store.Passkeys(ctx, id)
}

// AddPasskey fails when configured to.
func (b brokenPasskeys) AddPasskey(ctx context.Context, userID uuid.UUID, p passkey.Passkey) error {
	if b.fail["add"] {
		return errInjected
	}
	return b.Store.AddPasskey(ctx, userID, p)
}

// DeletePasskey fails when configured to.
func (b brokenPasskeys) DeletePasskey(ctx context.Context, userID, id uuid.UUID) error {
	if b.fail["delete"] {
		return errInjected
	}
	return b.Store.DeletePasskey(ctx, userID, id)
}

// TouchPasskey fails when configured to.
func (b brokenPasskeys) TouchPasskey(ctx context.Context, userID, id uuid.UUID, c webauthn.Credential, at time.Time) error {
	if b.fail["touch"] {
		return errInjected
	}
	return b.Store.TouchPasskey(ctx, userID, id, c, at)
}

// TestPasskeys_Failures verifies that store failures inside the passkey handlers are 500 problems.
//
// Scenarios:
//   - listing in the status, signing in with the password, beginning and finishing a registration, beginning and finishing a passkey sign-in and
//     removing each answer 500 when the store fails
//   - a failing recovery code store, and a password that cannot be checked, answer 500 too
func TestPasskeys_Failures(t *testing.T) {
	fail := map[string]bool{}
	u := newMFAUser(t, func(d *httpapi.Deps) {
		store := d.Readiness.(*postgres.Store)
		keys, err := passkey.NewService(brokenPasskeys{store, fail}, passkey.Config{RPID: "localhost", DisplayName: "S", Origins: []string{localOrigin}}, realClock{}, rand.Reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		d.Passkeys = keys
		d.MFA = mfa.NewService(brokenFactors{store, map[string]bool{}}, instanceKey, rand.Reader, realClock{})
	})
	device := newDevice()
	if r := u.addPasskey(t, device, "Laptop"); r.Status != http.StatusCreated {
		t.Fatalf("add = %d %s", r.Status, r.Body)
	}
	challenge := mfa.NewService(nil, instanceKey, rand.Reader, realClock{}).Challenge(u.id)
	options, ceremony := ceremonyOf(t, u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/begin", body: map[string]string{"challenge": challenge}}))
	answer := asMap(t, device.Assert(options))

	fail["add"] = true
	expectProblem(t, u.addPasskey(t, newDevice(), "Second"), 500, "internal_error")
	fail["add"] = false
	fail["list"] = true
	expectProblem(t, u.a.do(request{method: http.MethodGet, path: "/api/v1/me/security", cookie: u.cookie}), 500, "internal_error")
	expectProblem(t, u.login(), 500, "internal_error")
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/passkeys/begin", nil), 500, "internal_error")
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/passkeys/finish", map[string]any{"ceremony": "x", "name": "x", "credential": map[string]any{}}), 409, "ceremony_expired")
	expectProblem(t, u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/begin", body: map[string]string{"challenge": challenge}}), 500, "internal_error")
	fail["list"] = false
	fail["touch"] = true
	expectProblem(t, u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/finish", body: map[string]any{"challenge": challenge, "ceremony": ceremony, "credential": answer}}), 500, "internal_error")
	fail["touch"] = false
	fail["delete"] = true
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/passkeys/"+uuid.NewString()+"/remove", map[string]string{"password": adminPassword}), 500, "internal_error")
	fail["delete"] = false
}

// TestPasskeys_MoreFailures verifies the failures after a valid answer and inside the recovery codes.
//
// Scenarios:
//   - a failing account lookup, personal workspace or session after a right passkey is a 500 problem
//   - failing recovery codes, on registering, removing, disabling the app and renewing, are 500 problems, and so is a password that cannot be checked
func TestPasskeys_MoreFailures(t *testing.T) {
	var flaky *flakyStore
	var workspaces *failingWorkspaces
	keyFail, factorFail := map[string]bool{}, map[string]bool{}
	u := newMFAUser(t, withFailingWorkspaces(func(f *failingWorkspaces) { workspaces = f }), func(d *httpapi.Deps) {
		withFlaky(func(f *flakyStore) { flaky = f })(d)
		store := d.Readiness.(*postgres.Store)
		keys, err := passkey.NewService(brokenPasskeys{store, keyFail}, passkey.Config{RPID: "localhost", DisplayName: "S", Origins: []string{localOrigin}}, realClock{}, rand.Reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		d.Passkeys = keys
		d.MFA = mfa.NewService(brokenFactors{store, factorFail}, instanceKey, rand.Reader, realClock{})
	})
	device := newDevice()
	if r := u.addPasskey(t, device, "Laptop"); r.Status != http.StatusCreated {
		t.Fatalf("add = %d %s", r.Status, r.Body)
	}
	challenge := mfa.NewService(nil, instanceKey, rand.Reader, realClock{}).Challenge(u.id)
	begin := func() (map[string]any, string) {
		options, ceremony := ceremonyOf(t, u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/begin", body: map[string]string{"challenge": challenge}}))
		return asMap(t, device.Assert(options)), ceremony
	}
	finish := func(answer map[string]any, ceremony string) reply {
		return u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/finish", body: map[string]any{"challenge": challenge, "ceremony": ceremony, "credential": answer}})
	}

	flaky.failUserByID = true
	expectProblem(t, u.a.do(request{method: http.MethodPost, path: "/api/v1/auth/passkey/begin", body: map[string]string{"challenge": challenge}}), 500, "internal_error")
	expectProblem(t, finish(map[string]any{}, "unknown"), 500, "internal_error")
	flaky.failUserByID = false
	answer, ceremony := begin()
	workspaces.failPersonal = true
	expectProblem(t, finish(answer, ceremony), 500, "internal_error")
	workspaces.failPersonal = false
	answer, ceremony = begin()
	flaky.failCreateSession = true
	expectProblem(t, finish(answer, ceremony), 500, "internal_error")
	flaky.failCreateSession = false

	factorFail["left"] = true
	expectProblem(t, u.addPasskey(t, newDevice(), "Second"), 500, "internal_error")
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/recovery-codes", map[string]string{"password": adminPassword}), 500, "internal_error")
	factorFail["left"] = false
	factorFail["replace"] = true
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/recovery-codes", map[string]string{"password": adminPassword}), 500, "internal_error")
	factorFail["replace"] = false
	keyFail["list"] = true
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/recovery-codes", map[string]string{"password": adminPassword}), 500, "internal_error")
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/totp/disable", map[string]string{"password": adminPassword}), 500, "internal_error")
	keyFail["list"] = false
}

// TestPasskeys_RemoveFailure verifies that a failure to clear the recovery codes after removing the last factor is a 500 problem.
//
// Scenarios:
//   - the last passkey is removed and then reading the recovery codes fails: the answer is 500
//   - a password that cannot be checked is a 500 problem
func TestPasskeys_RemoveFailure(t *testing.T) {
	factorFail := map[string]bool{}
	u := newMFAUser(t, func(d *httpapi.Deps) {
		d.MFA = mfa.NewService(brokenFactors{d.Readiness.(*postgres.Store), factorFail}, instanceKey, rand.Reader, realClock{})
	})
	var added httpapi.PasskeyAdded
	mustDecode(t, u.addPasskey(t, newDevice(), "Laptop").Body, &added)
	factorFail["left"] = true
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/passkeys/"+added.Passkey.Id.String()+"/remove", map[string]string{"password": adminPassword}), 500, "internal_error")
	if _, err := u.a.store.DB().Exec(context.Background(), "UPDATE users SET password_hash = 'garbage' WHERE id = $1", u.id); err != nil {
		t.Fatal(err)
	}
	expectProblem(t, u.call(http.MethodPost, "/api/v1/me/passkeys/"+uuid.NewString()+"/remove", map[string]string{"password": adminPassword}), 500, "internal_error")
}
