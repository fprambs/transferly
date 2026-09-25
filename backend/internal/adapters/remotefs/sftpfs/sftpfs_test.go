package sftpfs_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/fprambs/sentinel/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/sftpfs"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/remotefs"
)

const (
	testUser     = "sentinel"
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

// connect dials with the given credentials on top of the defaults for a test server.
func connect(t *testing.T, s *sftptest.Server, mutate func(*sftpfs.Config)) (*sftpfs.FS, error) {
	t.Helper()
	cfg := sftpfs.Config{Address: s.Addr, Username: testUser, VerifyHostKey: trustAll}
	mutate(&cfg)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
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
		sentinel := remotefs.ErrHostKeyUnknown
		if changed {
			sentinel = remotefs.ErrHostKeyMismatch
		}
		if !errors.Is(err, sentinel) || !errors.As(err, &hostErr) || hostErr.Fingerprint != want || remotefs.Retryable(err) {
			t.Errorf("changed=%v: Dial() = %v; want %v with the fingerprint %s", changed, err, sentinel, want)
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
