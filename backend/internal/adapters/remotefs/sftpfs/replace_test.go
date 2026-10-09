package sftpfs_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/pkg/sftp"
)

// TestReplace checks native atomic replacement and refusal when the server did not negotiate it.
func TestReplace(t *testing.T) {
	f := dial(t, startServer(t, sftptest.Options{}))
	for _, name := range []string{"/source", "/target"} {
		w, err := f.OpenWrite(t.Context(), name, 0, remotefs.WriteCreate)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, name)
		_ = w.Close()
	}
	if err := f.Replace(t.Context(), "/source", "/target"); err != nil {
		t.Fatal(err)
	}
	r, _ := f.OpenRead(t.Context(), "/target", 0)
	defer func() { _ = r.Close() }()
	data, _ := io.ReadAll(r)
	if string(data) != "/source" {
		t.Fatalf("replacement = %q", data)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.Replace(ctx, "/source", "/target"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := sftp.SetSFTPExtensions(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = sftp.SetSFTPExtensions("hardlink@openssh.com", "posix-rename@openssh.com", "statvfs@openssh.com")
	}()
	unsupported := dial(t, startServer(t, sftptest.Options{}))
	if err := unsupported.Replace(t.Context(), "/source", "/target"); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Fatal(err)
	}
}
