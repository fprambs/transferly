package connect_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/googleauth"
	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/adapters/remotefs/gdrivefs/googletest"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/testutil"
)

// TestConnector_GDrive verifies connecting to Google Drive sites (ADR-0024).
//
// Use cases:
//   - a site whose account is connected opens its drive, and its log names the API it reached and the account, never the token; an
//     API without a port is reached on the port of its scheme
//   - without the sign-in with Google, or without a client configured, the site cannot connect; a site with no account connected is
//     refused credentials, and so is one whose token Google revoked
func TestConnector_GDrive(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := googletest.Start(t, googletest.Options{})
	google := googleauth.New(googleauth.Config{
		ClientID: googletest.ClientID, ClientSecret: googletest.ClientSecret, AuthURL: s.URL + "/o/oauth2/auth", TokenURL: s.URL + "/token", APIURL: s.URL,
	}, http.DefaultClient, time.Now)
	refresh := s.RefreshToken()
	add := func(name string, secrets site.SecretChanges) site.Site {
		created, err := e.sites.Create(t.Context(), e.user, e.workspace, session, site.Input{Name: name, Connector: "gdrive", Config: map[string]string{"account_email": googletest.Email}}, secrets)
		if err != nil {
			t.Fatalf("Create() = %v", err)
		}
		return created
	}
	connected := add("Drive", site.SecretChanges{site.SecretRefreshToken: refresh})
	sink := &collected{}
	recorder := connlog.New(sink, testutil.NewFakeClock(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)))
	c := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.Google(google), connect.ConnectionLog(recorder))

	fsys, err := c.Connect(t.Context(), e.key(connected), session)
	if err != nil {
		t.Fatalf("Connect() = %v", err)
	}
	_ = fsys.Close()
	entries := sink.take()
	if got := steps(entries); got != "connecting connected authenticated" {
		t.Errorf("steps = %q", got)
	}
	if first, last := entries[0], entries[len(entries)-1]; first.Params["host"] != "127.0.0.1" || last.Params["method"] != "google" || last.Params["username"] != googletest.Email {
		t.Errorf("connecting = %+v, authenticated = %+v", first, last)
	}
	for _, entry := range entries {
		for _, value := range entry.Params {
			if strings.Contains(value, refresh) {
				t.Errorf("an entry holds the refresh token: %+v", entry)
			}
		}
	}

	for name, unconfigured := range map[string]*connect.Connector{
		"without the sign-in": connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second),
		"without a client":    connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.Google(googleauth.New(googleauth.Config{}, http.DefaultClient, time.Now))),
	} {
		if _, err := unconfigured.Connect(t.Context(), e.key(connected), session); !errors.Is(err, remotefs.ErrInvalid) {
			t.Errorf("Connect() %s = %v, want ErrInvalid", name, err)
		}
	}
	portless := googleauth.New(googleauth.Config{
		ClientID: googletest.ClientID, ClientSecret: googletest.ClientSecret, AuthURL: s.URL + "/o/oauth2/auth", TokenURL: s.URL + "/token", APIURL: "http://127.0.0.1",
	}, http.DefaultClient, time.Now)
	defaulted := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.Google(portless), connect.ConnectionLog(recorder))
	if _, err := defaulted.Connect(t.Context(), e.key(connected), session); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Connect() to an API that does not answer = %v, want ErrConnectionLost", err)
	}
	if first := sink.take()[0]; first.Params["port"] != "80" {
		t.Errorf("connecting to an API without a port = %+v, want the port of its scheme", first)
	}
	if _, err := c.Connect(t.Context(), e.key(add("No account", nil)), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() without an account = %v, want ErrAuth", err)
	}
	s.RevokeTokens()
	if _, err := c.Connect(t.Context(), e.key(connected), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() with a revoked token = %v, want ErrAuth", err)
	}
}
