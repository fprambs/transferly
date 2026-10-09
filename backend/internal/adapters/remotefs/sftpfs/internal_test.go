package sftpfs

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// TestClassify verifies the mapping of client errors, including statuses a test server cannot produce.
//
// Scenarios:
//   - nil stays nil, missing and forbidden files map to their sentinels, also when wrapped
//   - the quota status is ErrQuota, the no connection and connection lost statuses are ErrConnectionLost
//   - the generic failure and other statuses are returned unchanged for the caller to refine
//   - anything that is not a server status, such as a closed stream, is a lost connection
func TestClassify(t *testing.T) {
	if classify(nil) != nil {
		t.Error("classify(nil) is not nil")
	}
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"not found", fs.ErrNotExist, remotefs.ErrNotFound},
		{"wrapped not found", fmt.Errorf("stat: %w", fs.ErrNotExist), remotefs.ErrNotFound},
		{"permission", fs.ErrPermission, remotefs.ErrPermission},
		{"quota", &sftp.StatusError{Code: statusQuotaExceeded}, remotefs.ErrQuota},
		{"no connection", &sftp.StatusError{Code: statusNoConnection}, remotefs.ErrConnectionLost},
		{"connection lost", &sftp.StatusError{Code: statusConnectionLost}, remotefs.ErrConnectionLost},
		{"end of stream", io.ErrUnexpectedEOF, remotefs.ErrConnectionLost},
		{"unknown transport error", errors.New("broken pipe"), remotefs.ErrConnectionLost},
	}
	for _, tt := range tests {
		if got := classify(tt.err); !errors.Is(got, tt.want) {
			t.Errorf("%s: classify() = %v; want %v", tt.name, got, tt.want)
		}
	}
	for _, code := range []uint32{statusFailure, 5, 8} {
		status := &sftp.StatusError{Code: code}
		if got := classify(status); !errors.Is(got, status) || errors.Is(got, remotefs.ErrConnectionLost) {
			t.Errorf("status %d: classify() = %v; want it unchanged", code, got)
		}
	}
	if !isFailure(fmt.Errorf("op: %w", &sftp.StatusError{Code: statusFailure})) || isFailure(errors.New("x")) || isFailure(&sftp.StatusError{Code: 8}) {
		t.Error("isFailure did not recognize only the generic failure status")
	}
}

// TestStreamError verifies that the end of a stream is not an error.
//
// Scenarios:
//   - nil and io.EOF pass through, and other errors are classified
func TestStreamError(t *testing.T) {
	if got := streamError(io.EOF); streamError(nil) != nil || !errors.Is(got, io.EOF) || errors.Is(got, remotefs.ErrConnectionLost) {
		t.Error("streamError altered nil or io.EOF")
	}
	if !errors.Is(streamError(&sftp.StatusError{Code: statusQuotaExceeded}), remotefs.ErrQuota) {
		t.Error("streamError did not classify a status")
	}
}

// dialInternal connects to a test server whose faults are chosen by fault.
func dialInternal(t *testing.T, fault func(method, path string) error) (*FS, *sftptest.Server) {
	t.Helper()
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{"u": "a long password"}, Fault: fault})
	fsys, err := Dial(t.Context(), Config{Address: server.Addr, Username: "u", Password: "a long password", VerifyHostKey: func(string, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fsys.Close() })
	return fsys, server
}

// TestFS_State verifies how a path is described when the server only says "failure".
//
// Scenarios:
//   - a path that exists is a file or a directory
//   - a missing path below existing directories is missing, however many levels are absent
//   - a path below a file is blocked
//   - when even the root cannot be inspected the walk ends and the path is reported as missing
//   - relative paths walk up to the current directory
func TestFS_State(t *testing.T) {
	broken := errors.New("boom")
	fsys, _ := dialInternal(t, func(method, p string) error {
		if method == "Stat" && (p == "/deep" || p == "/deep/er" || p == "/deep/er/still" || p == "/blind" || p == "/blind/x") {
			return broken
		}
		return nil
	})
	if err := fsys.Mkdir(context.Background(), "/dir"); err != nil {
		t.Fatal(err)
	}
	w, err := fsys.OpenWrite(context.Background(), "/file", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	tests := []struct {
		path string
		want pathState
	}{
		{"/dir", stateDir},
		{"/file", stateFile},
		{"/dir/absent", stateMissing},
		{"/deep/er/still/more", stateMissing},
		{"/file/below", stateBlocked},
		{"relative/absent", stateMissing},
	}
	for _, tt := range tests {
		if got := fsys.state(tt.path); got != tt.want {
			t.Errorf("state(%s) = %d; want %d", tt.path, got, tt.want)
		}
	}
}

// TestFS_StateWithoutRoot verifies that the walk up the tree ends when nothing can be inspected.
//
// Scenarios:
//   - with every lookup failing, the state is missing and the walk terminates
func TestFS_StateWithoutRoot(t *testing.T) {
	blind := errors.New("blind server")
	fsys, _ := dialInternal(t, func(method, _ string) error {
		if method == "Stat" {
			return blind
		}
		return nil
	})
	if got := fsys.state("/a/b/c"); got != stateMissing {
		t.Errorf("state() = %d; want missing", got)
	}
}

// TestHandshakeError covers [handshakeError] with the ways a real OpenSSH server refuses credentials, which the test
// server does not reproduce.
//
// Use cases:
//   - the library's own "unable to authenticate", a failure where a keyboard-interactive question was expected, too many
//     failed attempts, and an error that already is ErrAuth are all refused credentials
//   - a rejected host key is reported as such, and anything else is a lost connection worth retrying
func TestHandshakeError(t *testing.T) {
	refused := []error{
		errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password], no supported methods remain"),
		errors.New("ssh: handshake failed: ssh: unexpected message type 51 (expected 60)"),
		errors.New("ssh: disconnect, reason 2: Too many authentication failures"),
		fmt.Errorf("ssh: handshake failed: %w", remotefs.ErrAuth),
	}
	for _, err := range refused {
		if got := handshakeError("h:22", nil, false, err); !errors.Is(got, remotefs.ErrAuth) {
			t.Errorf("handshakeError(%q) = %v; want ErrAuth", err, got)
		}
	}
	if got := handshakeError("h:22", remotefs.ErrHostKeyUnknown, false, errors.New("x")); !errors.Is(got, remotefs.ErrHostKeyUnknown) {
		t.Errorf("rejected host key = %v; want ErrHostKeyUnknown", got)
	}
	if got := handshakeError("h:22", nil, false, io.EOF); !errors.Is(got, remotefs.ErrConnectionLost) {
		t.Errorf("EOF before the key = %v; want ErrConnectionLost", got)
	}
	for _, closed := range []error{io.EOF, syscall.ECONNRESET, net.ErrClosed} {
		if got := handshakeError("h:22", nil, true, fmt.Errorf("ssh: handshake failed: %w", closed)); !errors.Is(got, remotefs.ErrAuth) {
			t.Errorf("%v after the key = %v; want ErrAuth", closed, got)
		}
	}
	if got := handshakeError("h:22", nil, true, errors.New("ssh: no common algorithm")); !errors.Is(got, remotefs.ErrConnectionLost) {
		t.Errorf("other failure after the key = %v; want ErrConnectionLost", got)
	}
}

// TestPasswordAnswers covers [passwordAnswers].
//
// Use cases:
//   - a challenge without questions is acknowledged without answers
//   - the first challenge with questions gets the password for each one
//   - a second one fails as refused credentials instead of repeating the password
//   - a password the password method already sent is not sent again: the challenge fails as refused credentials
func TestPasswordAnswers(t *testing.T) {
	sent := &atomic.Bool{}
	answer := passwordAnswers("pw", sent)
	if answers, err := answer("", "banner", nil, nil); err != nil || answers != nil {
		t.Errorf("banner = %v, %v; want no answers", answers, err)
	}
	answers, err := answer("", "", []string{"Password:", "Again:"}, []bool{false, false})
	if err != nil || len(answers) != 2 || answers[0] != "pw" || answers[1] != "pw" {
		t.Fatalf("first challenge = %v, %v", answers, err)
	}
	if _, err := answer("", "", []string{"Password:"}, []bool{false}); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("second challenge = %v; want ErrAuth", err)
	}
	sent.Store(true)
	if answers, err := passwordAnswers("pw", sent)("", "", []string{"Password:"}, []bool{false}); !errors.Is(err, remotefs.ErrAuth) || answers != nil {
		t.Errorf("challenge after the password method = %v, %v; want ErrAuth", answers, err)
	}
}

// TestStrictServers_Bound verifies that the memory of strict servers stays bounded.
//
// Use cases:
//   - a full memory forgets every server and keeps learning with the next one
func TestStrictServers_Bound(t *testing.T) {
	known := &StrictServers{}
	for i := range maxStrictServers {
		known.add(fmt.Sprint("host-", i))
	}
	if !known.Has("host-0") {
		t.Fatal("a server was forgotten before the memory was full")
	}
	known.add("one-more")
	if known.Has("host-0") || !known.Has("one-more") || len(known.servers) != 1 {
		t.Errorf("full memory = %d servers; want only the last one", len(known.servers))
	}
}

// memoryStore is a [StrictStore] in memory that can fail its reads, and counts them and its writes.
type memoryStore struct {
	addresses []string
	failList  int
	lists     int
	written   []string
}

// StrictServers lists the stored addresses, failing as many times as told first.
func (m *memoryStore) StrictServers(context.Context) ([]string, error) {
	m.lists++
	if m.failList > 0 {
		m.failList--
		return nil, errors.New("database down")
	}
	return m.addresses, nil
}

// RememberStrictServer stores an address.
func (m *memoryStore) RememberStrictServer(_ context.Context, address string) error {
	m.written = append(m.written, address)
	return nil
}

// TestStrictServers_Store verifies that strict servers outlive Transferly through a store.
//
// Use cases:
//   - the servers in the store are known on first use, and the store is read once
//   - a store that cannot be read is read again on the next use
//   - a server learned is written once, and one already known is not written again
func TestStrictServers_Store(t *testing.T) {
	store := &memoryStore{addresses: []string{"gateway:22"}, failList: 1}
	known := &StrictServers{Store: store}
	if known.Has("gateway:22") {
		t.Fatal("a store that failed was taken as read")
	}
	if !known.Has("gateway:22") || known.Has("other:22") || store.lists != 2 {
		t.Errorf("stored server known = %v after %d reads; want known after reading again", known.Has("gateway:22"), store.lists)
	}
	known.add("new:22")
	known.add("new:22")
	known.add("gateway:22")
	if !known.Has("new:22") || len(store.written) != 1 || store.written[0] != "new:22" {
		t.Errorf("written %v; want only the new server, once", store.written)
	}

	fresh := &StrictServers{Store: &memoryStore{}}
	fresh.add("first:22")
	if !fresh.Has("first:22") {
		t.Error("a server learned before the store was read was forgotten")
	}
}

// TestSafeAlgorithms checks complete secure algorithm coverage and fail-closed trusted identities.
func TestSafeAlgorithms(t *testing.T) {
	safe := ssh.SupportedAlgorithms()
	for _, algorithm := range safe.KeyExchanges {
		if !slices.Contains(keyExchanges, algorithm) {
			t.Errorf("missing secure key exchange %s", algorithm)
		}
	}
	for _, algorithm := range safe.Ciphers {
		if !slices.Contains(ciphers, algorithm) {
			t.Errorf("missing secure cipher %s", algorithm)
		}
	}
	if got := safeHostAlgorithms(nil); !slices.Equal(got, safe.HostKeys) {
		t.Fatal("host algorithms differ from secure policy")
	}
	if got := safeHostAlgorithms([]string{ssh.KeyAlgoRSA}); len(got) == 0 || slices.Contains(got, ssh.KeyAlgoRSA) {
		t.Fatal("unsafe or unrestricted host algorithms", got)
	}
	e := negotiationError(&ssh.AlgorithmNegotiationError{What: "host key", RequestedAlgorithms: []string{"ssh-rsa"}})
	var negotiation *remotefs.NegotiationError
	if !errors.As(e, &negotiation) || negotiation.Category != "host_signature" {
		t.Fatal(e)
	}
}

// TestAuthenticationSignatureNegotiation checks the legacy RSA fallback diagnostic.
func TestAuthenticationSignatureNegotiation(t *testing.T) {
	err := handshakeError("h:22", nil, true, errors.New(`ssh: handshake failed: ssh: no common public key signature algorithm, server only supports "ssh-rsa" for key type "ssh-rsa", signer only supports [rsa-sha2-256 rsa-sha2-512]`))
	var negotiation *remotefs.NegotiationError
	if !errors.As(err, &negotiation) || negotiation.Category != "authentication_signature" || remotefs.Retryable(err) {
		t.Fatal(err)
	}
}

// basicSigner deliberately lacks AlgorithmSigner, so restrictions cannot be applied.
type basicSigner struct{ ssh.Signer }

// TestSafeSignerRequiresRestrictions verifies that a signer without algorithm restrictions fails closed.
func TestSafeSignerRequiresRestrictions(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := safeSigner(basicSigner{signer}); err == nil {
		t.Fatal("unrestricted signer accepted")
	}
}

// TestNegotiationAdvertisementBounds verifies that untrusted names cannot inject controls or grow diagnostics indefinitely.
func TestNegotiationAdvertisementBounds(t *testing.T) {
	names := []string{"bad\nname\x00é", strings.Repeat("a", 256)}
	names = append(names, make([]string, 80)...)
	err := negotiationError(&ssh.AlgorithmNegotiationError{What: "public key signature algorithm", RequestedAlgorithms: names})
	var diagnostic *remotefs.NegotiationError
	if !errors.As(err, &diagnostic) || diagnostic.Category != "authentication_signature" {
		t.Fatal(err)
	}
	if len(diagnostic.ServerAlgorithms) != 64 || diagnostic.ServerAlgorithms[0] != "badname" || len(diagnostic.ServerAlgorithms[1]) != 128 {
		t.Fatalf("unbounded advertisement: %+v", diagnostic)
	}
}
