package connect_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/testutil"
)

// TestConnector_Sudo verifies connecting to SFTP sites in sudo mode (ADR-0027).
//
// Use cases:
//   - sudo answers with the sudo password of the site, or with the password the account signs in with when the site has none, and
//     the log tells the user sudo runs as after signing in, never a password
//   - a wrong password is refused as elevation refused
//   - when the instance does not allow commands on servers, a site in sudo mode fails as unsupported, and one without sudo connects
func TestConnector_Sudo(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}, Sudo: &sftptest.Sudo{Password: sshPassword}})
	e.trust(server)
	host, port, _ := splitAddr(server.Addr)
	add := func(name string, config map[string]string, secrets site.SecretChanges) site.Site {
		all := map[string]string{"host": host, "port": strconv.Itoa(port), "username": sshUser}
		for k, v := range config {
			all[k] = v
		}
		created, err := e.sites.Create(t.Context(), e.user, e.workspace, session, site.Input{Name: name, Connector: "sftp", Config: all}, secrets)
		if err != nil {
			t.Fatalf("Create(%s) = %v", name, err)
		}
		return created
	}
	sink := &collected{}
	recorder := connlog.New(sink, testutil.NewFakeClock(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)))
	allowed := true
	c := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.ConnectionLog(recorder), connect.ServerCommands(func() bool { return allowed }))
	sudo := map[string]string{"sudo": "true", "sudo_user": "backup"}
	for name, s := range map[string]site.Site{
		"its own sudo password": add("sudo own", sudo, site.SecretChanges{site.SecretPassword: sshPassword, site.SecretSudoPassword: sshPassword}),
		"the sign-in password":  add("sudo login", sudo, site.SecretChanges{site.SecretPassword: sshPassword}),
	} {
		fsys, err := c.Connect(t.Context(), e.key(s), session)
		if err != nil {
			t.Errorf("Connect() with %s = %v", name, err)
			continue
		}
		_ = fsys.Close()
		entries := sink.take()
		if got := steps(entries); !strings.HasSuffix(got, "authenticated elevated") {
			t.Errorf("steps with %s = %q", name, got)
		}
		if last := entries[len(entries)-1]; last.Params["user"] != "backup" {
			t.Errorf("elevated = %+v, want the user sudo runs as", last)
		}
		for _, entry := range entries {
			for _, value := range entry.Params {
				if strings.Contains(value, sshPassword) {
					t.Errorf("an entry holds a password: %+v", entry)
				}
			}
		}
	}
	wrong := add("sudo wrong", sudo, site.SecretChanges{site.SecretPassword: sshPassword, site.SecretSudoPassword: "nope"})
	if _, err := c.Connect(t.Context(), e.key(wrong), session); !errors.Is(err, remotefs.ErrElevationRefused) {
		t.Errorf("Connect() with a wrong sudo password = %v, want ErrElevationRefused", err)
	}
	allowed = false
	if _, err := c.Connect(t.Context(), e.key(wrong), session); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Errorf("Connect() in sudo mode with commands off = %v, want ErrUnsupported", err)
	}
	plain, err := c.Connect(t.Context(), e.key(add("plain", nil, site.SecretChanges{site.SecretPassword: sshPassword})), session)
	if err != nil {
		t.Fatalf("Connect() without sudo and commands off = %v", err)
	}
	_ = plain.Close()
}
