package sftpfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"testing"

	"github.com/pkg/sftp"

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
		if got := handshakeError("h:22", nil, err); !errors.Is(got, remotefs.ErrAuth) {
			t.Errorf("handshakeError(%q) = %v; want ErrAuth", err, got)
		}
	}
	if got := handshakeError("h:22", remotefs.ErrHostKeyUnknown, errors.New("x")); !errors.Is(got, remotefs.ErrHostKeyUnknown) {
		t.Errorf("rejected host key = %v; want ErrHostKeyUnknown", got)
	}
	if got := handshakeError("h:22", nil, io.EOF); !errors.Is(got, remotefs.ErrConnectionLost) {
		t.Errorf("EOF = %v; want ErrConnectionLost", got)
	}
}

// TestPasswordAnswers covers [passwordAnswers].
//
// Use cases:
//   - a challenge without questions is acknowledged without answers
//   - the first challenge with questions gets the password for each one
//   - a second one fails as refused credentials instead of repeating the password
func TestPasswordAnswers(t *testing.T) {
	answer := passwordAnswers("pw")
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
}
