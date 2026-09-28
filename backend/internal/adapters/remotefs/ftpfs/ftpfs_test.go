package ftpfs_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/adapters/remotefs/ftpfs"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/ftpfs/ftptest"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/remotefs"
)

const (
	testUser     = "sentinel"
	testPassword = "sentinel"
)

// tlsModes maps the encryption of a test server to the mode of the client that talks to it.
var tlsModes = map[ftptest.Encryption]ftpfs.TLSMode{
	ftptest.Plain:    ftpfs.TLSNone,
	ftptest.Explicit: ftpfs.TLSExplicit,
	ftptest.Implicit: ftpfs.TLSImplicit,
}

// trusting returns a certificate check that accepts exactly the certificate of a test server.
func trusting(s *ftptest.Server) func(keyType, fingerprint string) error {
	return func(keyType, fingerprint string) error {
		if keyType != ftpfs.CertificateKeyType || fingerprint != s.Fingerprint {
			return &remotefs.HostKeyError{Type: keyType, Fingerprint: fingerprint}
		}
		return nil
	}
}

// config builds the client configuration for a test server.
func config(s *ftptest.Server, encryption ftptest.Encryption) ftpfs.Config {
	return ftpfs.Config{
		Address:       s.Addr,
		Username:      testUser,
		Password:      testPassword,
		TLS:           tlsModes[encryption],
		VerifyHostKey: trusting(s),
		Timeout:       5 * time.Second,
	}
}

// connect dials a test server, adjusting the configuration with mutate, and closes the connection when the test ends.
func connect(t *testing.T, s *ftptest.Server, encryption ftptest.Encryption, mutate func(*ftpfs.Config)) (*ftpfs.FS, error) {
	t.Helper()
	cfg := config(s, encryption)
	if mutate != nil {
		mutate(&cfg)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fsys, err := ftpfs.Dial(ctx, cfg)
	if err == nil {
		t.Cleanup(func() { _ = fsys.Close() })
	}
	return fsys, err
}

// TestConformance runs the shared suite against every way of talking FTP.
//
// Scenarios:
//   - plain FTP, explicit FTPS and implicit FTPS, each with MLSD and MLST
//   - plain FTP against a server that only has LIST, as old servers do
func TestConformance(t *testing.T) {
	for name, opt := range map[string]ftptest.Options{
		"plain":            {Encryption: ftptest.Plain},
		"explicit ftps":    {Encryption: ftptest.Explicit},
		"implicit ftps":    {Encryption: ftptest.Implicit},
		"plain, list only": {Encryption: ftptest.Plain, DisableMLSD: true},
	} {
		t.Run(name, func(t *testing.T) {
			remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS {
				server := ftptest.Start(t, opt)
				fsys, err := connect(t, server, opt.Encryption, nil)
				if err != nil {
					t.Fatalf("Dial() = %v", err)
				}
				return fsys
			})
		})
	}
}

// TestDial_Validation verifies that bad configurations are refused before any connection is made.
//
// Scenarios:
//   - an address without a port, an empty user name and TLS without a certificate check are refused
//   - a refused connection is a retryable lost connection, and a canceled context is its own error
func TestDial_Validation(t *testing.T) {
	server := ftptest.Start(t, ftptest.Options{})

	for name, mutate := range map[string]func(*ftpfs.Config){
		"no port":              func(c *ftpfs.Config) { c.Address = "localhost" },
		"no host":              func(c *ftpfs.Config) { c.Address = ":21" },
		"no user":              func(c *ftpfs.Config) { c.Username = "" },
		"tls without a verify": func(c *ftpfs.Config) { c.TLS, c.VerifyHostKey = ftpfs.TLSExplicit, nil },
	} {
		if _, err := connect(t, server, ftptest.Plain, mutate); err == nil {
			t.Errorf("%s: Dial() = nil, want an error", name)
		}
	}

	closed := ftptest.Start(t, ftptest.Options{})
	closed.Stop()
	_, err := connect(t, closed, ftptest.Plain, nil)
	if !errors.Is(err, remotefs.ErrConnectionLost) || !remotefs.Retryable(err) {
		t.Errorf("refused connection = %v; want a retryable ErrConnectionLost", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ftpfs.Dial(ctx, config(server, ftptest.Plain)); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled context = %v; want context.Canceled", err)
	}
}

// TestDial_Silence verifies that a server that accepts the connection and says nothing cannot hang the connection.
//
// Scenarios:
//   - a listener that never greets makes Dial fail with a retryable lost connection when the timeout ends
//   - a TLS handshake that gets plain text instead, as explicit FTPS against an implicit server, fails the same way
func TestDial_Silence(t *testing.T) {
	silent, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = silent.Close() })
	go func() {
		for {
			conn, err := silent.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()

	start := time.Now()
	_, err = ftpfs.Dial(t.Context(), ftpfs.Config{Address: silent.Addr().String(), Username: testUser, Password: testPassword, Timeout: 300 * time.Millisecond})
	if !errors.Is(err, remotefs.ErrConnectionLost) || !remotefs.Retryable(err) {
		t.Errorf("Dial() to a silent server = %v; want a retryable ErrConnectionLost", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Dial() took %v; the timeout is 300ms", elapsed)
	}

	implicit := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Implicit})
	_, err = connect(t, implicit, ftptest.Explicit, func(c *ftpfs.Config) { c.Timeout = 300 * time.Millisecond })
	if !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("explicit FTPS against an implicit server = %v; want ErrConnectionLost", err)
	}
}

// TestDial_Authentication verifies how credentials are judged.
//
// Scenarios:
//   - a wrong password and an unknown user are ErrAuth, which is not retryable
//   - the right credentials work over every encryption
func TestDial_Authentication(t *testing.T) {
	for _, encryption := range []ftptest.Encryption{ftptest.Plain, ftptest.Explicit, ftptest.Implicit} {
		server := ftptest.Start(t, ftptest.Options{Encryption: encryption})
		for name, mutate := range map[string]func(*ftpfs.Config){
			"wrong password": func(c *ftpfs.Config) { c.Password = "nope" },
			"unknown user":   func(c *ftpfs.Config) { c.Username = "ghost" },
		} {
			_, err := connect(t, server, encryption, mutate)
			if !errors.Is(err, remotefs.ErrAuth) || remotefs.Retryable(err) {
				t.Errorf("encryption %d, %s: Dial() = %v; want a non-retryable ErrAuth", encryption, name, err)
			}
		}
		if _, err := connect(t, server, encryption, nil); err != nil {
			t.Errorf("encryption %d: right credentials: Dial() = %v", encryption, err)
		}
	}
}

// TestDial_Certificates verifies that a certificate is pinned like an SSH host key.
//
// Scenarios:
//   - the check receives the certificate key type and the SHA-256 fingerprint of the certificate the server presents
//   - a certificate the check refuses stops the connection with the check's own error, for both FTPS modes
//   - a server that cannot do TLS is refused when TLS is asked for, and never sent the password
func TestDial_Certificates(t *testing.T) {
	for _, encryption := range []ftptest.Encryption{ftptest.Explicit, ftptest.Implicit} {
		server := ftptest.Start(t, ftptest.Options{Encryption: encryption})
		var seenType, seenFingerprint string
		_, err := connect(t, server, encryption, func(c *ftpfs.Config) {
			c.VerifyHostKey = func(keyType, fingerprint string) error {
				seenType, seenFingerprint = keyType, fingerprint
				return &remotefs.HostKeyError{Type: keyType, Fingerprint: fingerprint, Changed: true}
			}
		})
		var rejected *remotefs.HostKeyError
		if !errors.As(err, &rejected) || !errors.Is(err, remotefs.ErrHostKeyMismatch) {
			t.Fatalf("encryption %d: refused certificate = %v; want the check's HostKeyError", encryption, err)
		}
		if seenType != ftpfs.CertificateKeyType || seenFingerprint != server.Fingerprint || rejected.Fingerprint != server.Fingerprint {
			t.Errorf("encryption %d: check saw %q %q; want %q %q", encryption, seenType, seenFingerprint, ftpfs.CertificateKeyType, server.Fingerprint)
		}
	}

	plain := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Plain})
	if _, err := connect(t, plain, ftptest.Explicit, nil); err == nil {
		t.Error("explicit TLS against a server without TLS connected")
	}
}

// TestDial_Optional covers [ftpfs.TLSOptional].
//
// Use cases:
//   - a server without TLS refuses AUTH TLS, and the connection carries on in clear text and works
//   - a server with explicit TLS upgrades, so its certificate is checked
//   - an implicit TLS server, which speaks TLS from the first byte, is not reached, and the failure is not hidden
func TestDial_Optional(t *testing.T) {
	optional := func(c *ftpfs.Config) { c.TLS = ftpfs.TLSOptional }
	plain := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Plain})
	fsys, err := connect(t, plain, ftptest.Plain, optional)
	if err != nil {
		t.Fatalf("optional TLS against a server without TLS = %v; want clear text", err)
	}
	if _, err := fsys.List(t.Context(), "/"); err != nil {
		t.Errorf("List() in clear text = %v", err)
	}

	explicit := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Explicit})
	checked := false
	if _, err := connect(t, explicit, ftptest.Explicit, func(c *ftpfs.Config) {
		c.TLS = ftpfs.TLSOptional
		check := c.VerifyHostKey
		c.VerifyHostKey = func(keyType, fingerprint string) error {
			checked = true
			return check(keyType, fingerprint)
		}
	}); err != nil {
		t.Fatalf("optional TLS against an explicit TLS server = %v", err)
	}
	if !checked {
		t.Error("the certificate of a server that offers TLS was not checked")
	}

	implicit := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Implicit})
	if _, err := connect(t, implicit, ftptest.Implicit, optional); err == nil {
		t.Error("optional TLS reached an implicit TLS server")
	}
}

// TestDial_Destinations verifies that every connection goes through the dialer that is given, and only to the control host.
//
// Scenarios:
//   - the control connection and each data connection are opened by the dialer of the configuration
//   - a server that announces another address in PASV is not followed: data connections go to the control host
//   - a dialer that refuses, as the network guard does, stops the connection with its own error
func TestDial_Destinations(t *testing.T) {
	server := ftptest.Start(t, ftptest.Options{PublicHost: "192.0.2.10"})
	var mu sync.Mutex
	var dialed []string
	fsys, err := connect(t, server, ftptest.Plain, func(c *ftpfs.Config) {
		c.DisableEPSV = true
		c.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			mu.Lock()
			dialed = append(dialed, address)
			mu.Unlock()
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
	})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	if _, err := fsys.List(t.Context(), "/"); err != nil {
		t.Fatalf("List() = %v; the announced address must not be used", err)
	}
	host, _, _ := net.SplitHostPort(server.Addr)
	mu.Lock()
	defer mu.Unlock()
	if len(dialed) < 2 {
		t.Fatalf("dialed %v; want the control and a data connection", dialed)
	}
	for _, address := range dialed {
		if got, _, _ := net.SplitHostPort(address); got != host {
			t.Errorf("dialed %s; every connection must go to the control host %s", address, host)
		}
	}

	blocked := netguard.NewPolicy(nil, nil)
	_, err = connect(t, server, ftptest.Plain, func(c *ftpfs.Config) { c.DialContext = blocked.Dialer(time.Second).DialContext })
	if !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("guarded dialer = %v; want netguard.ErrBlocked", err)
	}
}

// TestFS_HostileNames verifies that paths cannot smuggle commands into the control connection.
//
// Scenarios:
//   - a path with a line break, a carriage return or NUL, or that is not absolute, is ErrInvalid in every operation
//   - nothing is created on the server by such a path, and the connection keeps working
func TestFS_HostileNames(t *testing.T) {
	server := ftptest.Start(t, ftptest.Options{})
	fsys, err := connect(t, server, ftptest.Plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	for _, bad := range []string{"/a\r\nDELE /keep", "/a\nb", "/a\rb", "/a\x00b", "relative", ""} {
		operations := map[string]error{
			"List":      second(fsys.List(ctx, bad)),
			"Stat":      second(fsys.Stat(ctx, bad)),
			"OpenRead":  second(fsys.OpenRead(ctx, bad, 0)),
			"OpenWrite": second(fsys.OpenWrite(ctx, bad, 0, remotefs.WriteOverwrite)),
			"Mkdir":     fsys.Mkdir(ctx, bad),
			"Remove":    fsys.Remove(ctx, bad),
			"RemoveAll": fsys.RemoveAll(ctx, bad),
			"Rename":    fsys.Rename(ctx, "/keep", bad),
			"Rename2":   fsys.Rename(ctx, bad, "/x"),
			"Chtimes":   fsys.Chtimes(ctx, bad, time.Now()),
		}
		for name, err := range operations {
			if !errors.Is(err, remotefs.ErrInvalid) {
				t.Errorf("%s(%q) = %v; want ErrInvalid", name, bad, err)
			}
		}
	}
	if err := fsys.Ping(ctx); err != nil {
		t.Errorf("Ping after hostile names = %v", err)
	}
	if entries, err := fsys.List(ctx, "/"); err != nil || len(entries) != 0 {
		t.Errorf("List(/) = %v, %v; want an untouched empty root", entries, err)
	}
}

// second returns the error of a call that also returns a value.
func second[T any](_ T, err error) error { return err }
