package connect_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/remotefs/connect"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/ftpfs"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/ftpfs/ftptest"
	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/quick"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/site"
)

const ftpUser, ftpPassword = "sentinel", "sentinel"

// trustCertificate records the certificate of an FTPS test server as trusted for its address.
func (e *env) trustCertificate(address, fingerprint string) {
	e.t.Helper()
	host, port, _ := splitAddr(address)
	if _, err := e.hostKeys.Trust(e.t.Context(), e.user, e.workspace, host, port, ftpfs.CertificateKeyType, fingerprint); err != nil {
		e.t.Fatalf("Trust() = %v", err)
	}
}

// TestConnector_FTP verifies connecting to FTP and FTPS sites.
//
// Scenarios:
//   - a plain FTP site connects with its stored password, and asks for no key
//   - an FTP account with an empty password connects without a stored secret
//   - an FTPS site reports the certificate it met, with its fingerprint under the certificate key type, and connects once trusted
//   - a certificate other than the trusted one is refused as a mismatch
//   - an FTPS site on an implicit port starts with TLS, and one on another port upgrades with AUTH TLS
//   - a wrong password is ErrAuth and the destination guard applies to FTP too
func TestConnector_FTP(t *testing.T) {
	e := newEnv(t)
	c := e.connector(e.allow)

	plain := ftptest.Start(t, ftptest.Options{})
	target := e.addSiteAt(plain.Addr, site.Input{Name: "Plain", Protocol: site.FTP, Username: ftpUser}, site.SecretChanges{Password: text(ftpPassword)})
	fsys, err := c.Connect(t.Context(), e.key(target), session)
	if err != nil {
		t.Fatalf("Connect() to an FTP site = %v", err)
	}
	if _, err := fsys.List(t.Context(), "/"); err != nil {
		t.Errorf("List() = %v", err)
	}
	_ = fsys.Close()

	public := ftptest.Start(t, ftptest.Options{Users: map[string]string{"anonymous": ""}})
	anonymous := e.addSiteAt(public.Addr, site.Input{Name: "Public", Protocol: site.FTP, Username: "anonymous"}, site.SecretChanges{})
	if fsys, err := c.Connect(t.Context(), e.key(anonymous), session); err != nil {
		t.Errorf("Connect() to an anonymous FTP site = %v", err)
	} else {
		_ = fsys.Close()
	}

	wrong := e.addSiteAt(plain.Addr, site.Input{Name: "Wrong", Protocol: site.FTP, Username: ftpUser}, site.SecretChanges{Password: text("nope")})
	if _, err := c.Connect(t.Context(), e.key(wrong), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() with a wrong password = %v; want ErrAuth", err)
	}

	explicit := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Explicit})
	secure := e.addSiteAt(explicit.Addr, site.Input{Name: "Explicit", Protocol: site.FTPS, Username: ftpUser}, site.SecretChanges{Password: text(ftpPassword)})
	_, err = c.Connect(t.Context(), e.key(secure), session)
	var unknown *remotefs.HostKeyError
	if !errors.Is(err, remotefs.ErrHostKeyUnknown) || !errors.As(err, &unknown) || unknown.Type != ftpfs.CertificateKeyType || unknown.Fingerprint != explicit.Fingerprint {
		t.Fatalf("first Connect() to an FTPS site = %v; want the unknown certificate with the server fingerprint", err)
	}
	e.trustCertificate(explicit.Addr, explicit.Fingerprint)
	if fsys, err := c.Connect(t.Context(), e.key(secure), session); err != nil {
		t.Errorf("Connect() to an FTPS site after trusting = %v", err)
	} else {
		_ = fsys.Close()
	}

	impostor := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Explicit})
	other := e.addSiteAt(impostor.Addr, site.Input{Name: "Impostor", Protocol: site.FTPS, Username: ftpUser}, site.SecretChanges{Password: text(ftpPassword)})
	e.trustCertificate(impostor.Addr, explicit.Fingerprint)
	if _, err := c.Connect(t.Context(), e.key(other), session); !errors.Is(err, remotefs.ErrHostKeyMismatch) {
		t.Errorf("Connect() to a server with another certificate = %v; want ErrHostKeyMismatch", err)
	}

	implicit := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Implicit})
	_, implicitPort, _ := splitAddr(implicit.Addr)
	implicitSite := e.addSiteAt(implicit.Addr, site.Input{Name: "Implicit", Protocol: site.FTPS, Username: ftpUser}, site.SecretChanges{Password: text(ftpPassword)})
	e.trustCertificate(implicit.Addr, implicit.Fingerprint)
	if _, err := c.Connect(t.Context(), e.key(implicitSite), session); err == nil {
		t.Error("an implicit FTPS server was reached with an explicit connection")
	}
	implicitConnector := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.ImplicitFTPSPorts(implicitPort))
	if fsys, err := implicitConnector.Connect(t.Context(), e.key(implicitSite), session); err != nil {
		t.Errorf("Connect() to an implicit FTPS port = %v", err)
	} else {
		_ = fsys.Close()
	}

	blocked := e.connector(netguard.NewPolicy(nil, nil))
	if _, err := blocked.Connect(t.Context(), e.key(target), session); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("Connect() to a forbidden FTP destination = %v; want netguard.ErrBlocked", err)
	}
}

// TestConnector_QuickFTP verifies quick connections over FTP and FTPS.
//
// Scenarios:
//   - an FTP quick connection connects with its password, and one without a password connects too, as an anonymous account does
//   - an FTPS quick connection asks to trust the certificate first, in memory only
func TestConnector_QuickFTP(t *testing.T) {
	e := newEnv(t)
	c := e.connector(e.allow)
	start := func(address string, protocol site.Protocol, secrets quick.Secrets, user string) uuid.UUID {
		t.Helper()
		host, port, _ := splitAddr(address)
		created, err := e.quick.Create(t.Context(), e.user, e.workspace, site.Input{Protocol: protocol, Host: host, Port: port, Username: user}, secrets)
		if err != nil {
			t.Fatalf("Create() = %v", err)
		}
		return created.Site.ID
	}
	key := func(id uuid.UUID) connections.Key {
		return connections.Key{UserID: e.user, WorkspaceID: e.workspace, SiteID: id}
	}

	plain := ftptest.Start(t, ftptest.Options{Users: map[string]string{ftpUser: ftpPassword, "anonymous": ""}})
	withPassword := start(plain.Addr, site.FTP, quick.Secrets{Password: []byte(ftpPassword)}, ftpUser)
	fsys, err := c.Connect(t.Context(), key(withPassword), "no-vault-session")
	if err != nil {
		t.Fatalf("Connect() to an FTP quick connection = %v", err)
	}
	_ = fsys.Close()
	anonymous := start(plain.Addr, site.FTP, quick.Secrets{}, "anonymous")
	if fsys, err := c.Connect(t.Context(), key(anonymous), "no-vault-session"); err != nil {
		t.Errorf("Connect() to an anonymous quick connection = %v", err)
	} else {
		_ = fsys.Close()
	}

	secure := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Explicit})
	ftps := start(secure.Addr, site.FTPS, quick.Secrets{Password: []byte(ftpPassword)}, ftpUser)
	if _, err := c.Connect(t.Context(), key(ftps), "no-vault-session"); !errors.Is(err, remotefs.ErrHostKeyUnknown) {
		t.Errorf("Connect() before trusting = %v; want ErrHostKeyUnknown", err)
	}
	e.trustCertificate(secure.Addr, secure.Fingerprint)
	if fsys, err := c.Connect(t.Context(), key(ftps), "no-vault-session"); err != nil {
		t.Errorf("Connect() after trusting = %v", err)
	} else {
		_ = fsys.Close()
	}
}
