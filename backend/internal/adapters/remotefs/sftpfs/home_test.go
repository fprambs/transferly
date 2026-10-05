package sftpfs_test

import (
	"errors"
	"testing"

	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// TestFS_Home verifies the folder a session starts in, as pwd tells it.
//
// Use cases:
//   - a server that opens sessions in a home folder tells it, and one that opens them at the root tells the root
//   - an answer that is not an absolute path means the root
//   - a failure of the server is reported, and a closed connection is a lost one
func TestFS_Home(t *testing.T) {
	for home, want := range map[string]string{"/home/deploy": "/home/deploy", "": "/", "relative": "/"} {
		fsys := dial(t, startServer(t, sftptest.Options{Home: home}))
		if got, err := fsys.Home(t.Context()); err != nil || got != want {
			t.Errorf("Home() with %q = %q, %v; want %q", home, got, err, want)
		}
	}
	refusing := dial(t, startServer(t, sftptest.Options{Fault: func(method, _ string) error {
		if method == "RealPath" {
			return errors.New("no")
		}
		return nil
	}}))
	if _, err := refusing.Home(t.Context()); err == nil {
		t.Error("Home() on a server that refuses = nil error")
	}
	_ = refusing.Close()
	if _, err := refusing.Home(t.Context()); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Home() after Close = %v, want ErrConnectionLost", err)
	}
}
