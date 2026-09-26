// Package ftptest runs an in-process FTP or FTPS server for adapter tests, built on ftpserverlib with an in-memory file system.
package ftptest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
)

// Encryption tells how the server protects the control connection.
type Encryption int

const (
	// Plain accepts clear connections only.
	Plain Encryption = iota
	// Explicit accepts clear connections and upgrades them with AUTH TLS; data connections follow PROT P.
	Explicit
	// Implicit expects TLS from the first byte, as on port 990.
	Implicit
)

// Options configure a test server.
type Options struct {
	// Users maps user names to passwords; an empty map accepts "sentinel" with "sentinel".
	Users map[string]string
	// Encryption selects plain FTP, explicit FTPS or implicit FTPS.
	Encryption Encryption
	// DisableMLSD makes the server answer listings only through LIST, as old servers do.
	DisableMLSD bool
	// DisableMFMT makes the server refuse to change modification times.
	DisableMFMT bool
	// Fault returns an error to answer an operation with instead of performing it; nil performs it. The operation is one
	// of "stat", "read", "write", "mkdir", "remove" or "rename", and name is the path it concerns.
	Fault func(op, name string) error
	// PublicHost is the IP the server announces for passive connections; empty announces its own address.
	PublicHost string
}

// Server is a running test server.
type Server struct {
	// Addr is the host:port the server listens on.
	Addr string
	// Fingerprint is the SHA-256 fingerprint of the certificate the server presents, in OpenSSH notation.
	Fingerprint string
	// FS is the in-memory file system served as "/".
	FS afero.Fs

	server   *ftpserver.FtpServer
	listener *cutting
}

// cutting is a listener that remembers the connections it accepted, so a test can cut them all at once.
type cutting struct {
	net.Listener
	mu    sync.Mutex
	conns []net.Conn
}

// Accept accepts a connection and remembers it.
func (l *cutting) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.mu.Lock()
		l.conns = append(l.conns, conn)
		l.mu.Unlock()
	}
	return conn, err
}

// cut closes every accepted connection.
func (l *cutting) cut() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, conn := range l.conns {
		_ = conn.Close()
	}
}

// driver is the ftpserverlib driver of one test server.
type driver struct {
	opt      Options
	fs       afero.Fs
	cert     tls.Certificate
	listener net.Listener
}

// Fingerprint returns the SHA-256 fingerprint of a DER certificate in OpenSSH notation, as Sentinel records it.
func Fingerprint(der []byte) string {
	digest := sha256.Sum256(der)
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(digest[:]), "=")
}

// certificate makes a self-signed certificate for 127.0.0.1 and localhost.
func certificate(t testing.TB) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sentinel-ftptest"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// Start runs a server on a free loopback port until the test ends.
func Start(t testing.TB, opt Options) *Server {
	t.Helper()
	if opt.Users == nil {
		opt.Users = map[string]string{"sentinel": "sentinel"}
	}
	memory := afero.NewMemMapFs()
	if err := memory.MkdirAll("/", 0o755); err != nil {
		t.Fatal(err)
	}
	socket, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &cutting{Listener: socket}
	cert := certificate(t)
	var served net.Listener = listener
	if opt.Encryption == Implicit {
		served = tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	}
	d := &driver{opt: opt, listener: served, fs: strict{Fs: memory, fault: opt.Fault}, cert: cert}
	server := ftpserver.NewFtpServer(d)
	if err := server.Listen(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Stop() })
	return &Server{Addr: server.Addr(), Fingerprint: Fingerprint(d.cert.Certificate[0]), FS: memory, server: server, listener: listener}
}

// Stop shuts the server down, cutting every connection, as when a server goes away in the middle of a session.
func (s *Server) Stop() {
	_ = s.server.Stop()
	s.listener.cut()
}

// GetSettings implements [ftpserver.MainDriver].
func (d *driver) GetSettings() (*ftpserver.Settings, error) {
	settings := &ftpserver.Settings{
		Listener:    d.listener,
		ListenAddr:  "127.0.0.1:0",
		PublicHost:  d.opt.PublicHost,
		DisableMLSD: d.opt.DisableMLSD,
		DisableMLST: d.opt.DisableMLSD,
		DisableMFMT: d.opt.DisableMFMT,
	}
	switch d.opt.Encryption {
	case Explicit:
		settings.TLSRequired = ftpserver.MandatoryEncryption
	case Implicit:
		settings.TLSRequired = ftpserver.ImplicitEncryption
	}
	return settings, nil
}

// ClientConnected implements [ftpserver.MainDriver].
func (d *driver) ClientConnected(ftpserver.ClientContext) (string, error) {
	return "sentinel test server", nil
}

// ClientDisconnected implements [ftpserver.MainDriver].
func (d *driver) ClientDisconnected(ftpserver.ClientContext) {}

// AuthUser implements [ftpserver.MainDriver].
func (d *driver) AuthUser(_ ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	if want, ok := d.opt.Users[user]; ok && want == pass {
		return d.fs, nil
	}
	return nil, errors.New("wrong user or password")
}

// GetTLSConfig implements [ftpserver.MainDriver].
func (d *driver) GetTLSConfig() (*tls.Config, error) {
	if d.opt.Encryption == Plain {
		return nil, errors.New("this server does not do TLS")
	}
	return &tls.Config{Certificates: []tls.Certificate{d.cert}, MinVersion: tls.VersionTLS12}, nil
}

// strict makes the in-memory file system as demanding as a disk: the in-memory one creates missing parents and removes
// directories that still have entries, which no real server does.
type strict struct {
	afero.Fs
	fault func(op, name string) error
}

// failing returns the injected error for an operation, if any.
func (s strict) failing(op, name string) error {
	if s.fault == nil {
		return nil
	}
	return s.fault(op, name)
}

// parent fails when the directory that would hold name is missing or is not a directory.
func (s strict) parent(name string) error {
	info, err := s.Fs.Stat(path.Dir(name))
	switch {
	case err != nil:
		return &os.PathError{Op: "open", Path: path.Dir(name), Err: os.ErrNotExist}
	case !info.IsDir():
		return &os.PathError{Op: "open", Path: path.Dir(name), Err: syscall.ENOTDIR}
	}
	return nil
}

// Mkdir creates a directory whose parent exists and that does not exist yet.
func (s strict) Mkdir(name string, perm os.FileMode) error {
	if err := s.failing("mkdir", name); err != nil {
		return err
	}
	if err := s.parent(name); err != nil {
		return err
	}
	if _, err := s.Fs.Stat(name); err == nil {
		return &os.PathError{Op: "mkdir", Path: name, Err: os.ErrExist}
	}
	return s.Fs.Mkdir(name, perm)
}

// Stat describes a path unless a fault is injected for it.
func (s strict) Stat(name string) (os.FileInfo, error) {
	if err := s.failing("stat", name); err != nil {
		return nil, err
	}
	return s.Fs.Stat(name)
}

// Create creates a file whose parent exists.
func (s strict) Create(name string) (afero.File, error) {
	if err := s.parent(name); err != nil {
		return nil, err
	}
	return s.Fs.Create(name)
}

// OpenFile opens a file, creating it only when its parent exists.
func (s strict) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	op := "read"
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_APPEND) != 0 {
		op = "write"
	}
	if err := s.failing(op, name); err != nil {
		return nil, err
	}
	if flag&os.O_CREATE != 0 {
		if err := s.parent(name); err != nil {
			return nil, err
		}
	}
	return s.Fs.OpenFile(name, flag, perm)
}

// Rename moves a path to a place whose parent exists.
func (s strict) Rename(oldname, newname string) error {
	if err := s.failing("rename", oldname); err != nil {
		return err
	}
	if err := s.parent(newname); err != nil {
		return err
	}
	return s.Fs.Rename(oldname, newname)
}

// Remove deletes a file or a directory without entries.
func (s strict) Remove(name string) error {
	if err := s.failing("remove", name); err != nil {
		return err
	}
	if info, err := s.Fs.Stat(name); err == nil && info.IsDir() {
		if children, err := afero.ReadDir(s.Fs, name); err == nil && len(children) > 0 {
			return &os.PathError{Op: "remove", Path: name, Err: syscall.ENOTEMPTY}
		}
	}
	return s.Fs.Remove(name)
}
