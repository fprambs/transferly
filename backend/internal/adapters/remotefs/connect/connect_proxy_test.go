package connect_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/fprambs/sentinel/internal/adapters/remotefs/ftpfs/ftptest"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/quick"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/site"
)

// socks5TestProxy starts a minimal SOCKS5 proxy, independent of the client under test, that relays one connection to
// whatever address it is asked to CONNECT to. auth, when set, is the username/password it demands.
func socks5TestProxy(t *testing.T, auth *[2]string) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go relaySOCKS5(conn, auth)
		}
	}()
	return ln.Addr().String()
}

// relaySOCKS5 runs a bare SOCKS5 handshake over conn and relays the connection to whatever it is asked to CONNECT to.
func relaySOCKS5(conn net.Conn, auth *[2]string) {
	defer func() { _ = conn.Close() }()
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	method := byte(0x00)
	if auth != nil {
		method = 0x02
	}
	if _, err := conn.Write([]byte{0x05, method}); err != nil {
		return
	}
	if auth != nil {
		head := make([]byte, 2)
		if _, err := io.ReadFull(conn, head); err != nil {
			return
		}
		user := make([]byte, head[1])
		if _, err := io.ReadFull(conn, user); err != nil {
			return
		}
		plen := make([]byte, 1)
		if _, err := io.ReadFull(conn, plen); err != nil {
			return
		}
		pass := make([]byte, plen[0])
		if _, err := io.ReadFull(conn, pass); err != nil {
			return
		}
		ok := string(user) == auth[0] && string(pass) == auth[1]
		status := byte(0x00)
		if !ok {
			status = 0x01
		}
		if _, err := conn.Write([]byte{0x01, status}); err != nil || !ok {
			return
		}
	}
	reqHead := make([]byte, 4)
	if _, err := io.ReadFull(conn, reqHead); err != nil {
		return
	}
	var host string
	switch reqHead[3] {
	case 0x01:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return
		}
		host = net.IP(ip).String()
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return
		}
		name := make([]byte, length[0])
		if _, err := io.ReadFull(conn, name); err != nil {
			return
		}
		host = string(name)
	default:
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		return
	}
	port := int(portBytes[0])<<8 | int(portBytes[1])
	target, err := (&net.Dialer{}).Dial("tcp", net.JoinHostPort(host, itoa(port)))
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer func() { _ = target.Close() }()
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	go func() { _, _ = io.Copy(target, conn) }()
	_, _ = io.Copy(conn, target)
}

// itoa converts a non-negative int to its decimal text.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [6]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// jumpTestHost starts a minimal SSH server that authenticates with username and password and forwards every
// "direct-tcpip" channel it is asked to open to the real address the client names, standing in for a real jump host.
// It returns its address and its host key, which the caller trusts like any other host's.
func jumpTestHost(t *testing.T, username, password string) (address string, hostKey ssh.PublicKey) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if conn.User() == username && string(pass) == password {
				return nil, nil
			}
			return nil, errors.New("wrong credentials")
		},
	}
	config.AddHostKey(signer)
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go forwardChannels(conn, config)
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

// forwardChannels completes the SSH handshake on conn and relays every "direct-tcpip" channel to its named address.
func forwardChannels(conn net.Conn, config *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer func() { _ = sconn.Close() }()
	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		if newChannel.ChannelType() != "direct-tcpip" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only direct-tcpip is forwarded")
			continue
		}
		var dest struct {
			Addr       string
			Port       uint32
			OriginAddr string
			OriginPort uint32
		}
		if err := ssh.Unmarshal(newChannel.ExtraData(), &dest); err != nil {
			_ = newChannel.Reject(ssh.ConnectionFailed, "malformed request")
			continue
		}
		address := net.JoinHostPort(dest.Addr, itoa(int(dest.Port)))
		target, err := (&net.Dialer{}).Dial("tcp", address)
		if err != nil {
			_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			_ = target.Close()
			continue
		}
		go ssh.DiscardRequests(requests)
		go relayChannel(channel, target)
	}
}

// relayChannel dials address and copies bytes between it and channel until either side closes.
func relayChannel(channel ssh.Channel, target net.Conn) {
	defer func() { _ = channel.Close() }()
	defer func() { _ = target.Close() }()
	go func() { _, _ = io.Copy(target, channel); _ = target.Close() }()
	_, _ = io.Copy(channel, target)
}

// TestConnector_Proxy verifies connecting through a SOCKS5 proxy, for both SFTP and FTP sites.
//
// Scenarios:
//   - an SFTP site behind a SOCKS5 proxy without authentication connects and lists files
//   - one behind a proxy that demands a username and password connects with its stored proxy password
//   - a wrong proxy password fails the connection
//   - an FTP site behind a proxy connects too, since the proxy only replaces the raw TCP dial
func TestConnector_Proxy(t *testing.T) {
	e := newEnv(t)
	c := e.connector(e.allow)

	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	e.trust(server)
	proxy := socks5TestProxy(t, nil)
	proxyHost, proxyPort, _ := splitAddr(proxy)
	direct := e.addSite(server, site.Input{Name: "Direct", Proxy: site.Proxy{Kind: site.ProxySOCKS5, Host: proxyHost, Port: proxyPort}}, site.SecretChanges{Password: text(sshPassword)})
	fsys, err := c.Connect(t.Context(), e.key(direct), session)
	if err != nil {
		t.Fatalf("Connect() through a SOCKS5 proxy = %v", err)
	}
	if _, err := fsys.List(t.Context(), "/"); err != nil {
		t.Errorf("List() through a SOCKS5 proxy = %v", err)
	}
	_ = fsys.Close()

	authProxy := socks5TestProxy(t, &[2]string{"proxyuser", "proxypass"})
	authHost, authPort, _ := splitAddr(authProxy)
	authenticated := e.addSite(server, site.Input{
		Name: "Authenticated", Proxy: site.Proxy{Kind: site.ProxySOCKS5, Host: authHost, Port: authPort, Username: "proxyuser"},
	}, site.SecretChanges{Password: text(sshPassword), ProxyPassword: text("proxypass")})
	if fsys, err := c.Connect(t.Context(), e.key(authenticated), session); err != nil {
		t.Errorf("Connect() through an authenticated proxy = %v", err)
	} else {
		_ = fsys.Close()
	}

	wrongProxy := e.addSite(server, site.Input{
		Name: "Wrong proxy password", Proxy: site.Proxy{Kind: site.ProxySOCKS5, Host: authHost, Port: authPort, Username: "proxyuser"},
	}, site.SecretChanges{Password: text(sshPassword), ProxyPassword: text("not it")})
	if _, err := c.Connect(t.Context(), e.key(wrongProxy), session); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Connect() with a wrong proxy password = %v; want ErrConnectionLost", err)
	}

	plain := ftptest.Start(t, ftptest.Options{})
	ftpProxy := socks5TestProxy(t, nil)
	ftpProxyHost, ftpProxyPort, _ := splitAddr(ftpProxy)
	ftpSite := e.addSiteAt(plain.Addr, site.Input{
		Name: "FTP via proxy", Protocol: site.FTP, Username: ftpUser, Proxy: site.Proxy{Kind: site.ProxySOCKS5, Host: ftpProxyHost, Port: ftpProxyPort},
	}, site.SecretChanges{Password: text(ftpPassword)})
	if fsys, err := c.Connect(t.Context(), e.key(ftpSite), session); err != nil {
		t.Errorf("Connect() to an FTP site through a proxy = %v", err)
	} else {
		_ = fsys.Close()
	}
}

// TestConnector_Jump verifies connecting to an SFTP site through a jump host.
//
// Scenarios:
//   - a jump host authenticating with a password reaches the site, which lists files, and closing it closes both
//     connections
//   - one authenticating with a private key and its passphrase works too
//   - a site with no jump credentials stored is ErrAuth, so is one with a key jump host and no key stored
//   - a wrong jump password fails the connection
//   - a jump host reached through a proxy works too, and one is refused for a protocol other than SFTP
func TestConnector_Jump(t *testing.T) {
	e := newEnv(t)
	c := e.connector(e.allow)
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	e.trust(server)

	jump, jumpKey := jumpTestHost(t, "bastion", "let-me-through")
	jumpHost, jumpPort, _ := splitAddr(jump)
	if _, err := e.hostKeys.Trust(t.Context(), e.user, e.workspace, jumpHost, jumpPort, jumpKey.Type(), ssh.FingerprintSHA256(jumpKey)); err != nil {
		t.Fatalf("Trust() of the jump host = %v", err)
	}
	viaJump := e.addSite(server, site.Input{
		Name: "Via jump", Jump: site.Jump{Host: jumpHost, Port: jumpPort, Username: "bastion", AuthMethod: site.AuthPassword},
	}, site.SecretChanges{Password: text(sshPassword), JumpPassword: text("let-me-through")})
	fsys, err := c.Connect(t.Context(), e.key(viaJump), session)
	if err != nil {
		t.Fatalf("Connect() through a jump host = %v", err)
	}
	if _, err := fsys.List(t.Context(), "/"); err != nil {
		t.Errorf("List() through a jump host = %v", err)
	}
	if err := fsys.Close(); err != nil {
		t.Errorf("Close() of a jumped connection = %v", err)
	}

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyJump, keyJumpHostKey := jumpTestHostWithKey(t, "bastion", private)
	keyJumpHost, keyJumpPort, _ := splitAddr(keyJump)
	if _, err := e.hostKeys.Trust(t.Context(), e.user, e.workspace, keyJumpHost, keyJumpPort, keyJumpHostKey.Type(), ssh.FingerprintSHA256(keyJumpHostKey)); err != nil {
		t.Fatalf("Trust() of the key jump host = %v", err)
	}
	pemKey, passphrase := marshalKey(t, private, "open sesame")
	viaKeyJump := e.addSite(server, site.Input{
		Name: "Via key jump", Jump: site.Jump{Host: keyJumpHost, Port: keyJumpPort, Username: "bastion", AuthMethod: site.AuthKey},
	}, site.SecretChanges{Password: text(sshPassword), JumpPrivateKey: text(pemKey), JumpPassphrase: text(passphrase)})
	if fsys, err := c.Connect(t.Context(), e.key(viaKeyJump), session); err != nil {
		t.Errorf("Connect() through a key jump host = %v", err)
	} else {
		_ = fsys.Close()
	}

	noJumpSecret := e.addSite(server, site.Input{
		Name: "No jump secret", Jump: site.Jump{Host: jumpHost, Port: jumpPort, Username: "bastion", AuthMethod: site.AuthPassword},
	}, site.SecretChanges{Password: text(sshPassword)})
	if _, err := c.Connect(t.Context(), e.key(noJumpSecret), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() with no jump password stored = %v; want ErrAuth", err)
	}
	noJumpKey := e.addSite(server, site.Input{
		Name: "No jump key", Jump: site.Jump{Host: keyJumpHost, Port: keyJumpPort, Username: "bastion", AuthMethod: site.AuthKey},
	}, site.SecretChanges{Password: text(sshPassword)})
	if _, err := c.Connect(t.Context(), e.key(noJumpKey), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() with no jump key stored = %v; want ErrAuth", err)
	}

	wrongJumpPassword := e.addSite(server, site.Input{
		Name: "Wrong jump password", Jump: site.Jump{Host: jumpHost, Port: jumpPort, Username: "bastion", AuthMethod: site.AuthPassword},
	}, site.SecretChanges{Password: text(sshPassword), JumpPassword: text("nope")})
	if _, err := c.Connect(t.Context(), e.key(wrongJumpPassword), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() with a wrong jump password = %v; want ErrAuth", err)
	}

	proxy := socks5TestProxy(t, nil)
	proxyHost, proxyPort, _ := splitAddr(proxy)
	viaProxiedJump := e.addSite(server, site.Input{
		Name: "Via proxied jump", Jump: site.Jump{Host: jumpHost, Port: jumpPort, Username: "bastion", AuthMethod: site.AuthPassword},
		Proxy: site.Proxy{Kind: site.ProxySOCKS5, Host: proxyHost, Port: proxyPort},
	}, site.SecretChanges{Password: text(sshPassword), JumpPassword: text("let-me-through")})
	if fsys, err := c.Connect(t.Context(), e.key(viaProxiedJump), session); err != nil {
		t.Errorf("Connect() through a jump host reached via a proxy = %v", err)
	} else {
		_ = fsys.Close()
	}
}

// jumpTestHostWithKey is [jumpTestHost] authenticating with a public key instead of a password.
func jumpTestHostWithKey(t *testing.T, username string, private ed25519.PrivateKey) (address string, hostKey ssh.PublicKey) {
	t.Helper()
	authorized, err := ssh.NewPublicKey(private.Public())
	if err != nil {
		t.Fatal(err)
	}
	_, hostPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(hostPrivate)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if conn.User() == username && string(key.Marshal()) == string(authorized.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unauthorized key")
		},
	}
	config.AddHostKey(signer)
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go forwardChannels(conn, config)
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

// marshalKey encodes a private key, protected by passphrase, in PEM.
func marshalKey(t *testing.T, private ed25519.PrivateKey, passphrase string) (pemText, pass string) {
	t.Helper()
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block)), passphrase
}

// TestConnector_JumpEdgeCases verifies the failures and open secrets that a plain jump host connection does not
// reach: a jump host that cannot reach the site, a proxy that needs no stored password, a failing lookup of the
// jump host's trusted key types, and a quick connection whose jump host has no credentials to give it, since a
// quick connection carries none for a jump host.
//
// Scenarios:
//   - the jump host answering that it cannot reach the site is reported as a lost connection
//   - a proxy with a username but no stored password still connects, when the proxy needs no real authentication
//   - a failing lookup of the jump host's trusted key types fails the connection
//   - a quick connection with a password jump host, and one with a key jump host, are both ErrAuth without a stored
//     jump credential
func TestConnector_JumpEdgeCases(t *testing.T) {
	e := newEnv(t)
	c := e.connector(e.allow)
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	e.trust(server)
	jump, jumpKey := jumpTestHost(t, "bastion", "let-me-through")
	jumpHost, jumpPort, _ := splitAddr(jump)
	if _, err := e.hostKeys.Trust(t.Context(), e.user, e.workspace, jumpHost, jumpPort, jumpKey.Type(), ssh.FingerprintSHA256(jumpKey)); err != nil {
		t.Fatalf("Trust() of the jump host = %v", err)
	}

	closed, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	unreachable := closed.Addr().String()
	_ = closed.Close()
	unreachableSite := e.addSiteAt(unreachable, site.Input{
		Name: "Unreachable via jump", Jump: site.Jump{Host: jumpHost, Port: jumpPort, Username: "bastion", AuthMethod: site.AuthPassword},
	}, site.SecretChanges{Password: text(sshPassword), JumpPassword: text("let-me-through")})
	if _, err := c.Connect(t.Context(), e.key(unreachableSite), session); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Connect() to a site the jump host cannot reach = %v; want ErrConnectionLost", err)
	}

	noAuthProxy := socks5TestProxy(t, nil)
	noAuthHost, noAuthPort, _ := splitAddr(noAuthProxy)
	unstoredProxyPassword := e.addSite(server, site.Input{
		Name: "Proxy needs no real auth", Proxy: site.Proxy{Kind: site.ProxySOCKS5, Host: noAuthHost, Port: noAuthPort, Username: "someone"},
	}, site.SecretChanges{Password: text(sshPassword)})
	if fsys, err := c.Connect(t.Context(), e.key(unstoredProxyPassword), session); err != nil {
		t.Errorf("Connect() through a proxy with a username but no stored password = %v", err)
	} else {
		_ = fsys.Close()
	}

	flakyJumpSite := e.addSite(server, site.Input{
		Name: "Flaky jump lookup", Jump: site.Jump{Host: jumpHost, Port: jumpPort, Username: "bastion", AuthMethod: site.AuthPassword},
	}, site.SecretChanges{Password: text(sshPassword), JumpPassword: text("let-me-through")})
	e.flaky.failTrustedAt, e.flaky.trusted = 2, 0
	if _, err := c.Connect(t.Context(), e.key(flakyJumpSite), session); !errors.Is(err, errFlaky) {
		t.Errorf("Connect() with a failing lookup of the jump host's trusted keys = %v; want the store error", err)
	}
	e.flaky.failTrustedAt = 0

	host, port, _ := splitAddr(server.Addr)
	quickPassword, err := e.quick.Create(t.Context(), e.user, e.workspace, site.Input{
		Host: host, Port: port, Username: sshUser, Protocol: site.SFTP,
		Jump: site.Jump{Host: jumpHost, Port: jumpPort, Username: "bastion", AuthMethod: site.AuthPassword},
	}, quick.Secrets{Password: []byte(sshPassword)})
	if err != nil {
		t.Fatalf("quick.Create() = %v", err)
	}
	if _, err := c.Connect(t.Context(), connections.Key{UserID: e.user, WorkspaceID: e.workspace, SiteID: quickPassword.Site.ID}, "s"); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() to a quick connection with a password jump host and no jump password = %v; want ErrAuth", err)
	}
	quickKey, err := e.quick.Create(t.Context(), e.user, e.workspace, site.Input{
		Host: host, Port: port, Username: sshUser, Protocol: site.SFTP,
		Jump: site.Jump{Host: jumpHost, Port: jumpPort, Username: "bastion", AuthMethod: site.AuthKey},
	}, quick.Secrets{Password: []byte(sshPassword)})
	if err != nil {
		t.Fatalf("quick.Create() = %v", err)
	}
	if _, err := c.Connect(t.Context(), connections.Key{UserID: e.user, WorkspaceID: e.workspace, SiteID: quickKey.Site.ID}, "s"); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() to a quick connection with a key jump host and no jump key = %v; want ErrAuth", err)
	}

	_, unencrypted, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	plainKeyJump, plainKeyJumpHostKey := jumpTestHostWithKey(t, "bastion", unencrypted)
	plainKeyJumpHost, plainKeyJumpPort, _ := splitAddr(plainKeyJump)
	if _, err := e.hostKeys.Trust(t.Context(), e.user, e.workspace, plainKeyJumpHost, plainKeyJumpPort, plainKeyJumpHostKey.Type(), ssh.FingerprintSHA256(plainKeyJumpHostKey)); err != nil {
		t.Fatalf("Trust() of the plain key jump host = %v", err)
	}
	plainBlock, err := ssh.MarshalPrivateKey(unencrypted, "")
	if err != nil {
		t.Fatal(err)
	}
	viaPlainKeyJump := e.addSite(server, site.Input{
		Name: "Via unencrypted key jump", Jump: site.Jump{Host: plainKeyJumpHost, Port: plainKeyJumpPort, Username: "bastion", AuthMethod: site.AuthKey},
	}, site.SecretChanges{Password: text(sshPassword), JumpPrivateKey: text(string(pem.EncodeToMemory(plainBlock)))})
	if fsys, err := c.Connect(t.Context(), e.key(viaPlainKeyJump), session); err != nil {
		t.Errorf("Connect() through a jump host with an unencrypted key and no stored passphrase = %v", err)
	} else {
		_ = fsys.Close()
	}

	e.flaky.failKind = site.SecretProxyPassword
	proxy := socks5TestProxy(t, &[2]string{"proxyuser", "proxypass"})
	proxyHost, proxyPort, _ := splitAddr(proxy)
	flakyProxySite := e.addSite(server, site.Input{
		Name: "Flaky proxy secret", Proxy: site.Proxy{Kind: site.ProxySOCKS5, Host: proxyHost, Port: proxyPort, Username: "proxyuser"},
	}, site.SecretChanges{Password: text(sshPassword), ProxyPassword: text("proxypass")})
	if _, err := c.Connect(t.Context(), e.key(flakyProxySite), session); !errors.Is(err, errFlaky) {
		t.Errorf("Connect() with a failing read of the proxy password = %v; want the store error", err)
	}
	e.flaky.failKind = ""

	e.flaky.failKind = site.SecretJumpPassphrase
	flakyJumpPassphraseSite := e.addSite(server, site.Input{
		Name: "Flaky jump passphrase", Jump: site.Jump{Host: plainKeyJumpHost, Port: plainKeyJumpPort, Username: "bastion", AuthMethod: site.AuthKey},
	}, site.SecretChanges{Password: text(sshPassword), JumpPrivateKey: text(string(pem.EncodeToMemory(plainBlock)))})
	if _, err := c.Connect(t.Context(), e.key(flakyJumpPassphraseSite), session); !errors.Is(err, errFlaky) {
		t.Errorf("Connect() with a failing read of the jump host's passphrase = %v; want the store error", err)
	}
	e.flaky.failKind = ""

	impostor := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	untrustedViaJump := e.addSite(impostor, site.Input{
		Name: "Untrusted via jump", Jump: site.Jump{Host: jumpHost, Port: jumpPort, Username: "bastion", AuthMethod: site.AuthPassword},
	}, site.SecretChanges{Password: text(sshPassword), JumpPassword: text("let-me-through")})
	if _, err := c.Connect(t.Context(), e.key(untrustedViaJump), session); !errors.Is(err, remotefs.ErrHostKeyUnknown) {
		t.Errorf("Connect() through a jump host to an untrusted site = %v; want ErrHostKeyUnknown", err)
	}
}
