package sftpfs_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// dialSudo connects to a test server in sudo mode with the sudo password given, within timeout when it is not zero.
func dialSudo(ctx context.Context, t *testing.T, s *sftptest.Server, password string, timeout time.Duration) (*sftpfs.FS, error) {
	t.Helper()
	fsys, err := sftpfs.Dial(ctx, sftpfs.Config{
		Address: s.Addr, Username: testUser, Password: testPassword, VerifyHostKey: trustAll, Timeout: timeout,
		Sudo: &sftpfs.Sudo{User: "root", Password: password},
	})
	if err == nil {
		t.Cleanup(func() { _ = fsys.Close() })
	}
	return fsys, err
}

// TestSudo verifies the sudo mode of a session (ADR-0027).
//
// Use cases:
//   - sudo without a password, and with the password it asks for, starts an SFTP session that works and offers no commands
//   - a missing or wrong password is refused, and so are sudo refusing the account, a server that runs no commands and one that
//     opens no sessions
//   - sudo that never answers fails as a lost connection within the timeout, an ended context ends the wait, and an SFTP server
//     that answers garbage fails as a lost connection
func TestSudo(t *testing.T) {
	for name, opt := range map[string]sftptest.Sudo{"no password": {}, "password": {Password: "root-pass"}} {
		s := startServer(t, sftptest.Options{Users: map[string]string{testUser: testPassword}, Sudo: &opt})
		fsys, err := dialSudo(t.Context(), t, s, opt.Password, 0)
		if err != nil {
			t.Fatalf("%s: Dial() = %v", name, err)
		}
		put(t, fsys, "/f", "x")
		if entries, err := fsys.List(t.Context(), "/"); err != nil || len(entries) != 1 {
			t.Errorf("%s: List() = %v, %v", name, entries, err)
		}
		if tools, _ := fsys.Tools(t.Context()); tools.Copy {
			t.Errorf("%s: Tools() = %+v, want none", name, tools)
		}
	}
	asking := startServer(t, sftptest.Options{Users: map[string]string{testUser: testPassword}, Sudo: &sftptest.Sudo{Password: "root-pass"}})
	for name, password := range map[string]string{"missing": "", "wrong": "nope"} {
		if _, err := dialSudo(t.Context(), t, asking, password, 0); !errors.Is(err, remotefs.ErrElevationRefused) {
			t.Errorf("Dial() with a %s password = %v, want ErrElevationRefused", name, err)
		}
	}
	for name, opt := range map[string]sftptest.Options{
		"refusing sudo": {Sudo: &sftptest.Sudo{Exit: "transferly is not in the sudoers file.\n"}},
		"no commands":   {},
		"no sessions":   {NoSessions: true},
	} {
		opt.Users = map[string]string{testUser: testPassword}
		_, err := dialSudo(t.Context(), t, startServer(t, opt), "", 0)
		if !errors.Is(err, remotefs.ErrElevationRefused) {
			t.Errorf("Dial() with %s = %v, want ErrElevationRefused", name, err)
		}
		if name == "refusing sudo" && !strings.Contains(err.Error(), "sudoers") {
			t.Errorf("the refusal %q does not tell why", err)
		}
	}
	hanging := startServer(t, sftptest.Options{Users: map[string]string{testUser: testPassword}, Sudo: &sftptest.Sudo{Hang: true}})
	if _, err := dialSudo(t.Context(), t, hanging, "", 300*time.Millisecond); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Dial() with sudo that never answers = %v, want ErrConnectionLost", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	if _, err := dialSudo(ctx, t, hanging, "", 0); !errors.Is(err, context.Canceled) {
		t.Errorf("Dial() canceled while sudo waits = %v, want context.Canceled", err)
	}
	garbage := startServer(t, sftptest.Options{Users: map[string]string{testUser: testPassword}, Sudo: &sftptest.Sudo{Garbage: true}})
	if _, err := dialSudo(t.Context(), t, garbage, "", 0); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Dial() with an SFTP server that answers garbage = %v, want ErrConnectionLost", err)
	}
}
