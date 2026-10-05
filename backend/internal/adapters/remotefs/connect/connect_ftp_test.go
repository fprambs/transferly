package connect_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/remotefs/ftpfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/ftpfs/ftptest"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/hostkey"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
)

const ftpUser, ftpPassword = "transferly", "transferly"

// trustCertificate records the certificate of an FTPS test server as trusted for its address.
func (e *env) trustCertificate(address, fingerprint string) {
	e.t.Helper()
	host, port, _ := splitAddr(address)
	if _, err := e.hostKeys.Trust(e.t.Context(), e.user, e.workspace, host, port, ftpfs.CertificateKeyType, fingerprint, hostkey.TrustNew); err != nil {
		e.t.Fatalf("Trust() = %v", err)
	}
}

// TestConnector_FTP verifies connecting to FTP and FTPS sites.
//
// Scenarios:
//   - a plain FTP site connects with its stored password, and asks for no key
//   - an FTP account with an empty password connects without a stored secret, and an anonymous site answers FileZilla's
//     anonymous password
//   - an FTPS site reports the certificate it met, with its fingerprint under the certificate key type, and connects once trusted
//   - a certificate other than the trusted one is refused as a mismatch
//   - an FTPS connection keeps verifying the certificate of its data connections after the request that opened it ended
//   - a legacy FTPS site off port 990 upgrades with AUTH TLS, and a site with implicit encryption starts with TLS on any port
//   - optional encryption carries on in clear text with a server that does not offer TLS and uses TLS with one that does,
//     while required explicit encryption refuses a server that does not offer it
//   - a wrong password is ErrAuth and the destination guard applies to FTP too
func TestConnector_FTP(t *testing.T) {
	e := newEnv(t)
	c := e.connector(e.allow)

	plain := ftptest.Start(t, ftptest.Options{})
	target := e.addSiteAt(plain.Addr, spec{Name: "Plain", Protocol: "ftp", Username: ftpUser}, changes{Password: text(ftpPassword)})
	fsys, err := c.Connect(t.Context(), e.key(target), session)
	if err != nil {
		t.Fatalf("Connect() to an FTP site = %v", err)
	}
	if _, err := fsys.List(t.Context(), "/"); err != nil {
		t.Errorf("List() = %v", err)
	}
	_ = fsys.Close()

	public := ftptest.Start(t, ftptest.Options{Users: map[string]string{"anonymous": ""}})
	anonymous := e.addSiteAt(public.Addr, spec{Name: "Public", Protocol: "ftp", Username: "anonymous"}, changes{})
	if fsys, err := c.Connect(t.Context(), e.key(anonymous), session); err != nil {
		t.Errorf("Connect() to an anonymous FTP site = %v", err)
	} else {
		_ = fsys.Close()
	}

	guests := ftptest.Start(t, ftptest.Options{Users: map[string]string{"anonymous": "anonymous@example.com"}})
	guest := e.addSiteAt(guests.Addr, spec{Name: "Guests", Protocol: "ftp", AuthMethod: "anonymous"}, changes{})
	if fsys, err := c.Connect(t.Context(), e.key(guest), session); err != nil {
		t.Errorf("Connect() to an FTP site of the anonymous logon type = %v", err)
	} else {
		_ = fsys.Close()
	}

	wrong := e.addSiteAt(plain.Addr, spec{Name: "Wrong", Protocol: "ftp", Username: ftpUser}, changes{Password: text("nope")})
	if _, err := c.Connect(t.Context(), e.key(wrong), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() with a wrong password = %v; want ErrAuth", err)
	}

	explicit := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Explicit})
	secure := e.addSiteAt(explicit.Addr, spec{Name: "Explicit", Protocol: "ftps", Username: ftpUser}, changes{Password: text(ftpPassword)})
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
	dialing, stop := context.WithCancel(t.Context())
	connected, err := c.Connect(dialing, e.key(secure), session)
	if err != nil {
		t.Fatalf("Connect() to an FTPS site = %v", err)
	}
	stop()
	if _, err := connected.List(t.Context(), "/"); err != nil {
		t.Errorf("List() once the request that connected ended = %v; want the data connection verified anyway", err)
	}
	_ = connected.Close()

	impostor := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Explicit})
	other := e.addSiteAt(impostor.Addr, spec{Name: "Impostor", Protocol: "ftps", Username: ftpUser}, changes{Password: text(ftpPassword)})
	e.trustCertificate(impostor.Addr, explicit.Fingerprint)
	if _, err := c.Connect(t.Context(), e.key(other), session); !errors.Is(err, remotefs.ErrHostKeyMismatch) {
		t.Errorf("Connect() to a server with another certificate = %v; want ErrHostKeyMismatch", err)
	}

	implicit := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Implicit})
	legacy := e.addSiteAt(implicit.Addr, spec{Name: "Legacy FTPS", Protocol: "ftps", Username: ftpUser}, changes{Password: text(ftpPassword)})
	e.trustCertificate(implicit.Addr, implicit.Fingerprint)
	if _, err := c.Connect(t.Context(), e.key(legacy), session); err == nil {
		t.Error("an implicit FTPS server was reached with the explicit connection a legacy FTPS site off port 990 means")
	}
	implicitSite := e.addSiteAt(implicit.Addr, spec{Name: "Implicit", Protocol: "ftp", FTPEncryption: "implicit", Username: ftpUser},
		changes{Password: text(ftpPassword)})
	if fsys, err := c.Connect(t.Context(), e.key(implicitSite), session); err != nil {
		t.Errorf("Connect() to a site with implicit encryption on any port = %v", err)
	} else {
		_ = fsys.Close()
	}

	optionalPlain := e.addSiteAt(plain.Addr, spec{Name: "Optional plain", Protocol: "ftp", FTPEncryption: "optional", Username: ftpUser},
		changes{Password: text(ftpPassword)})
	if fsys, err := c.Connect(t.Context(), e.key(optionalPlain), session); err != nil {
		t.Errorf("Connect() with optional encryption to a server without TLS = %v; want it to carry on in clear text", err)
	} else {
		_ = fsys.Close()
	}
	optionalTLS := e.addSiteAt(explicit.Addr, spec{Name: "Optional TLS", Protocol: "ftp", FTPEncryption: "optional", Username: ftpUser},
		changes{Password: text(ftpPassword)})
	if fsys, err := c.Connect(t.Context(), e.key(optionalTLS), session); err != nil {
		t.Errorf("Connect() with optional encryption to a server with TLS = %v", err)
	} else {
		_ = fsys.Close()
	}
	required := e.addSiteAt(plain.Addr, spec{Name: "Required", Protocol: "ftp", FTPEncryption: "explicit", Username: ftpUser},
		changes{Password: text(ftpPassword)})
	if _, err := c.Connect(t.Context(), e.key(required), session); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Connect() requiring TLS from a server without it = %v; want ErrInvalid", err)
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
	start := func(address, protocol string, secrets qsecrets, user string) uuid.UUID {
		t.Helper()
		host, port, _ := splitAddr(address)
		created, err := e.quick.Create(t.Context(), e.user, e.workspace, spec{Protocol: protocol, Host: host, Port: port, Username: user}.input(), secrets.secrets())
		if err != nil {
			t.Fatalf("Create() = %v", err)
		}
		return created.Site.ID
	}
	key := func(id uuid.UUID) connections.Key {
		return connections.Key{UserID: e.user, WorkspaceID: e.workspace, SiteID: id}
	}

	plain := ftptest.Start(t, ftptest.Options{Users: map[string]string{ftpUser: ftpPassword, "anonymous": ""}})
	withPassword := start(plain.Addr, "ftp", qsecrets{Password: []byte(ftpPassword)}, ftpUser)
	fsys, err := c.Connect(t.Context(), key(withPassword), "no-vault-session")
	if err != nil {
		t.Fatalf("Connect() to an FTP quick connection = %v", err)
	}
	_ = fsys.Close()
	anonymous := start(plain.Addr, "ftp", qsecrets{}, "anonymous")
	if fsys, err := c.Connect(t.Context(), key(anonymous), "no-vault-session"); err != nil {
		t.Errorf("Connect() to an anonymous quick connection = %v", err)
	} else {
		_ = fsys.Close()
	}

	secure := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Explicit})
	ftps := start(secure.Addr, "ftps", qsecrets{Password: []byte(ftpPassword)}, ftpUser)
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

// TestConnector_FTPAccount verifies FTP sites of the account logon type (4.8e).
//
// Scenarios:
//   - a site with a password and an account connects to a server that does not ask for the account
//   - an account that cannot be opened, because the vault is locked in the session, fails the connection
func TestConnector_FTPAccount(t *testing.T) {
	e := newEnv(t)
	c := e.connector(e.allow)
	plain := ftptest.Start(t, ftptest.Options{})

	billed := e.addSiteAt(plain.Addr, spec{Name: "Billed", Protocol: "ftp", Username: ftpUser, AuthMethod: "account"}, changes{Password: text(ftpPassword), Account: text("billing")})
	fsys, err := c.Connect(t.Context(), e.key(billed), session)
	if err != nil {
		t.Fatalf("Connect() to an FTP site of the account logon type = %v", err)
	}
	_ = fsys.Close()

	only := e.addSiteAt(plain.Addr, spec{Name: "Account only", Protocol: "ftp", Username: ftpUser, AuthMethod: "account"}, changes{Account: text("billing")})
	if _, err := c.Connect(t.Context(), e.key(only), "locked-session"); !errors.Is(err, site.ErrLocked) {
		t.Errorf("Connect() with the vault locked = %v; want ErrLocked", err)
	}
}
