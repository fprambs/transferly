package connect_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/adapters/remotefs/ftpfs/ftptest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/hostkey"
	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/testutil"
)

// collected keeps the events published to it: all of them, and those not taken yet.
type collected struct {
	mu     sync.Mutex
	events []ports.Event
	all    []ports.Event
}

// Publish implements [ports.EventSink].
func (c *collected) Publish(e ports.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	c.all = append(c.all, e)
}

// take returns the entries published since the last call, and forgets them.
func (c *collected) take() []connlog.Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	entries := make([]connlog.Entry, 0, len(c.events))
	for _, e := range c.events {
		entries = append(entries, e.Data.(connlog.Entry))
	}
	c.events = nil
	return entries
}

// steps names the steps of entries, prefixed with "jump:" for those of the jump host.
func steps(entries []connlog.Entry) string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := string(e.Step)
		if e.Params["hop"] == "jump" {
			name = "jump:" + name
		}
		names = append(names, name)
	}
	return strings.Join(names, " ")
}

// logged returns a connector that tells its steps to sink.
func (e *env) logged(sink *collected) *connect.Connector {
	recorder := connlog.New(sink, testutil.NewFakeClock(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)))
	return connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.ConnectionLog(recorder))
}

// TestConnector_ConnectionLog covers the steps a connection tells the user (ADR-0011).
//
// Use cases:
//   - an SFTP site tells that it connects directly, the address it reached, that the server key is not trusted yet,
//     and once trusted that it was verified and which account signed in with which logon type
//   - a server key other than the trusted one is told as changed, and nothing signs in
//   - through a jump host reached via a proxy, the jump host's steps come first, marked as the jump host's, naming the
//     proxy, and then the site's own, reached through the jump host
//   - FTP with optional encryption tells that it went on without TLS, and FTP with TLS tells the certificate it checked
//   - every entry is about the site and the user of the connection, and none holds a password, key or passphrase
func TestConnector_ConnectionLog(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	sink := &collected{}
	c := e.logged(sink)

	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	target := e.addSite(server, spec{Name: "Logged"}, changes{Password: text(sshPassword)})
	if _, err := c.Connect(t.Context(), e.key(target), session); err == nil {
		t.Fatal("Connect() to an unknown server key succeeded")
	}
	if got := steps(sink.take()); got != "connecting connected host_key_unknown" {
		t.Errorf("steps to an unknown key = %q", got)
	}
	e.trust(server)
	fsys, err := c.Connect(t.Context(), e.key(target), session)
	if err != nil {
		t.Fatal(err)
	}
	_ = fsys.Close()
	entries := sink.take()
	if got := steps(entries); got != "connecting connected host_key_verified authenticated" {
		t.Errorf("steps of a connection = %q", got)
	}
	first, last := entries[0], entries[len(entries)-1]
	if first.Params["via"] != "direct" || first.Params["host"] != "127.0.0.1" || first.Level != connlog.Info || first.SiteID != target.ID || first.WorkspaceID != e.workspace {
		t.Errorf("connecting = %+v", first)
	}
	if last.Params["method"] != "password" || last.Params["username"] != sshUser || !last.Time.Equal(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("authenticated = %+v", last)
	}
	if entries[1].Params["address"] != server.Addr || entries[2].Params["fingerprint"] != ssh.FingerprintSHA256(server.HostKey) {
		t.Errorf("connected and verified = %+v %+v", entries[1], entries[2])
	}

	changed := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	host, port, _ := splitAddr(changed.Addr)
	if _, err := e.hostKeys.Trust(t.Context(), e.user, e.workspace, host, port, changed.HostKey.Type(), "SHA256:"+strings.Repeat("A", 43), hostkey.TrustNew); err != nil {
		t.Fatal(err)
	}
	moved := e.addSite(changed, spec{Name: "Changed"}, changes{Password: text(sshPassword)})
	_, _ = c.Connect(t.Context(), e.key(moved), session)
	if entries := sink.take(); steps(entries) != "connecting connected host_key_changed" || entries[2].Level != connlog.Error {
		t.Errorf("steps to a changed key = %q", steps(entries))
	}

	jump, jumpKey := jumpTestHost(t, "bastion", "let-me-through")
	jumpHost, jumpPort, _ := splitAddr(jump)
	if _, err := e.hostKeys.Trust(t.Context(), e.user, e.workspace, jumpHost, jumpPort, jumpKey.Type(), ssh.FingerprintSHA256(jumpKey), hostkey.TrustNew); err != nil {
		t.Fatal(err)
	}
	proxy := socks5TestProxy(t, nil)
	proxyHost, proxyPort, _ := splitAddr(proxy)
	jumped := e.addSite(server, spec{
		Name: "Jumped", Jump: jumpSpec{Host: jumpHost, Port: jumpPort, Username: "bastion", AuthMethod: "password"},
		Proxy: proxySpec{Kind: "socks5", Host: proxyHost, Port: proxyPort},
	}, changes{Password: text(sshPassword), JumpPassword: text("let-me-through")})
	fsys, err = c.Connect(t.Context(), e.key(jumped), session)
	if err != nil {
		t.Fatal(err)
	}
	_ = fsys.Close()
	entries = sink.take()
	if got := steps(entries); got != "jump:connecting jump:connected jump:host_key_verified jump:authenticated connecting host_key_verified authenticated" {
		t.Errorf("steps through a jump host = %q", got)
	}
	if entries[0].Params["via"] != "proxy" || entries[0].Params["proxy"] != proxy || entries[3].Params["username"] != "bastion" || entries[4].Params["via"] != "jump" {
		t.Errorf("jump entries = %+v %+v %+v", entries[0], entries[3], entries[4])
	}

	const ftpLogger, ftpSecret = "logger", "ftp secret LOGCHECK"
	accounts := map[string]string{ftpLogger: ftpSecret}
	plain := ftptest.Start(t, ftptest.Options{Users: accounts})
	optional := e.addSiteAt(plain.Addr, spec{Name: "Optional", Protocol: "ftp", FTPEncryption: "optional", Username: ftpLogger}, changes{Password: text(ftpSecret)})
	if fsys, err := c.Connect(t.Context(), e.key(optional), session); err != nil {
		t.Fatalf("Connect() with optional encryption = %v", err)
	} else {
		_ = fsys.Close()
	}
	if entries := sink.take(); steps(entries) != "connecting connected plain_text authenticated" || entries[2].Level != connlog.Warn {
		t.Errorf("steps of FTP without TLS = %q", steps(entries))
	}
	explicit := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Explicit, Users: accounts})
	e.trustCertificate(explicit.Addr, explicit.Fingerprint)
	secure := e.addSiteAt(explicit.Addr, spec{Name: "Explicit", Protocol: "ftp", FTPEncryption: "explicit", Username: ftpLogger}, changes{Password: text(ftpSecret)})
	if fsys, err := c.Connect(t.Context(), e.key(secure), session); err != nil {
		t.Fatalf("Connect() with TLS = %v", err)
	} else {
		_ = fsys.Close()
	}
	if got := steps(sink.take()); got != "connecting connected host_key_verified authenticated" {
		t.Errorf("steps of FTP with TLS = %q", got)
	}

	if len(sink.all) < 20 {
		t.Fatalf("only %d entries were published", len(sink.all))
	}
	for _, event := range sink.all {
		raw, err := json.Marshal(event.Data)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{sshPassword, "let-me-through", ftpSecret} {
			if strings.Contains(string(raw), secret) {
				t.Errorf("an entry holds a secret: %s", raw)
			}
		}
		if event.Type != connlog.EventType || event.UserID != e.user {
			t.Errorf("event %s for %s; want connection.log for the user", event.Type, event.UserID)
		}
	}
}
