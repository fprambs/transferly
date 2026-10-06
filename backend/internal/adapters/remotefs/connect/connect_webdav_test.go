package connect_test

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/adapters/remotefs/davfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/davfs/davtest"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/testutil"
)

// TestConnector_WebDAV verifies connecting to WebDAV sites (ADR-0026).
//
// Use cases:
//   - a site over plain HTTP signs in with its password, and a public share anonymously; the log tells that HTTP is clear text and
//     who signed in, never the password
//   - a wrong password fails authentication
//   - over HTTPS, a self-signed certificate that is not trusted yet is offered by its fingerprint, under the certificate key type
//   - a path without its leading slash is saved with it
func TestConnector_WebDAV(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	sink := &collected{}
	recorder := connlog.New(sink, testutil.NewFakeClock(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)))
	c := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.ConnectionLog(recorder))
	add := func(name, address string, config map[string]string, secrets site.SecretChanges) site.Site {
		host, port, _ := strings.Cut(address, ":")
		all := map[string]string{"host": host, "port": port, "path": davtest.Root}
		for k, v := range config {
			all[k] = v
		}
		created, err := e.sites.Create(t.Context(), e.user, e.workspace, session, site.Input{Name: name, Connector: "webdav", Config: all}, secrets)
		if err != nil {
			t.Fatalf("Create(%s) = %v", name, err)
		}
		return created
	}
	private := davtest.Start(t, davtest.Options{})
	public := davtest.Start(t, davtest.Options{Anonymous: true})
	plain := map[string]string{"tls": "false", "username": davtest.Username}
	for name, s := range map[string]site.Site{
		"a site with its password": add("dav password", strings.TrimPrefix(private.URL, "http://"), plain, site.SecretChanges{site.SecretPassword: davtest.Password}),
		"a public share":           add("dav public", strings.TrimPrefix(public.URL, "http://"), map[string]string{"tls": "false", "auth": "anonymous"}, nil),
	} {
		fsys, err := c.Connect(t.Context(), e.key(s), session)
		if err != nil {
			t.Errorf("Connect() to %s = %v", name, err)
			continue
		}
		_ = fsys.Close()
		entries := sink.take()
		if got := steps(entries); got != "connecting plain_text connected authenticated" {
			t.Errorf("steps to %s = %q", name, got)
		}
		if last := entries[len(entries)-1]; last.Params["username"] != s.Config["username"] {
			t.Errorf("authenticated to %s = %+v, want the username of the site", name, last)
		}
		for _, entry := range entries {
			for _, value := range entry.Params {
				if strings.Contains(value, davtest.Password) {
					t.Errorf("an entry holds the password: %+v", entry)
				}
			}
		}
	}
	wrong := add("dav wrong", strings.TrimPrefix(private.URL, "http://"), plain, site.SecretChanges{site.SecretPassword: "wrong"})
	if _, err := c.Connect(t.Context(), e.key(wrong), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() with a wrong password = %v, want ErrAuth", err)
	}

	server := httptest.NewUnstartedServer(davtest.New(davtest.Options{}).Handler())
	server.StartTLS()
	t.Cleanup(server.Close)
	secure := add("dav https", strings.TrimPrefix(server.URL, "https://"), map[string]string{"username": davtest.Username, "path": strings.TrimPrefix(davtest.Root, "/")}, site.SecretChanges{site.SecretPassword: davtest.Password})
	if secure.Config["path"] != davtest.Root {
		t.Errorf("saved path = %q, want %q", secure.Config["path"], davtest.Root)
	}
	_, err := c.Connect(t.Context(), e.key(secure), session)
	var unknown *remotefs.HostKeyError
	if !errors.As(err, &unknown) || unknown.Type != davfs.CertificateKeyType || unknown.Fingerprint != davfs.Fingerprint(server.Certificate().Raw) {
		t.Errorf("Connect() to a self-signed server = %v, want its certificate offered", err)
	}
}
