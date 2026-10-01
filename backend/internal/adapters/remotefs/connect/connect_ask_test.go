package connect_test

import (
	"errors"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/credentials"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/testutil"
)

// TestConnector_AskForPassword covers sites of the [site.AuthAsk] logon type in [connect.Connector.Connect] and
// [connect.Connector.Admit].
//
// Use cases:
//   - with nowhere to hold typed passwords, such a site always needs one
//   - with no password typed in the session it needs one, and the site is from then on known to ask
//   - another session is not admitted to its open connections, the session that typed the password is, and a site that
//     does not ask admits everyone
//   - the typed password connects without the vault being involved
//   - a password the server refuses is ErrAuth and is dropped, so the next attempt asks again
func TestConnector_AskForPassword(t *testing.T) {
	e := newEnv(t)
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	e.trust(server)
	target := e.addSite(server, site.Input{Name: "Asks", AuthMethod: site.AuthAsk}, site.SecretChanges{})
	plain := e.addSite(server, site.Input{Name: "Stored"}, site.SecretChanges{Password: text(sshPassword)})

	if _, err := e.connector(e.allow).Connect(t.Context(), e.key(target), session); !errors.Is(err, credentials.ErrRequired) {
		t.Fatalf("Connect() with no store = %v; want ErrRequired", err)
	}

	store := credentials.New(testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), time.Hour)
	c := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.AskedPasswords(store))
	if _, err := c.Connect(t.Context(), e.key(target), session); !errors.Is(err, credentials.ErrRequired) {
		t.Fatalf("Connect() before typing = %v; want ErrRequired", err)
	}
	if err := c.Admit(t.Context(), e.key(target), session); !errors.Is(err, credentials.ErrRequired) {
		t.Errorf("Admit() of a session with no password = %v; want ErrRequired", err)
	}
	if err := c.Admit(t.Context(), e.key(plain), session); err != nil {
		t.Errorf("Admit() to a site that does not ask = %v", err)
	}

	asked := credentials.Key{Session: session, UserID: e.user, WorkspaceID: e.workspace, SiteID: target.ID}
	store.Put(asked, []byte(sshPassword), true)
	if err := c.Admit(t.Context(), e.key(target), session); err != nil {
		t.Errorf("Admit() of the session that typed the password = %v", err)
	}
	if err := c.Admit(t.Context(), e.key(target), "another-session"); !errors.Is(err, credentials.ErrRequired) {
		t.Errorf("Admit() of another session = %v; want ErrRequired", err)
	}
	e.vaults.Lock(session)
	fsys, err := c.Connect(t.Context(), e.key(target), session)
	if err != nil {
		t.Fatalf("Connect() with the typed password and the vault locked = %v", err)
	}
	_ = fsys.Close()

	store.Put(asked, []byte("not the password"), true)
	if _, err := c.Connect(t.Context(), e.key(target), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Fatalf("Connect() with a wrong typed password = %v; want ErrAuth", err)
	}
	if _, ok := store.Password(asked); ok {
		t.Error("a refused password was kept")
	}
}

// TestConnector_Anonymous covers sites of the [site.AuthAnonymous] logon type in [connect.Connector.Connect].
//
// Use cases:
//   - an anonymous SFTP site logs in with the SSH "none" method, with no secret stored and the vault locked
//   - a server that refuses "none" is offered FileZilla's anonymous password next
//   - a server that takes neither refuses the site with ErrAuth
func TestConnector_Anonymous(t *testing.T) {
	e := newEnv(t)
	c := e.connector(e.allow)
	e.vaults.Lock(session)

	open := sftptest.Start(t, sftptest.Options{Anonymous: true})
	e.trust(open)
	public := e.addSite(open, site.Input{Name: "Public", AuthMethod: site.AuthAnonymous}, site.SecretChanges{})
	fsys, err := c.Connect(t.Context(), e.key(public), session)
	if err != nil {
		t.Fatalf("Connect() with the none method = %v", err)
	}
	_ = fsys.Close()

	byPassword := sftptest.Start(t, sftptest.Options{Users: map[string]string{"anonymous": "anonymous@example.com"}})
	e.trust(byPassword)
	fallback := e.addSite(byPassword, site.Input{Name: "Password", AuthMethod: site.AuthAnonymous}, site.SecretChanges{})
	fsys, err = c.Connect(t.Context(), e.key(fallback), session)
	if err != nil {
		t.Fatalf("Connect() with the anonymous password = %v", err)
	}
	_ = fsys.Close()

	closed := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	e.trust(closed)
	refused := e.addSite(closed, site.Input{Name: "Closed", AuthMethod: site.AuthAnonymous}, site.SecretChanges{})
	if _, err := c.Connect(t.Context(), e.key(refused), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() to a server with no anonymous access = %v; want ErrAuth", err)
	}
}
