package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/googleauth"
	"github.com/fprambs/transferly/internal/adapters/remotefs/gdrivefs/googletest"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// signedInWithGoogle returns an API whose sign-in with Google talks to a fake of Google.
func signedInWithGoogle(t *testing.T, fake *googletest.Server) *api {
	google := googleauth.New(googleauth.Config{
		ClientID: googletest.ClientID, ClientSecret: googletest.ClientSecret, AuthURL: fake.URL + "/o/oauth2/auth", TokenURL: fake.URL + "/token", APIURL: fake.URL,
	}, http.DefaultClient, time.Now)
	return newAPI(t, func(d *httpapi.Deps) { d.Google = google })
}

// stingyToken serves a token endpoint that grants access tokens without refresh tokens, and returns its URL.
func stingyToken(t *testing.T) string {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"a","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// outcomeOf reads the outcome a callback page publishes, after checking that only its own script may run.
func outcomeOf(t *testing.T, r reply) string {
	t.Helper()
	nonce := regexp.MustCompile(`<script nonce="([^"]+)">`).FindStringSubmatch(string(r.Body))
	policy := r.Header.Get("Content-Security-Policy")
	if r.Status != http.StatusOK || nonce == nil || !strings.Contains(policy, "script-src 'nonce-"+nonce[1]+"'") || !strings.Contains(policy, "default-src 'none'") ||
		!strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") || !strings.Contains(string(r.Body), `new BroadcastChannel("transferly-oauth")`) {
		t.Fatalf("callback page = %d %s %s", r.Status, policy, r.Body)
	}
	return regexp.MustCompile(`const outcome = (\{[^\n]*\});`).FindStringSubmatch(string(r.Body))[1]
}

// TestGoogleSignIn verifies signing Google accounts in over HTTP (ADR-0024).
//
// Use cases:
//   - a signed-in person starts a sign-in and gets the consent page of Google, which comes back to the callback of the origin of the
//     request, and a state
//   - the callback finishes it: its page publishes the state, the refresh token and the email of the account on the broadcast channel,
//     under a policy that runs its own script only
//   - a state that is not known, a person who declines, a sign-in Google refuses, one without a refresh token and one whose account
//     cannot be read publish why
//   - without the sign-in with Google, starting is refused with guidance and the callback says it is not configured; without a session
//     starting is refused
func TestGoogleSignIn(t *testing.T) {
	fake := googletest.Start(t, googletest.Options{})
	a := signedInWithGoogle(t, fake)
	cookie, csrf := a.setup()
	start := func(api *api) reply {
		return api.do(request{method: http.MethodPost, path: "/api/v1/oauth/google/authorizations", cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf, "Origin": localOrigin}})
	}
	r := start(a)
	var started httpapi.GoogleAuthorization
	mustDecode(t, r.Body, &started)
	consentPage, _ := url.Parse(started.Url)
	if r.Status != http.StatusCreated || started.State == "" || consentPage.Query().Get("redirect_uri") != localOrigin+"/api/v1/oauth/google/callback" || consentPage.Query().Get("state") != started.State {
		t.Fatalf("authorize = %d %s", r.Status, r.Body)
	}

	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, started.Url, nil)
	resp, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	back, _ := url.Parse(resp.Header.Get("Location"))
	outcome := outcomeOf(t, a.do(request{method: http.MethodGet, path: back.RequestURI()}))
	if !strings.Contains(outcome, `"state":"`+started.State+`"`) || !strings.Contains(outcome, `"email":"`+googletest.Email+`"`) || !strings.Contains(outcome, `"refreshToken":"refresh-`) {
		t.Errorf("outcome = %s", outcome)
	}

	if outcome := outcomeOf(t, a.do(request{method: http.MethodGet, path: back.RequestURI()})); !strings.Contains(outcome, `"error":"expired"`) {
		t.Errorf("outcome of a finished state = %s", outcome)
	}
	mustDecode(t, start(a).Body, &started)
	if outcome := outcomeOf(t, a.do(request{method: http.MethodGet, path: "/api/v1/oauth/google/callback?error=access_denied&state=" + started.State})); !strings.Contains(outcome, `"error":"denied"`) {
		t.Errorf("outcome of a refusal = %s", outcome)
	}
	mustDecode(t, start(a).Body, &started)
	if outcome := outcomeOf(t, a.do(request{method: http.MethodGet, path: "/api/v1/oauth/google/callback?code=forged&state=" + started.State})); !strings.Contains(outcome, `"error":"denied"`) {
		t.Errorf("outcome of a forged code = %s", outcome)
	}

	for reason, google := range map[string]googleauth.Config{
		"no_refresh_token": {ClientID: googletest.ClientID, ClientSecret: googletest.ClientSecret, AuthURL: fake.URL + "/o/oauth2/auth", TokenURL: stingyToken(t), APIURL: fake.URL},
		"failed":           {ClientID: googletest.ClientID, ClientSecret: googletest.ClientSecret, AuthURL: fake.URL + "/o/oauth2/auth", TokenURL: fake.URL + "/token", APIURL: "http://127.0.0.1:1"},
	} {
		other := newAPI(t, func(d *httpapi.Deps) { d.Google = googleauth.New(google, http.DefaultClient, time.Now) })
		otherCookie, otherCSRF := other.setup()
		var begun httpapi.GoogleAuthorization
		mustDecode(t, other.do(request{method: http.MethodPost, path: "/api/v1/oauth/google/authorizations", cookie: otherCookie, headers: map[string]string{"X-CSRF-Token": otherCSRF}}).Body, &begun)
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, begun.Url, nil)
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		back, _ := url.Parse(resp.Header.Get("Location"))
		if outcome := outcomeOf(t, other.do(request{method: http.MethodGet, path: back.RequestURI()})); !strings.Contains(outcome, `"error":"`+reason+`"`) {
			t.Errorf("outcome = %s, want %s", outcome, reason)
		}
	}

	plain := newAPI(t)
	plainCookie, plainCSRF := plain.setup()
	expectProblem(t, plain.do(request{method: http.MethodPost, path: "/api/v1/oauth/google/authorizations", cookie: plainCookie, headers: map[string]string{"X-CSRF-Token": plainCSRF}}), http.StatusConflict, "oauth_not_configured")
	if outcome := outcomeOf(t, plain.do(request{method: http.MethodGet, path: "/api/v1/oauth/google/callback?state=x&code=y"})); !strings.Contains(outcome, `"error":"not_configured"`) {
		t.Errorf("outcome without the sign-in = %s", outcome)
	}
	unconfigured := newAPI(t, func(d *httpapi.Deps) { d.Google = googleauth.New(googleauth.Config{}, http.DefaultClient, time.Now) })
	unconfiguredCookie, unconfiguredCSRF := unconfigured.setup()
	expectProblem(t, unconfigured.do(request{method: http.MethodPost, path: "/api/v1/oauth/google/authorizations", cookie: unconfiguredCookie, headers: map[string]string{"X-CSRF-Token": unconfiguredCSRF}}), http.StatusConflict, "oauth_not_configured")
	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/oauth/google/authorizations"}), http.StatusUnauthorized, "unauthenticated")
}
