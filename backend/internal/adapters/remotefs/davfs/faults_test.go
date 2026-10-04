package davfs_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/davfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/davfs/davtest"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// faulty is a test server whose requests fail once a rule is armed.
type faulty struct {
	*davtest.Server
	mu     sync.Mutex
	method string
	name   string
	status int
	skip   int
}

// newFaulty starts a test server that fails what its armed rule matches.
func newFaulty(t *testing.T) *faulty {
	f := &faulty{}
	f.Server = davtest.Start(t, davtest.Options{Fault: f.check})
	return f
}

// arm makes the requests of method on the path name below the root, with or without a trailing slash, answer status, after
// letting the first skip of them through.
func (f *faulty) arm(method, name string, status, skip int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.method, f.name, f.status, f.skip = method, name, status, skip
}

// check answers a request with the armed status when it matches.
func (f *faulty) check(r *http.Request) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.method == "" || r.Method != f.method || strings.TrimSuffix(r.URL.Path, "/") != strings.TrimSuffix(davtest.Root+strings.TrimPrefix(f.name, "/"), "/") {
		return 0
	}
	if f.skip > 0 {
		f.skip--
		return 0
	}
	return f.status
}

// write uploads content to name in the given mode and returns the failure of the upload.
func write(t *testing.T, fsys remotefs.RemoteFS, name, content string, mode remotefs.WriteMode) error {
	t.Helper()
	w, err := fsys.OpenWrite(t.Context(), name, 0, mode)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, content); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

// TestDial verifies how a site is reached.
//
// Use cases:
//   - an endpoint that is not an http or https URL is invalid
//   - a wrong password fails authentication, a missing root is not found, a root that is a file is not a directory, and a server
//     that cannot be reached fails as the network does
//   - a public share needs no credentials
func TestDial(t *testing.T) {
	for _, endpoint := range []string{"::", "ftp://host/", "http://"} {
		if _, err := davfs.Dial(t.Context(), davfs.Config{Endpoint: endpoint, Client: &http.Client{}}); !errors.Is(err, remotefs.ErrInvalid) {
			t.Errorf("Dial(%q) = %v, want ErrInvalid", endpoint, err)
		}
	}
	s := davtest.Start(t, davtest.Options{})
	if _, err := davfs.Dial(t.Context(), davfs.Config{Endpoint: s.Endpoint(), Username: davtest.Username, Password: "wrong", Client: &http.Client{}}); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Dial(wrong password) = %v, want ErrAuth", err)
	}
	if _, err := davfs.Dial(t.Context(), davfs.Config{Endpoint: s.Endpoint() + "missing", Username: davtest.Username, Password: davtest.Password, Client: &http.Client{}}); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Dial(missing root) = %v, want ErrNotFound", err)
	}
	if err := s.Put("/file", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := davfs.Dial(t.Context(), davfs.Config{Endpoint: s.Endpoint() + "file", Username: davtest.Username, Password: davtest.Password, Client: &http.Client{}}); !errors.Is(err, remotefs.ErrNotDirectory) {
		t.Errorf("Dial(root that is a file) = %v, want ErrNotDirectory", err)
	}
	if _, err := davfs.Dial(t.Context(), davfs.Config{Endpoint: "http://127.0.0.1:1/", Client: &http.Client{}}); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Dial(unreachable) = %v, want ErrConnectionLost", err)
	}
	public := davtest.Start(t, davtest.Options{Anonymous: true})
	fsys, err := davfs.Dial(t.Context(), davfs.Config{Endpoint: public.Endpoint(), Client: &http.Client{}})
	if err != nil {
		t.Fatalf("Dial(public share) = %v", err)
	}
	_ = fsys.Close()
}

// TestTLSConfig verifies how the certificate of a server is judged.
//
// Use cases:
//   - a certificate the given authorities accept is trusted without asking, with the intermediates the server sends
//   - any other is judged by its fingerprint under the certificate key type, and a refusal fails the connection with it
//   - a server that presents no certificate is refused
func TestTLSConfig(t *testing.T) {
	server := httptest.NewUnstartedServer(davtest.New(davtest.Options{Anonymous: true}).Handler())
	server.StartTLS()
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	asked := 0
	trusting := func(string, string) error { asked++; return nil }
	dialWith := func(cfg *tls.Config) error {
		fsys, err := davfs.Dial(t.Context(), davfs.Config{Endpoint: server.URL + davtest.Root, Client: &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}})
		if err == nil {
			_ = fsys.Close()
		}
		return err
	}
	if err := dialWith(davfs.TLSConfig(roots, trusting)); err != nil || asked != 0 {
		t.Errorf("Dial(certificate of a known authority) = %v after %d questions, want no question", err, asked)
	}
	var keyType, fingerprint string
	refusing := func(k, f string) error {
		keyType, fingerprint = k, f
		return &remotefs.HostKeyError{Type: k, Fingerprint: f}
	}
	err := dialWith(davfs.TLSConfig(x509.NewCertPool(), refusing))
	var unknown *remotefs.HostKeyError
	if !errors.As(err, &unknown) || keyType != davfs.CertificateKeyType || fingerprint != davfs.Fingerprint(server.Certificate().Raw) {
		t.Errorf("Dial(self-signed certificate) = %v with %s %s, want a HostKeyError for its fingerprint", err, keyType, fingerprint)
	}
	chain := tls.ConnectionState{PeerCertificates: []*x509.Certificate{server.Certificate(), server.Certificate()}, ServerName: "127.0.0.1"}
	if err := davfs.TLSConfig(roots, refusing).VerifyConnection(chain); err != nil {
		t.Errorf("VerifyConnection(chain with an intermediate) = %v, want it trusted", err)
	}
	if err := davfs.TLSConfig(nil, trusting).VerifyConnection(tls.ConnectionState{}); err == nil {
		t.Error("VerifyConnection(no certificate) = nil, want a refusal")
	}
}

// TestWrap verifies how failures read as the errors of the port.
//
// Use cases:
//   - cancellations and invalid arguments stay as they are; untrusted server keys and destinations the guard refused keep their
//     error and are not retried
//   - each status reads as its error, and failures of the server or the network are a lost connection, which may be retried
func TestWrap(t *testing.T) {
	for _, kept := range []error{context.Canceled, fmt.Errorf("%w: x", remotefs.ErrInvalid)} {
		if got := davfs.Wrap("list", "/", kept); !errors.Is(got, kept) || got.Error() != kept.Error() {
			t.Errorf("Wrap(%v) = %v, want it unchanged", kept, got)
		}
	}
	key := &remotefs.HostKeyError{Type: davfs.CertificateKeyType, Fingerprint: "SHA256:x"}
	if err := davfs.Wrap("list", "/", fmt.Errorf("tls: %w", key)); !errors.Is(err, remotefs.ErrHostKeyUnknown) || remotefs.Retryable(err) {
		t.Errorf("Wrap(host key) = %v, want the host key error", err)
	}
	if err := davfs.Wrap("list", "/", fmt.Errorf("dial: %w", netguard.ErrBlocked)); !errors.Is(err, netguard.ErrBlocked) || remotefs.Retryable(err) {
		t.Errorf("Wrap(blocked) = %v, want netguard.ErrBlocked", err)
	}
	for status, want := range map[int]error{
		401: remotefs.ErrAuth, 403: remotefs.ErrPermission, 423: remotefs.ErrPermission, 404: remotefs.ErrNotFound, 409: remotefs.ErrNotFound,
		410: remotefs.ErrNotFound, 412: remotefs.ErrExists, 507: remotefs.ErrQuota, 400: remotefs.ErrInvalid, 429: remotefs.ErrConnectionLost,
		502: remotefs.ErrConnectionLost,
	} {
		answer := &davfs.StatusError{Method: "PUT", Status: status}
		if err := davfs.Wrap("write", "/f", answer); !errors.Is(err, want) || !strings.Contains(err.Error(), "PUT answered") {
			t.Errorf("Wrap(%d) = %v, want %v", status, err, want)
		}
	}
	if err := davfs.Wrap("list", "/", io.ErrUnexpectedEOF); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Wrap(network failure) = %v, want ErrConnectionLost", err)
	}
}

// TestFailures verifies that what the server refuses reaches the caller as the errors of the port.
//
// Use cases:
//   - a failed lookup fails every operation that starts with one
//   - a failed listing, removal, rename, folder creation or read fails its operation; MKCOL refused as not allowed means the
//     folder exists; a removal of a folder that is already gone is not an error
func TestFailures(t *testing.T) {
	s := newFaulty(t)
	fsys := dial(t, s.Server)
	ctx := t.Context()
	if err := s.Mkdir("/d"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("/f", []byte("content")); err != nil {
		t.Fatal(err)
	}
	s.arm("PROPFIND", "/f", http.StatusBadGateway, 0)
	for name, err := range map[string]error{
		"Stat":      second(fsys.Stat(ctx, "/f")),
		"List":      second(fsys.List(ctx, "/f")),
		"OpenRead":  second(fsys.OpenRead(ctx, "/f", 0)),
		"OpenWrite": second(fsys.OpenWrite(ctx, "/f", 0, remotefs.WriteOverwrite)),
		"Mkdir":     fsys.Mkdir(ctx, "/f"),
		"Remove":    fsys.Remove(ctx, "/f"),
		"RemoveAll": fsys.RemoveAll(ctx, "/f"),
		"Rename":    fsys.Rename(ctx, "/f", "/g"),
		"RenameTo":  fsys.Rename(ctx, "/d", "/f"),
		"Below":     second(fsys.OpenWrite(ctx, "/f/x", 0, remotefs.WriteOverwrite)),
	} {
		if !errors.Is(err, remotefs.ErrConnectionLost) {
			t.Errorf("%s with a failing lookup = %v, want ErrConnectionLost", name, err)
		}
	}
	s.arm("PROPFIND", "/d", http.StatusBadGateway, 1)
	if _, err := fsys.List(ctx, "/d"); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("List with a failing listing = %v", err)
	}
	s.arm("PROPFIND", "/d", http.StatusBadGateway, 1)
	if err := fsys.Remove(ctx, "/d"); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Remove with a failing listing = %v", err)
	}
	s.arm("DELETE", "/d", http.StatusLocked, 0)
	if err := fsys.Remove(ctx, "/d"); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("Remove of a locked folder = %v, want ErrPermission", err)
	}
	if err := fsys.RemoveAll(ctx, "/d"); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("RemoveAll of a locked folder = %v, want ErrPermission", err)
	}
	s.arm("DELETE", "/d", http.StatusNotFound, 0)
	if err := fsys.RemoveAll(ctx, "/d"); err != nil {
		t.Errorf("RemoveAll of a folder removed meanwhile = %v, want nil", err)
	}
	s.arm("MOVE", "/f", http.StatusForbidden, 0)
	if err := fsys.Rename(ctx, "/f", "/g"); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("Rename refused = %v, want ErrPermission", err)
	}
	s.arm("MKCOL", "/new", http.StatusMethodNotAllowed, 0)
	if err := fsys.Mkdir(ctx, "/new"); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("Mkdir answered 405 = %v, want ErrExists", err)
	}
	s.arm("MKCOL", "/new", http.StatusInsufficientStorage, 0)
	if err := fsys.Mkdir(ctx, "/new"); !errors.Is(err, remotefs.ErrQuota) {
		t.Errorf("Mkdir answered 507 = %v, want ErrQuota", err)
	}
	s.arm("GET", "/f", http.StatusForbidden, 0)
	if _, err := fsys.OpenRead(ctx, "/f", 0); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("OpenRead refused = %v, want ErrPermission", err)
	}
	s.arm("PROPFIND", "/", http.StatusServiceUnavailable, 0)
	if err := fsys.Ping(ctx); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Ping refused = %v, want ErrConnectionLost", err)
	}
}

// TestWrites verifies how uploads end.
//
// Use cases:
//   - an upload the server refuses fails on close with its error and leaves no file behind
//   - an upload whose context ends is aborted and leaves no file behind; closing again gives the same result
//   - writing after closing is invalid, and a write the server cut short fails
//   - writes cannot resume
func TestWrites(t *testing.T) {
	s := newFaulty(t)
	fsys := dial(t, s.Server)
	s.arm("PUT", "/full", http.StatusInsufficientStorage, 0)
	if err := write(t, fsys, "/full", "data", remotefs.WriteCreate); !errors.Is(err, remotefs.ErrQuota) {
		t.Errorf("upload to a full server = %v, want ErrQuota", err)
	}
	if _, err := fsys.Stat(t.Context(), "/full"); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Stat after a refused upload = %v, want ErrNotFound", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	w, err := fsys.OpenWrite(ctx, "/cut", 0, remotefs.WriteCreate)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "partial")
	cancel()
	if err := w.Close(); !errors.Is(err, context.Canceled) {
		t.Errorf("Close after the context ended = %v, want context.Canceled", err)
	}
	if err := w.Close(); !errors.Is(err, context.Canceled) {
		t.Errorf("second Close = %v, want the same failure", err)
	}
	if _, err := w.Write([]byte("late")); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Write after Close = %v, want ErrInvalid", err)
	}
	waitGone(t, fsys, "/cut")

	s.arm("PUT", "/early", http.StatusPreconditionFailed, 0)
	w, err = fsys.OpenWrite(t.Context(), "/early", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	var writeErr error
	for i := 0; i < 1000 && writeErr == nil; i++ {
		_, writeErr = w.Write(make([]byte, 64<<10))
	}
	if !errors.Is(writeErr, remotefs.ErrExists) {
		t.Errorf("Write refused by the server = %v, want ErrExists", writeErr)
	}
	_ = w.Close()
	if _, err := fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteResume); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("OpenWrite(resume) = %v, want ErrInvalid", err)
	}
}

// waitGone waits for a path to disappear, since an aborted upload is removed after the server notices it ended.
func waitGone(t *testing.T, fsys remotefs.RemoteFS, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := fsys.Stat(t.Context(), name)
		if errors.Is(err, remotefs.ErrNotFound) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Stat(%s) = %v, want it removed", name, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestReads verifies reads from servers that ignore ranges.
//
// Use cases:
//   - a read from an offset skips the first bytes of the whole file the server answers
func TestReads(t *testing.T) {
	s := davtest.Start(t, davtest.Options{IgnoreRanges: true})
	if err := s.Put("/f", []byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	fsys := dial(t, s)
	r, err := fsys.OpenRead(t.Context(), "/f", 4)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	_ = r.Close()
	if string(got) != "456789" {
		t.Errorf("read from 4 = %q, want 456789", got)
	}
}

// TestMetadata verifies the operations WebDAV does not have, and the description of the root.
//
// Use cases:
//   - modes and times cannot be changed, and the adapter claims no optional capability
//   - the root is a folder; relative paths are invalid, and the root cannot be written
func TestMetadata(t *testing.T) {
	s := davtest.Start(t, davtest.Options{})
	if err := s.Put("/f", []byte("x")); err != nil {
		t.Fatal(err)
	}
	fsys := dial(t, s)
	if err := fsys.Chmod(t.Context(), "/f", 0o600); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Chmod = %v, want ErrInvalid", err)
	}
	if err := fsys.Chtimes(t.Context(), "/f", time.Now()); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Chtimes = %v, want ErrInvalid", err)
	}
	if fsys.Capabilities() != (remotefs.Capabilities{}) {
		t.Errorf("Capabilities() = %+v, want none", fsys.Capabilities())
	}
	if e, err := fsys.Stat(t.Context(), "/"); err != nil || !e.IsDir() || e.Path != "/" {
		t.Errorf("Stat(/) = %+v, %v, want the root folder", e, err)
	}
	if _, err := fsys.Stat(t.Context(), "relative"); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Stat(relative) = %v, want ErrInvalid", err)
	}
	if err := fsys.Rename(t.Context(), "/f", "relative"); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Rename(to a relative path) = %v, want ErrInvalid", err)
	}
	if _, err := fsys.OpenWrite(t.Context(), "/", 0, remotefs.WriteOverwrite); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("OpenWrite(/) = %v, want ErrInvalid", err)
	}
}

// second returns the error of a call that also returns a value.
func second[T any](_ T, err error) error { return err }
