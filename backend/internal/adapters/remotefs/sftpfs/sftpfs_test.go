package sftpfs_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"io"
	"io/fs"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/fprambs/transferly/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/prompts"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

const (
	testUser     = "transferly"
	testPassword = "correct horse battery"
)

// trustAll accepts any host key, for tests that are not about host keys.
func trustAll(string, string) error { return nil }

// startServer runs a test server with one password user.
func startServer(t *testing.T, opt sftptest.Options) *sftptest.Server {
	t.Helper()
	if opt.Users == nil {
		opt.Users = map[string]string{testUser: testPassword}
	}
	return sftptest.Start(t, opt)
}

// dial connects to a test server as the password user.
func dial(t *testing.T, s *sftptest.Server) *sftpfs.FS {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fsys, err := sftpfs.Dial(ctx, sftpfs.Config{Address: s.Addr, Username: testUser, Password: testPassword, VerifyHostKey: trustAll})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	t.Cleanup(func() { _ = fsys.Close() })
	return fsys
}

// TestConformance runs the shared RemoteFS suite against the SFTP adapter and an in-process server.
func TestConformance(t *testing.T) {
	remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS { return dial(t, startServer(t, sftptest.Options{})) })
}

// keyPair is a client key in the forms the tests need.
type keyPair struct {
	public    ssh.PublicKey
	plain     []byte
	protected []byte
}

// newKeyPair generates an ed25519 key, plain and protected by a passphrase.
func newKeyPair(t *testing.T, passphrase string) keyPair {
	t.Helper()
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	protected, err := ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	return keyPair{public: public, plain: pem.EncodeToMemory(plain), protected: pem.EncodeToMemory(protected)}
}

// connectTimeout bounds each connection of a test. Decrypting a protected key runs bcrypt inside Dial, which the race detector and
// coverage slow down to many seconds when the whole suite runs at once.
const connectTimeout = time.Minute

// connect dials with the given credentials on top of the defaults for a test server.
func connect(t *testing.T, s *sftptest.Server, mutate func(*sftpfs.Config)) (*sftpfs.FS, error) {
	t.Helper()
	cfg := sftpfs.Config{Address: s.Addr, Username: testUser, VerifyHostKey: trustAll}
	mutate(&cfg)
	ctx, cancel := context.WithTimeout(t.Context(), connectTimeout)
	defer cancel()
	fsys, err := sftpfs.Dial(ctx, cfg)
	if err == nil {
		t.Cleanup(func() { _ = fsys.Close() })
	}
	return fsys, err
}

// TestDial_Authentication verifies how credentials are used and how failures are reported.
//
// Scenarios:
//   - a password works, also on servers that only offer keyboard-interactive
//   - a wrong password and no credentials at all are ErrAuth and not retryable
//   - a wrong password is tried once, on servers that offer both the password method and keyboard-interactive and on those
//     that offer one, so it counts as a single failed attempt towards a lockout
//   - a private key works, with and without a passphrase
//   - a key that needs a passphrase but got none, a wrong passphrase, unreadable key data and a key the server does not
//     know are ErrAuth
//   - a key that the server refuses falls back to the password
func TestDial_Authentication(t *testing.T) {
	protected := newKeyPair(t, "open sesame")
	plain := newKeyPair(t, "unused passphrase")
	stranger := newKeyPair(t, "unused passphrase")
	server := startServer(t, sftptest.Options{Keys: map[string][]ssh.PublicKey{testUser: {protected.public, plain.public}}})
	keyboard := startServer(t, sftptest.Options{KeyboardOnly: true})

	ok := map[string]func(*sftpfs.Config){
		"password":             func(c *sftpfs.Config) { c.Password = testPassword },
		"plain key":            func(c *sftpfs.Config) { c.PrivateKey = plain.plain },
		"protected key":        func(c *sftpfs.Config) { c.PrivateKey, c.Passphrase = protected.protected, []byte("open sesame") },
		"refused key, then pw": func(c *sftpfs.Config) { c.PrivateKey, c.Password = stranger.plain, testPassword },
	}
	for name, mutate := range ok {
		fsys, err := connect(t, server, mutate)
		if err != nil {
			t.Errorf("%s: Dial() = %v", name, err)
			continue
		}
		if err := fsys.Ping(t.Context()); err != nil {
			t.Errorf("%s: Ping() = %v", name, err)
		}
	}
	if _, err := connect(t, keyboard, func(c *sftpfs.Config) { c.Password = testPassword }); err != nil {
		t.Errorf("keyboard-interactive password: Dial() = %v", err)
	}
	if _, err := connect(t, keyboard, func(c *sftpfs.Config) { c.Password = "wrong wrong wrong" }); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("keyboard-interactive wrong password = %v; want ErrAuth", err)
	}

	passwordOnly := startServer(t, sftptest.Options{PasswordOnly: true})
	for name, s := range map[string]*sftptest.Server{"both methods": server, "keyboard-interactive": keyboard, "password method": passwordOnly} {
		before := s.RefusedPasswords()
		if _, err := connect(t, s, func(c *sftpfs.Config) { c.Password = "wrong wrong wrong" }); !errors.Is(err, remotefs.ErrAuth) {
			t.Errorf("%s: wrong password = %v; want ErrAuth", name, err)
		}
		if got := s.RefusedPasswords() - before; got != 1 {
			t.Errorf("%s: wrong password tried %d times; want once", name, got)
		}
	}

	failures := map[string]func(*sftpfs.Config){
		"wrong password":       func(c *sftpfs.Config) { c.Password = "wrong wrong wrong" },
		"no credentials":       func(c *sftpfs.Config) {},
		"missing passphrase":   func(c *sftpfs.Config) { c.PrivateKey = protected.protected },
		"wrong passphrase":     func(c *sftpfs.Config) { c.PrivateKey, c.Passphrase = protected.protected, []byte("not the one") },
		"unreadable key":       func(c *sftpfs.Config) { c.PrivateKey = []byte("not a key") },
		"key of a stranger":    func(c *sftpfs.Config) { c.PrivateKey = stranger.plain },
		"unknown account":      func(c *sftpfs.Config) { c.Username, c.Password = "nobody", testPassword },
		"password for the key": func(c *sftpfs.Config) { c.Username, c.PrivateKey = "nobody", plain.plain },
	}
	for name, mutate := range failures {
		_, err := connect(t, server, mutate)
		if !errors.Is(err, remotefs.ErrAuth) || remotefs.Retryable(err) {
			t.Errorf("%s: Dial() = %v; want a non-retryable ErrAuth", name, err)
		}
	}
}

// TestDial_Prompt covers asking the person connecting whatever the server asks (ADR-0016).
//
// Use cases:
//   - each keyboard-interactive challenge reaches the prompt with its name, instruction, questions and whether each
//     answer may be shown, and the answers sign in; a banner without questions is not asked
//   - a server that only takes the password method gets the password asked with one hidden question
//   - a prompt that fails, declined or unanswered, is a non-retryable ErrAuth
//   - a connection whose context ends while the person is asked fails with the context error
func TestDial_Prompt(t *testing.T) {
	otp := startServer(t, sftptest.Options{KeyboardOnly: true, Code: "123456"})
	var asked []prompts.Challenge
	answer := func(c prompts.Challenge) ([]string, error) {
		asked = append(asked, c)
		return []string{testPassword, "123456"}, nil
	}
	if _, err := connect(t, otp, func(c *sftpfs.Config) { c.Prompt = answer }); err != nil {
		t.Fatalf("Dial() with a code = %v", err)
	}
	if len(asked) != 1 || asked[0].Name != "Two steps" || asked[0].Instruction == "" || len(asked[0].Questions) != 2 ||
		asked[0].Questions[0].Text != "Password: " || asked[0].Questions[0].Echo || !asked[0].Questions[1].Echo {
		t.Errorf("asked = %+v", asked)
	}

	passwordOnly := startServer(t, sftptest.Options{PasswordOnly: true})
	var question prompts.Challenge
	if _, err := connect(t, passwordOnly, func(c *sftpfs.Config) {
		c.Prompt = func(ch prompts.Challenge) ([]string, error) { question = ch; return []string{testPassword}, nil }
	}); err != nil {
		t.Fatalf("Dial() asking the password = %v", err)
	}
	if len(question.Questions) != 1 || question.Questions[0].Echo || question.Questions[0].Text != "Password:" {
		t.Errorf("password question = %+v", question)
	}

	for name, server := range map[string]*sftptest.Server{"keyboard-interactive": otp, "password": passwordOnly} {
		_, err := connect(t, server, func(c *sftpfs.Config) {
			c.Prompt = func(prompts.Challenge) ([]string, error) { return nil, prompts.ErrCanceled }
		})
		if !errors.Is(err, remotefs.ErrAuth) || remotefs.Retryable(err) {
			t.Errorf("%s declined = %v; want a non-retryable ErrAuth", name, err)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	_, err := sftpfs.Dial(ctx, sftpfs.Config{Address: otp.Addr, Username: testUser, VerifyHostKey: trustAll,
		Prompt: func(prompts.Challenge) ([]string, error) { cancel(); return nil, context.Canceled },
	})
	if !errors.Is(err, context.Canceled) || errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Dial() canceled while asking = %v; want context.Canceled", err)
	}
}

// TestDial_HostKeys verifies host key handling.
//
// Scenarios:
//   - the callback receives the key type and the SHA-256 fingerprint of the server key in OpenSSH notation
//   - an unknown key and a changed key are reported with their fingerprint and matching sentinels, without retry
//   - a missing callback is refused before connecting
//   - restricting the accepted key types to the server's works, and to another type makes the handshake fail
func TestDial_HostKeys(t *testing.T) {
	server := startServer(t, sftptest.Options{})
	want := ssh.FingerprintSHA256(server.HostKey)
	password := func(c *sftpfs.Config) { c.Password = testPassword }

	var gotType, gotFingerprint string
	_, err := connect(t, server, func(c *sftpfs.Config) {
		password(c)
		c.VerifyHostKey = func(keyType, fingerprint string) error {
			gotType, gotFingerprint = keyType, fingerprint
			return nil
		}
	})
	if err != nil || gotType != "ssh-ed25519" || gotFingerprint != want {
		t.Errorf("callback got %q %q, Dial() = %v; want ssh-ed25519 %s", gotType, gotFingerprint, err, want)
	}

	for _, changed := range []bool{false, true} {
		_, err := connect(t, server, func(c *sftpfs.Config) {
			password(c)
			c.VerifyHostKey = func(keyType, fingerprint string) error {
				return &remotefs.HostKeyError{Type: keyType, Fingerprint: fingerprint, Changed: changed}
			}
		})
		var hostErr *remotefs.HostKeyError
		transferly := remotefs.ErrHostKeyUnknown
		if changed {
			transferly = remotefs.ErrHostKeyMismatch
		}
		if !errors.Is(err, transferly) || !errors.As(err, &hostErr) || hostErr.Fingerprint != want || remotefs.Retryable(err) {
			t.Errorf("changed=%v: Dial() = %v; want %v with the fingerprint %s", changed, err, transferly, want)
		}
	}

	if _, err := connect(t, server, func(c *sftpfs.Config) { password(c); c.VerifyHostKey = nil }); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Dial() without a host key callback = %v; want ErrInvalid", err)
	}
	if _, err := connect(t, server, func(c *sftpfs.Config) { password(c); c.HostKeyAlgorithms = []string{"ssh-ed25519"} }); err != nil {
		t.Errorf("Dial() restricted to the server's key type = %v", err)
	}
	if _, err := connect(t, server, func(c *sftpfs.Config) { password(c); c.HostKeyAlgorithms = []string{"ecdsa-sha2-nistp256"} }); err == nil {
		t.Error("Dial() succeeded although the server has no key of the accepted type")
	}
}

// TestDial_ConnectionFailures verifies how transport problems are reported.
//
// Scenarios:
//   - a refused connection is a retryable ErrConnectionLost
//   - a custom dial function is used, and its error is reported as a lost connection; a blocked destination
//     keeps its own error and is never retried
//   - a server that closes the connection during the handshake, or that refuses the sftp subsystem, is a lost connection
//   - a context canceled before dialing or while the server stays silent returns the context error promptly
//   - canceling the context from the host key callback stops the connection
func TestDial_ConnectionFailures(t *testing.T) {
	password := func(c *sftpfs.Config) { c.Password = testPassword }

	closed, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := closed.Addr().String()
	_ = closed.Close()
	_, err = sftpfs.Dial(t.Context(), sftpfs.Config{Address: address, Username: testUser, Password: testPassword, VerifyHostKey: trustAll})
	if !errors.Is(err, remotefs.ErrConnectionLost) || !remotefs.Retryable(err) {
		t.Errorf("refused connection = %v; want a retryable ErrConnectionLost", err)
	}

	server := startServer(t, sftptest.Options{})
	dialed := 0
	if _, err := connect(t, server, func(c *sftpfs.Config) {
		password(c)
		c.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			dialed++
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
	}); err != nil || dialed != 1 {
		t.Errorf("custom dialer: Dial() = %v after %d dials", err, dialed)
	}
	failing := errors.New("no route")
	if _, err := connect(t, server, func(c *sftpfs.Config) {
		password(c)
		c.DialContext = func(context.Context, string, string) (net.Conn, error) { return nil, failing }
	}); !errors.Is(err, failing) || !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("failing dialer = %v; want the dialer error as a lost connection", err)
	}
	if _, err := connect(t, server, func(c *sftpfs.Config) {
		password(c)
		c.DialContext = func(context.Context, string, string) (net.Conn, error) { return nil, netguard.ErrBlocked }
	}); !errors.Is(err, netguard.ErrBlocked) || remotefs.Retryable(err) {
		t.Errorf("blocked destination = %v; want a non-retryable netguard.ErrBlocked", err)
	}

	hangup, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hangup.Close() })
	go func() {
		for {
			c, err := hangup.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	_, err = sftpfs.Dial(t.Context(), sftpfs.Config{Address: hangup.Addr().String(), Username: testUser, Password: testPassword, VerifyHostKey: trustAll})
	if !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("server that hangs up = %v; want ErrConnectionLost", err)
	}
	if _, err := connect(t, startServer(t, sftptest.Options{NoSFTP: true}), password); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("server without the sftp subsystem = %v; want ErrConnectionLost", err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := sftpfs.Dial(canceled, sftpfs.Config{Address: server.Addr, Username: testUser, Password: testPassword, VerifyHostKey: trustAll}); !errors.Is(err, context.Canceled) {
		t.Errorf("Dial() with a canceled context = %v; want context.Canceled", err)
	}

	silent, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = silent.Close() })
	go func() {
		for {
			c, err := silent.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	waiting, stop := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer stop()
	started := time.Now()
	if _, err := sftpfs.Dial(waiting, sftpfs.Config{Address: silent.Addr().String(), Username: testUser, Password: testPassword, VerifyHostKey: trustAll}); !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 5*time.Second {
		t.Errorf("Dial() to a silent server = %v after %v; want a prompt deadline error", err, time.Since(started))
	}

	inCallback, cancelInCallback := context.WithCancel(t.Context())
	defer cancelInCallback()
	if _, err := sftpfs.Dial(inCallback, sftpfs.Config{Address: server.Addr, Username: testUser, Password: testPassword, VerifyHostKey: func(string, string) error {
		cancelInCallback()
		return nil
	}}); !errors.Is(err, context.Canceled) {
		t.Errorf("Dial() canceled during the handshake = %v; want context.Canceled", err)
	}
}

// jumpServerConfig returns a minimal SSH server config, with a generated host key, that accepts the given password.
func jumpServerConfig(t *testing.T, username, password string) *ssh.ServerConfig {
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
	return config
}

// jumpHost starts a minimal SSH server that authenticates with username and password and forwards every
// "direct-tcpip" channel it is asked to open to the real address the client names, exactly as a real jump host
// forwards a port. It stands in for a real one in tests, since the fake SFTP server has no use for port forwarding
// of its own.
func jumpHost(t *testing.T, username, password string) string {
	t.Helper()
	config := jumpServerConfig(t, username, password)
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
	return ln.Addr().String()
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
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go ssh.DiscardRequests(requests)
		go relay(channel, net.JoinHostPort(dest.Addr, strconv.Itoa(int(dest.Port))))
	}
}

// relay dials address and copies bytes between it and channel until either side closes.
func relay(channel ssh.Channel, address string) {
	defer func() { _ = channel.Close() }()
	target, err := (&net.Dialer{}).Dial("tcp", address)
	if err != nil {
		return
	}
	defer func() { _ = target.Close() }()
	go func() { _, _ = io.Copy(target, channel); _ = target.Close() }()
	_, _ = io.Copy(channel, target)
}

// TestDialSSH_ThenTunnel verifies the two-step connection a jump host uses: [sftpfs.DialSSH] to reach it, then a
// channel it opens handed to [sftpfs.Dial] as Config.Conn to reach the real site over that channel.
//
// Scenarios:
//   - a channel opened on a bare [sftpfs.DialSSH] client, through a jump host that only forwards "direct-tcpip",
//     reaches the real site exactly as a direct Dial would, with Config.Address never dialed since Config.Conn
//     takes its place
//   - DialSSH reports the same failures as Dial: authentication and host key errors
func TestDialSSH_ThenTunnel(t *testing.T) {
	server := startServer(t, sftptest.Options{})
	jumpAddr := jumpHost(t, "bastion", "let-me-through")

	jump, err := sftpfs.DialSSH(t.Context(), sftpfs.Config{Address: jumpAddr, Username: "bastion", Password: "let-me-through", VerifyHostKey: trustAll})
	if err != nil {
		t.Fatalf("DialSSH() = %v", err)
	}
	t.Cleanup(func() { _ = jump.Close() })

	tunnel, err := jump.DialContext(t.Context(), "tcp", server.Addr)
	if err != nil {
		t.Fatalf("jump.DialContext() = %v", err)
	}
	fsys, err := sftpfs.Dial(t.Context(), sftpfs.Config{
		Conn: tunnel, Address: "unreachable.invalid:1", Username: testUser, Password: testPassword, VerifyHostKey: trustAll,
	})
	if err != nil {
		t.Fatalf("Dial() over a jumped connection = %v", err)
	}
	defer func() { _ = fsys.Close() }()
	if _, err := fsys.List(t.Context(), "."); err != nil {
		t.Errorf("List() over a jumped connection = %v", err)
	}

	if _, err := sftpfs.DialSSH(t.Context(), sftpfs.Config{Address: jumpAddr, Username: "bastion", Password: "wrong", VerifyHostKey: trustAll}); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("DialSSH() with a wrong password = %v; want ErrAuth", err)
	}
	if _, err := sftpfs.DialSSH(t.Context(), sftpfs.Config{Address: jumpAddr, Username: "bastion", Password: "let-me-through", VerifyHostKey: func(string, string) error {
		return &remotefs.HostKeyError{}
	}}); !errors.As(err, new(*remotefs.HostKeyError)) {
		t.Errorf("DialSSH() with a refused host key = %v; want a HostKeyError", err)
	}
}

// TestFS_ConnectionDrop verifies what callers see when the network fails after connecting.
//
// Scenarios:
//   - after the connection is dropped, operations, pings, and reads and writes on open files are retryable
//     ErrConnectionLost
//   - closing a dropped connection is not an error
func TestFS_ConnectionDrop(t *testing.T) {
	server := startServer(t, sftptest.Options{})
	fsys := dial(t, server)
	put(t, fsys, "/f", "content")
	reading, err := fsys.OpenRead(t.Context(), "/f", 0)
	if err != nil {
		t.Fatal(err)
	}
	writing, err := fsys.OpenWrite(t.Context(), "/g", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}

	server.DropConnections()
	time.Sleep(100 * time.Millisecond)

	calls := map[string]error{
		"List":  second(fsys.List(t.Context(), "/")),
		"Ping":  fsys.Ping(t.Context()),
		"Mkdir": fsys.Mkdir(t.Context(), "/d"),
		"Read":  second(reading.Read(make([]byte, 4))),
		"Write": second(writing.Write([]byte("x"))),
		"Close": writing.Close(),
	}
	for name, err := range calls {
		if !errors.Is(err, remotefs.ErrConnectionLost) || !remotefs.Retryable(err) {
			t.Errorf("%s after a drop = %v; want a retryable ErrConnectionLost", name, err)
		}
	}
	if err := reading.Close(); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("closing a reader after a drop = %v", err)
	}
	if err := fsys.Close(); err != nil {
		t.Errorf("Close() after a drop = %v; want nil", err)
	}
}

// put writes content to a remote path.
func put(t *testing.T, fsys remotefs.RemoteFS, p, content string) {
	t.Helper()
	w, err := fsys.OpenWrite(t.Context(), p, 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatalf("OpenWrite(%s) = %v", p, err)
	}
	if _, err := io.WriteString(w, content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// second returns the error of a call that also returns a value.
func second[T any](_ T, err error) error { return err }

// TestFS_ServerErrors verifies how status codes injected by the server are reported.
//
// Scenarios:
//   - permission denied statuses are ErrPermission for listing, reading, writing, creating, removing, renaming,
//     and changing modes and times
//   - a generic failure while listing a directory is not reported as "not a directory"
//   - the status "connection lost" is a retryable ErrConnectionLost, and "operation unsupported" stays a plain error
//   - an unexpected failure while listing, removing or looking up during RemoveAll is reported, not hidden
func TestFS_ServerErrors(t *testing.T) {
	var mu sync.Mutex
	failing := map[string]error{}
	set := func(method string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err == nil {
			delete(failing, method)
			return
		}
		failing[method] = err
	}
	server := startServer(t, sftptest.Options{Fault: func(method, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		return failing[method]
	}})
	fsys := dial(t, server)
	put(t, fsys, "/f", "x")
	if err := fsys.Mkdir(t.Context(), "/d"); err != nil {
		t.Fatal(err)
	}

	denied := map[string]func() error{
		"List": func() error { return second(fsys.List(t.Context(), "/d")) },
		"Stat": func() error { return second(fsys.Stat(t.Context(), "/f")) },
		"Get":  func() error { r, err := fsys.OpenRead(t.Context(), "/f", 0); closeIf(r); return err },
		"Put": func() error {
			w, err := fsys.OpenWrite(t.Context(), "/h", 0, remotefs.WriteOverwrite)
			closeIf(w)
			return err
		},
		"Put2": func() error {
			w, err := fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteResume)
			closeIf(w)
			return err
		},
		"Mkdir":    func() error { return fsys.Mkdir(t.Context(), "/new") },
		"Remove":   func() error { return fsys.Remove(t.Context(), "/f") },
		"Rename":   func() error { return fsys.Rename(t.Context(), "/f", "/renamed") },
		"Setstat":  func() error { return fsys.Chmod(t.Context(), "/f", 0o600) },
		"Setstat2": func() error { return fsys.Chtimes(t.Context(), "/f", time.Now()) },
	}
	for name, call := range denied {
		method := strings.TrimSuffix(name, "2")
		set(method, permissionDenied)
		if err := call(); !errors.Is(err, remotefs.ErrPermission) {
			t.Errorf("%s with a permission error = %v; want ErrPermission", name, err)
		}
		set(method, nil)
	}

	set("List", errors.New("disk on fire"))
	if err := second(fsys.List(t.Context(), "/d")); err == nil || errors.Is(err, remotefs.ErrNotDirectory) {
		t.Errorf("a generic failure while listing a directory = %v; want a plain error, not ErrNotDirectory", err)
	}
	set("List", nil)

	set("Mkdir", sftp.ErrSSHFxConnectionLost)
	if err := fsys.Mkdir(t.Context(), "/x"); !errors.Is(err, remotefs.ErrConnectionLost) || !remotefs.Retryable(err) {
		t.Errorf("connection lost status = %v; want a retryable ErrConnectionLost", err)
	}
	set("Mkdir", nil)
	set("Setstat", sftp.ErrSSHFxOpUnsupported)
	if err := fsys.Chmod(t.Context(), "/f", 0o600); err == nil || errors.Is(err, remotefs.ErrConnectionLost) || errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("unsupported status = %v; want a plain error", err)
	}
	set("Setstat", nil)

	if err := fsys.Mkdir(t.Context(), "/tree"); err != nil {
		t.Fatal(err)
	}
	put(t, fsys, "/tree/f", "x")
	set("Lstat", permissionDenied)
	if err := fsys.RemoveAll(t.Context(), "/tree"); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("RemoveAll with Lstat denied = %v; want ErrPermission", err)
	}
	set("Lstat", nil)
	set("List", errors.New("disk on fire"))
	if err := fsys.RemoveAll(t.Context(), "/tree"); err == nil {
		t.Error("RemoveAll with listing failing succeeded")
	}
	set("List", nil)
	set("Remove", permissionDenied)
	if err := fsys.RemoveAll(t.Context(), "/tree"); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("RemoveAll with Remove denied = %v; want ErrPermission", err)
	}
	set("Remove", nil)
}

// permissionDenied is the error a real server raises for EACCES, which the SFTP server maps to its own status.
var permissionDenied = &fs.PathError{Op: "open", Path: "/", Err: syscall.EACCES}

// closeIf closes a stream that was opened.
func closeIf(c io.Closer) {
	if c != nil && !reflect.ValueOf(c).IsNil() {
		_ = c.Close()
	}
}

// TestFS_RemoveAll verifies the safety of recursive removal.
//
// Scenarios:
//   - a symbolic link inside the tree is removed and its target is left alone
//   - canceling the context between entries stops the removal and returns the context error
//   - relative and cleaned root paths are refused as the root
func TestFS_RemoveAll(t *testing.T) {
	var cancelNow atomic.Pointer[context.CancelFunc]
	server := startServer(t, sftptest.Options{Fault: func(method, _ string) error {
		if cancel := cancelNow.Load(); cancel != nil && method == "Remove" {
			(*cancel)()
		}
		return nil
	}})
	fsys := dial(t, server)

	if err := os.MkdirAll(filepath.Join(server.Root, "victim"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(server.Root, "victim", "precious"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(server.Root, "tree"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(server.Root, "victim"), filepath.Join(server.Root, "tree", "link")); err != nil {
		t.Fatal(err)
	}
	if err := fsys.RemoveAll(t.Context(), "/tree"); err != nil {
		t.Fatalf("RemoveAll() = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(server.Root, "victim", "precious")); err != nil || string(data) != "keep" {
		t.Errorf("the target of a symbolic link was removed: %q, %v", data, err)
	}

	if err := os.MkdirAll(filepath.Join(server.Root, "tree"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		put(t, fsys, "/tree/"+name, "x")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancelNow.Store(&cancel)
	if err := fsys.RemoveAll(ctx, "/tree"); !errors.Is(err, context.Canceled) {
		t.Errorf("RemoveAll() canceled midway = %v; want context.Canceled", err)
	}
	cancelNow.Store(nil)
	if _, err := os.Stat(filepath.Join(server.Root, "tree")); err != nil {
		t.Errorf("a canceled RemoveAll removed the directory itself: %v", err)
	}

	for _, root := range []string{"/", "//", "/a/.."} {
		if err := fsys.RemoveAll(t.Context(), root); !errors.Is(err, remotefs.ErrPermission) {
			t.Errorf("RemoveAll(%q) = %v; want ErrPermission", root, err)
		}
	}
}

// failingReader fails after producing some data.
type failingReader struct{ err error }

// Read returns data once and then the error.
func (f *failingReader) Read(p []byte) (int, error) {
	if f.err != nil {
		err := f.err
		f.err = nil
		return copy(p, "partial"), err
	}
	return 0, io.EOF
}

// failingWriter refuses every write.
type failingWriter struct{ err error }

// Write always fails.
func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

// TestFS_TransferErrors verifies that errors of the local side of a transfer are not taken for network failures.
//
// Scenarios:
//   - io.Copy into a remote file returns the error of a failing source unchanged and not as a lost connection
//   - io.Copy from a remote file returns the error of a failing destination unchanged
//   - io.Copy between healthy sides transfers everything, through the concurrent write and read paths of the adapter
func TestFS_TransferErrors(t *testing.T) {
	fsys := dial(t, startServer(t, sftptest.Options{}))
	sourceErr := errors.New("local disk failed")

	w, err := fsys.OpenWrite(t.Context(), "/up", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(w, &failingReader{err: sourceErr})
	_ = w.Close()
	if !errors.Is(err, sourceErr) || errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("copy from a failing source = %v; want the source error only", err)
	}

	put(t, fsys, "/down", strings.Repeat("d", 100_000))
	r, err := fsys.OpenRead(t.Context(), "/down", 0)
	if err != nil {
		t.Fatal(err)
	}
	sinkErr := errors.New("local disk full")
	_, err = io.Copy(failingWriter{err: sinkErr}, r)
	_ = r.Close()
	if !errors.Is(err, sinkErr) || errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("copy to a failing destination = %v; want the destination error only", err)
	}

	payload := bytes.Repeat([]byte("0123456789abcdef"), 200_000)
	w, err = fsys.OpenWrite(t.Context(), "/big", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := io.Copy(w, struct{ io.Reader }{bytes.NewReader(payload)}); err != nil || n != int64(len(payload)) {
		t.Fatalf("upload = %d, %v", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = fsys.OpenRead(t.Context(), "/big", 0)
	if err != nil {
		t.Fatal(err)
	}
	var back bytes.Buffer
	if n, err := io.Copy(&back, r); err != nil || n != int64(len(payload)) || !bytes.Equal(back.Bytes(), payload) {
		t.Errorf("download = %d bytes, %v; want the uploaded content", n, err)
	}
	_ = r.Close()
}

// TestFS_Entries verifies the shape of the entries.
//
// Scenarios:
//   - entries have second precision, absolute paths and the base name
//   - a symbolic link lists as a link and stats as its target
func TestFS_Entries(t *testing.T) {
	server := startServer(t, sftptest.Options{})
	fsys := dial(t, server)
	put(t, fsys, "/dir-file", "abc")
	if err := os.Symlink(filepath.Join(server.Root, "dir-file"), filepath.Join(server.Root, "link")); err != nil {
		t.Fatal(err)
	}

	entries, err := fsys.List(t.Context(), "/")
	if err != nil || len(entries) != 2 {
		t.Fatalf("List() = %v, %v", entries, err)
	}
	for _, e := range entries {
		if e.ModTimePrecision != remotefs.PrecisionSecond || e.Path != "/"+e.Name {
			t.Errorf("entry = %+v; want second precision and an absolute path", e)
		}
		if e.Name == "link" && e.Mode&fs.ModeSymlink == 0 {
			t.Errorf("link entry mode = %v; want a symbolic link", e.Mode)
		}
	}
	if e, err := fsys.Stat(t.Context(), "/link"); err != nil || e.Size != 3 || e.Mode&fs.ModeSymlink != 0 {
		t.Errorf("Stat(/link) = %+v, %v; want the target's 3 bytes", e, err)
	}
}

// TestFS_WriteToDirectory verifies exclusive creation over an existing directory.
//
// Scenarios:
//   - creating a file where a directory exists is ErrExists, and overwriting it is ErrInvalid
func TestFS_WriteToDirectory(t *testing.T) {
	fsys := dial(t, startServer(t, sftptest.Options{}))
	if err := fsys.Mkdir(t.Context(), "/d"); err != nil {
		t.Fatal(err)
	}
	if err := second(fsys.OpenWrite(t.Context(), "/d", 0, remotefs.WriteCreate)); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("create over a directory = %v; want ErrExists", err)
	}
	if err := second(fsys.OpenWrite(t.Context(), "/d", 0, remotefs.WriteOverwrite)); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("overwrite of a directory = %v; want ErrInvalid", err)
	}
}

// TestDial_RefusesLegacyCryptography verifies that a server that only speaks broken algorithms is not talked to.
//
// Scenarios:
//   - a server offering only CBC ciphers cannot be connected to, though its password is right
//   - a server offering only the SHA-1 Diffie-Hellman group 1 key exchange cannot be connected to
//   - a server offering only ciphers that the client also offers, as OpenSSH does, still works, so the refusal is about
//     the algorithms and not about the test server
func TestDial_RefusesLegacyCryptography(t *testing.T) {
	password := func(c *sftpfs.Config) { c.Password = testPassword }

	for name, opt := range map[string]sftptest.Options{
		"cbc cipher":        {Ciphers: []string{"aes128-cbc"}},
		"3des cipher":       {Ciphers: []string{"3des-cbc"}},
		"sha1 group 1 kex":  {KeyExchanges: []string{"diffie-hellman-group1-sha1"}},
		"sha1 group 14 kex": {KeyExchanges: []string{"diffie-hellman-group14-sha1"}},
		"rc4 stream cipher": {Ciphers: []string{"arcfour256"}},
	} {
		server := startServer(t, opt)
		if fsys, err := connect(t, server, password); err == nil {
			_ = fsys.Close()
			t.Errorf("%s: connected to a server that only offers a legacy algorithm", name)
		}
	}

	modern := startServer(t, sftptest.Options{Ciphers: []string{"aes128-gcm@openssh.com"}, KeyExchanges: []string{"curve25519-sha256"}})
	if _, err := connect(t, modern, password); err != nil {
		t.Errorf("modern algorithms: Dial() = %v", err)
	}
	for _, cipher := range []string{"chacha20-poly1305@openssh.com", "aes128-ctr", "aes192-ctr", "aes256-ctr", "aes256-gcm@openssh.com"} {
		if _, err := connect(t, startServer(t, sftptest.Options{Ciphers: []string{cipher}}), password); err != nil {
			t.Errorf("%s: Dial() = %v", cipher, err)
		}
	}
}

// TestDial_GroupExchangeSHA256 covers enterprise gateways whose only SHA-2 key exchange is group exchange.
func TestDial_GroupExchangeSHA256(t *testing.T) {
	server := startServer(t, sftptest.Options{
		KeyExchanges: []string{"diffie-hellman-group-exchange-sha256"},
		Ciphers:      []string{"aes256-ctr"},
	})
	fsys := dial(t, server)
	if _, err := fsys.List(t.Context(), "/"); err != nil {
		t.Fatalf("List after group exchange = %v", err)
	}
}

// dropAfter accepts every write and drops the server's connections once it received limit bytes.
type dropAfter struct {
	server *sftptest.Server
	limit  int64
	got    int64
}

// Write implements [io.Writer].
func (d *dropAfter) Write(p []byte) (int, error) {
	d.got += int64(len(p))
	if d.got >= d.limit && d.limit > 0 {
		d.server.DropConnections()
		d.limit = 0
	}
	return len(p), nil
}

// TestFS_StrictServers verifies how a connection learns that its server is strict and what changes then.
//
// Use cases:
//   - a server is trusted until a whole file comes with another size than it reported, and then on this connection and the next
//   - a strict server is read one request at a time and still sends the whole file
//   - a pipelined read that loses the connection at once marks the server strict, and one that fails later does not
//   - nobody remembers anything without a memory of strict servers
//   - a site marked with inexact sizes treats its server as strict from the start
func TestFS_StrictServers(t *testing.T) {
	server := startServer(t, sftptest.Options{})
	known := &sftpfs.StrictServers{}
	config := func(c *sftpfs.Config) { c.Password, c.Strict = testPassword, known }
	fsys, err := connect(t, server, config)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("0123456789abcdef"), 1<<16)
	put(t, fsys, "/big", string(payload))
	fsys.NoteWholeRead(10, 10)
	if !fsys.SizesTrusted() || known.Has(server.Addr) {
		t.Fatal("a whole file of the reported size made the server strict")
	}
	fsys.NoteWholeRead(14, 10)
	if fsys.SizesTrusted() || !known.Has(server.Addr) {
		t.Fatal("a whole file of another size did not make the server strict")
	}
	r, err := fsys.OpenRead(t.Context(), "/big", 0)
	if err != nil {
		t.Fatal(err)
	}
	var back bytes.Buffer
	if n, err := io.Copy(&back, r); err != nil || n != int64(len(payload)) || !bytes.Equal(back.Bytes(), payload) {
		t.Errorf("strict download = %d bytes, %v; want the whole file", n, err)
	}
	_ = r.Close()
	again, err := connect(t, server, config)
	if err != nil {
		t.Fatal(err)
	}
	if again.SizesTrusted() {
		t.Error("a new connection to a strict server trusted its sizes")
	}

	early := startServer(t, sftptest.Options{})
	plain := dial(t, early)
	put(t, plain, "/big", string(payload))
	r, err = plain.OpenRead(t.Context(), "/big", 0)
	if err != nil {
		t.Fatal(err)
	}
	early.DropConnections()
	time.Sleep(100 * time.Millisecond)
	if _, err := io.Copy(io.Discard, r); !errors.Is(err, remotefs.ErrConnectionLost) || plain.SizesTrusted() {
		t.Errorf("read dropped at once = %v, trusted %v; want ErrConnectionLost and a strict server", err, plain.SizesTrusted())
	}

	late := startServer(t, sftptest.Options{})
	plain = dial(t, late)
	put(t, plain, "/big", strings.Repeat("x", 8<<20))
	r, err = plain.OpenRead(t.Context(), "/big", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(&dropAfter{server: late, limit: 2 << 20}, r); !errors.Is(err, remotefs.ErrConnectionLost) || !plain.SizesTrusted() {
		t.Errorf("read dropped late = %v, trusted %v; want ErrConnectionLost and a trusted server", err, plain.SizesTrusted())
	}
	if (*sftpfs.StrictServers)(nil).Has(server.Addr) {
		t.Error("a missing memory remembered a server")
	}

	marked, err := connect(t, late, func(c *sftpfs.Config) { c.Password, c.InexactSizes = testPassword, true })
	if err != nil {
		t.Fatal(err)
	}
	if marked.SizesTrusted() {
		t.Error("a site marked with inexact sizes trusted its server")
	}
}

// TestFS_StrictBulkReads verifies that strict servers, read one request at a time, are read through a second channel of the
// connection whose requests are as large as a reply can carry (how much that saves is measured against OpenSSH by
// TestStrictBulkReads_OpenSSH, since this test server answers 32 KiB at most).
//
// Use cases:
//   - a file on a strict server is read whole through a second SFTP session of the connection, opened once
//   - a server that refuses a second SFTP session is read through the session, and the file still comes whole
//   - a connection closed before reading anything closes cleanly
func TestFS_StrictBulkReads(t *testing.T) {
	payload := bytes.Repeat([]byte("strict gateway "), 1_100_000/15)
	for _, tc := range []struct {
		name     string
		one      bool
		sessions int64
	}{{"bulk", false, 2}, {"one session", true, 1}} {
		server := startServer(t, sftptest.Options{OneSFTPSession: tc.one})
		fsys, err := connect(t, server, func(c *sftpfs.Config) { c.Password, c.InexactSizes = testPassword, true })
		if err != nil {
			t.Fatal(err)
		}
		put(t, fsys, "/big", string(payload))
		for range 2 {
			r, err := fsys.OpenRead(t.Context(), "/big", 0)
			if err != nil {
				t.Fatal(err)
			}
			var back bytes.Buffer
			n, err := io.Copy(&back, r)
			_ = r.Close()
			if err != nil || n != int64(len(payload)) || !bytes.Equal(back.Bytes(), payload) {
				t.Errorf("%s: %d bytes, %v; want the whole file", tc.name, n, err)
			}
		}
		if got := server.Sessions(); got != tc.sessions {
			t.Errorf("%s: %d SFTP sessions; want %d", tc.name, got, tc.sessions)
		}
		_ = fsys.Close()
	}

	server := startServer(t, sftptest.Options{})
	idle, err := connect(t, server, func(c *sftpfs.Config) { c.Password, c.InexactSizes = testPassword, true })
	if err != nil {
		t.Fatal(err)
	}
	if err := idle.Close(); err != nil {
		t.Errorf("Close() before any read = %v", err)
	}
}

// TestFS_LyingServer verifies that a malicious server that claims more data in its replies to reads than it carries makes reads
// fail instead of bringing the process down, whichever way the file is read.
//
// Use cases:
//   - a pipelined download, a strict one read one request at a time, a small read at an offset and a large one read in parallel all
//     fail with an error
func TestFS_LyingServer(t *testing.T) {
	server := startServer(t, sftptest.Options{LyingReads: true})
	for _, strict := range []bool{false, true} {
		fsys, err := connect(t, server, func(c *sftpfs.Config) { c.Password, c.InexactSizes = testPassword, strict })
		if err != nil {
			t.Fatal(err)
		}
		put(t, fsys, "/big", strings.Repeat("x", 1<<20))
		r, err := fsys.OpenRead(t.Context(), "/big", 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, r); err == nil {
			t.Errorf("strict %v: a download from a lying server succeeded", strict)
		}
		_ = r.Close()
		r, err = fsys.OpenRead(t.Context(), "/big", 10)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Read(make([]byte, 100)); err == nil {
			t.Errorf("strict %v: a read at an offset from a lying server succeeded", strict)
		}
		if _, err := r.Read(make([]byte, 200_000)); err == nil {
			t.Errorf("strict %v: a large read from a lying server succeeded", strict)
		}
		_ = r.Close()
		_ = fsys.Close()
	}
}

// TestDial_ServerDropsOnBadPassword verifies servers that close the connection on a wrong password instead of answering with a
// failure, as some gateways do.
//
// Use cases:
//   - a wrong password is refused credentials, which are not tried again, and not a lost connection
//   - the right password signs in
//   - a server that closes the connection before its key was checked is still a lost connection
func TestDial_ServerDropsOnBadPassword(t *testing.T) {
	server := startServer(t, sftptest.Options{DropOnBadPassword: true})
	_, err := connect(t, server, func(c *sftpfs.Config) { c.Password = "wrong password" })
	if !errors.Is(err, remotefs.ErrAuth) || remotefs.Retryable(err) {
		t.Errorf("wrong password = %v; want refused credentials that are not retried", err)
	}
	fsys, err := connect(t, server, func(c *sftpfs.Config) { c.Password = testPassword })
	if err != nil {
		t.Fatalf("right password = %v", err)
	}
	_ = fsys.Close()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		if conn, err := listener.Accept(); err == nil {
			_ = conn.Close()
		}
	}()
	_, err = sftpfs.Dial(t.Context(), sftpfs.Config{Address: listener.Addr().String(), Username: "u", Password: "p", VerifyHostKey: func(string, string) error { return nil }})
	if !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("closed before the key = %v; want ErrConnectionLost", err)
	}
}

// TestDial_NegotiationDiagnostics checks that incompatibility is actionable and never retried.
func TestDial_NegotiationDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		category, algorithm string
		options             sftptest.Options
	}{
		{"key_exchange", "diffie-hellman-group14-sha1", sftptest.Options{KeyExchanges: []string{"diffie-hellman-group14-sha1"}}},
		{"cipher", "aes128-cbc", sftptest.Options{Ciphers: []string{"aes128-cbc"}}},
		{"mac", "hmac-sha1-96", sftptest.Options{Ciphers: []string{"aes128-ctr"}, MACs: []string{"hmac-sha1-96"}}},
	} {
		t.Run(tc.category, func(t *testing.T) {
			server := startServer(t, tc.options)
			_, err := connect(t, server, func(c *sftpfs.Config) { c.Password = testPassword })
			var negotiation *remotefs.NegotiationError
			if !errors.As(err, &negotiation) {
				t.Fatalf("expected negotiation error, got %v", err)
			}
			if negotiation.Category != tc.category || !slices.Contains(negotiation.ServerAlgorithms, tc.algorithm) {
				t.Fatalf("diagnostic = %+v", negotiation)
			}
			if remotefs.Retryable(err) || remotefs.Retryable(errors.Join(err, remotefs.ErrConnectionLost)) {
				t.Fatal("incompatibility must not retry")
			}
			if server.Sessions() != 0 {
				t.Fatal("incompatible connection started SFTP")
			}
		})
	}
	for _, mac := range ssh.SupportedAlgorithms().MACs {
		t.Run(mac, func(t *testing.T) {
			_, err := connect(t, startServer(t, sftptest.Options{Ciphers: []string{"aes128-ctr"}, MACs: []string{mac}}), func(c *sftpfs.Config) { c.Password = testPassword })
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestDial_HostSignatures accepts RSA SHA-2 server signatures and refuses SHA-1.
func TestDial_HostSignatures(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, algorithm := range []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSA} {
		t.Run(algorithm, func(t *testing.T) {
			restricted, err := ssh.NewSignerWithAlgorithms(signer.(ssh.AlgorithmSigner), []string{algorithm})
			if err != nil {
				t.Fatal(err)
			}
			server := startServer(t, sftptest.Options{HostKey: restricted})
			_, err = connect(t, server, func(c *sftpfs.Config) { c.Password = testPassword })
			if algorithm != ssh.KeyAlgoRSA {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var negotiation *remotefs.NegotiationError
			if !errors.As(err, &negotiation) || negotiation.Category != "host_signature" || !slices.Contains(negotiation.ServerAlgorithms, algorithm) {
				t.Fatalf("unsafe signature diagnostic = %v", err)
			}
		})
	}
}

// TestDial_AuthenticationSignatures accepts RSA SHA-2 authentication and refuses SHA-1.
func TestDial_AuthenticationSignatures(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, algorithm := range []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSA} {
		t.Run(algorithm, func(t *testing.T) {
			server := startServer(t, sftptest.Options{PublicKeyAuths: []string{algorithm}, Keys: map[string][]ssh.PublicKey{testUser: {signer.PublicKey()}}})
			_, err := connect(t, server, func(c *sftpfs.Config) { c.PrivateKey = pem.EncodeToMemory(block) })
			if algorithm != ssh.KeyAlgoRSA {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var negotiation *remotefs.NegotiationError
			if !errors.As(err, &negotiation) || negotiation.Category != "authentication_signature" || remotefs.Retryable(err) {
				t.Fatalf("unsafe authentication signature diagnostic = %v", err)
			}
		})
	}
}

// TestDial_TCPTimeoutStops checks that a TCP timeout ends without host verification or retry.
func TestDial_TCPTimeoutStops(t *testing.T) {
	calls := 0
	_, err := sftpfs.DialSSH(t.Context(), sftpfs.Config{
		Address: "blocked.example:22", Username: testUser, Password: testPassword,
		VerifyHostKey: func(string, string) error { t.Fatal("timeout must precede host verification"); return nil },
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			calls++
			return nil, &net.DNSError{Err: "i/o timeout", IsTimeout: true}
		},
	})
	if !errors.Is(err, remotefs.ErrConnectTimeout) || remotefs.Retryable(err) || calls != 1 {
		t.Fatalf("timeout = %v, calls = %d", err, calls)
	}
}

// TestDial_RejectsDSAAuthentication refuses an obsolete signing key before opening a network connection.
// Its synthetic parser fixture has the required field sizes and is never used to sign.
func TestDial_RejectsDSAAuthentication(t *testing.T) {
	p := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 1023), big.NewInt(1))
	q := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 159), big.NewInt(1))
	der, err := asn1.Marshal([]any{0, p, q, big.NewInt(2), big.NewInt(8), big.NewInt(3)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sftpfs.DialSSH(t.Context(), sftpfs.Config{
		Address: "unused.example:22", Username: testUser,
		PrivateKey:    pem.EncodeToMemory(&pem.Block{Type: "DSA PRIVATE KEY", Bytes: der}),
		VerifyHostKey: func(string, string) error { return nil },
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("obsolete key reached network")
			return nil, nil
		},
	})
	if !errors.Is(err, remotefs.ErrAuth) || !strings.Contains(err.Error(), "obsolete signature") {
		t.Fatalf("DSA = %v", err)
	}
}
