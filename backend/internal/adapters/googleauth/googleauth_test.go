package googleauth_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/googleauth"
	"github.com/fprambs/transferly/internal/adapters/remotefs/gdrivefs/googletest"
)

// config returns the configuration that points at a fake of Google.
func config(s *googletest.Server) googleauth.Config {
	return googleauth.Config{ClientID: googletest.ClientID, ClientSecret: googletest.ClientSecret, AuthURL: s.URL + "/o/oauth2/auth", TokenURL: s.URL + "/token", APIURL: s.URL}
}

// consent follows the consent page of a sign-in as a browser does, and returns what Google sends back: the code, or the error.
func consent(t *testing.T, authURL string) url.Values {
	t.Helper()
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, authURL, nil)
	resp, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	back, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return back.Query()
}

// clock is a settable time.
type clock struct{ at time.Time }

// now returns the time of the clock.
func (c *clock) now() time.Time { return c.at }

// TestSignIn verifies the sign-in with Google (ADR-0024).
//
// Use cases:
//   - without an OAuth client, signing in is refused
//   - a sign-in sends to the consent page with PKCE, offline access and the scopes of Drive, and comes back to the callback of its
//     origin; finishing it gives the refresh token, the email of the account, the origin and who started it
//   - a state is finished once, a state that is too old is unknown, and abandoned states are forgotten
//   - a person who declines, a code Google refuses, and an answer without a refresh token fail the sign-in, which still names its origin
//   - an account Google does not describe, or describes in a form that cannot be read, fails the sign-in
//   - the refresh token authorizes requests to the APIs
func TestSignIn(t *testing.T) {
	s := googletest.Start(t, googletest.Options{})
	at := &clock{at: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	user := uuid.New()
	if _, _, err := googleauth.New(googleauth.Config{}, http.DefaultClient, at.now).Authorize(user, "http://x"); !errors.Is(err, googleauth.ErrNotConfigured) {
		t.Errorf("Authorize() without a client = %v, want ErrNotConfigured", err)
	}

	svc := googleauth.New(config(s), http.DefaultClient, at.now)
	authURL, state, err := svc.Authorize(user, "https://files.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(authURL)
	params := q.Query()
	if params.Get("code_challenge_method") != "S256" || params.Get("access_type") != "offline" || params.Get("prompt") != "consent" ||
		params.Get("redirect_uri") != "https://files.example.com/api/v1/oauth/google/callback" || !strings.Contains(params.Get("scope"), "auth/drive") {
		t.Errorf("consent page = %s", authURL)
	}
	back := consent(t, authURL)
	result, err := svc.Complete(t.Context(), back.Get("state"), back.Get("code"), back.Get("error"))
	if err != nil || result.Email != googletest.Email || result.RefreshToken == "" || result.Origin != "https://files.example.com" || result.User != user || back.Get("state") != state {
		t.Fatalf("Complete() = %+v, %v", result, err)
	}
	if _, err := svc.Complete(t.Context(), state, back.Get("code"), ""); !errors.Is(err, googleauth.ErrUnknownState) {
		t.Errorf("Complete() of a finished state = %v, want ErrUnknownState", err)
	}

	authURL, _, _ = svc.Authorize(user, "https://files.example.com")
	_, abandoned, _ := svc.Authorize(user, "https://files.example.com")
	at.at = at.at.Add(11 * time.Minute)
	back = consent(t, authURL)
	if _, err := svc.Complete(t.Context(), back.Get("state"), back.Get("code"), ""); !errors.Is(err, googleauth.ErrUnknownState) {
		t.Errorf("Complete() of an old state = %v, want ErrUnknownState", err)
	}
	if _, _, err := svc.Authorize(user, "https://files.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Complete(t.Context(), abandoned, "code", ""); !errors.Is(err, googleauth.ErrUnknownState) {
		t.Errorf("Complete() of an abandoned state, forgotten by a later sign-in = %v, want ErrUnknownState", err)
	}

	_, state, _ = svc.Authorize(user, "https://files.example.com")
	if result, err := svc.Complete(t.Context(), state, "", "access_denied"); !errors.Is(err, googleauth.ErrDenied) || result.Origin != "https://files.example.com" {
		t.Errorf("Complete() declined = %+v, %v, want ErrDenied with the origin", result, err)
	}
	_, state, _ = svc.Authorize(user, "https://files.example.com")
	if _, err := svc.Complete(t.Context(), state, "forged", ""); !errors.Is(err, googleauth.ErrDenied) {
		t.Errorf("Complete() with a refused code = %v, want ErrDenied", err)
	}

	stingy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"a","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(stingy.Close)
	noRefresh := config(s)
	noRefresh.TokenURL = stingy.URL
	stingySvc := googleauth.New(noRefresh, http.DefaultClient, at.now)
	authURL, _, _ = stingySvc.Authorize(user, "https://files.example.com")
	back = consent(t, authURL)
	if _, err := stingySvc.Complete(t.Context(), back.Get("state"), back.Get("code"), ""); !errors.Is(err, googleauth.ErrNoRefreshToken) {
		t.Errorf("Complete() without a refresh token = %v, want ErrNoRefreshToken", err)
	}

	blind := googletest.Start(t, googletest.Options{Fault: func(r *http.Request) *googletest.Fault {
		if strings.HasSuffix(r.URL.Path, "/userinfo") {
			return &googletest.Fault{Status: http.StatusForbidden, Reason: "forbidden"}
		}
		return nil
	}})
	blindSvc := googleauth.New(config(blind), http.DefaultClient, at.now)
	authURL, _, _ = blindSvc.Authorize(user, "https://files.example.com")
	back = consent(t, authURL)
	if _, err := blindSvc.Complete(t.Context(), back.Get("state"), back.Get("code"), ""); err == nil {
		t.Error("Complete() for an account Google does not describe succeeded")
	}
	garbled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) }))
	t.Cleanup(garbled.Close)
	unreadable := config(s)
	unreadable.APIURL = garbled.URL
	unreadableSvc := googleauth.New(unreadable, http.DefaultClient, at.now)
	authURL, _, _ = unreadableSvc.Authorize(user, "https://files.example.com")
	back = consent(t, authURL)
	if _, err := unreadableSvc.Complete(t.Context(), back.Get("state"), back.Get("code"), ""); err == nil {
		t.Error("Complete() with an account Google describes in garbage succeeded")
	}
	gone := config(s)
	gone.APIURL = "http://127.0.0.1:1"
	goneSvc := googleauth.New(gone, http.DefaultClient, at.now)
	authURL, _, _ = goneSvc.Authorize(user, "https://files.example.com")
	back = consent(t, authURL)
	if _, err := goneSvc.Complete(t.Context(), back.Get("state"), back.Get("code"), ""); err == nil {
		t.Error("Complete() when the API cannot be reached succeeded")
	}

	authorized := svc.Client(http.DefaultClient, result.RefreshToken)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL+"/drive/v3/about", nil)
	resp, err := authorized.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Errorf("request with the refresh token = %v, %v", resp, err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
	if svc.Config().ClientID != googletest.ClientID {
		t.Errorf("Config() = %+v", svc.Config())
	}
}
