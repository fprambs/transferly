package sftpfs_test

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/sftpfs"
	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// prefixed presents a directory of another file system as its root, so a shared server can run isolated tests.
type prefixed struct {
	inner  remotefs.RemoteFS
	prefix string
}

// real maps a path of the test root to a path of the shared server.
func (p prefixed) real(name string) string { return path.Join(p.prefix, path.Clean("/"+name)) }

// entry maps an entry of the shared server back to the test root.
func (p prefixed) entry(e remotefs.Entry) remotefs.Entry {
	e.Path = "/" + strings.TrimPrefix(strings.TrimPrefix(e.Path, p.prefix), "/")
	return e
}

// List implements remotefs.RemoteFS.
func (p prefixed) List(ctx context.Context, dir string) ([]remotefs.Entry, error) {
	entries, err := p.inner.List(ctx, p.real(dir))
	for i := range entries {
		entries[i] = p.entry(entries[i])
	}
	return entries, err
}

// Stat implements remotefs.RemoteFS.
func (p prefixed) Stat(ctx context.Context, name string) (remotefs.Entry, error) {
	e, err := p.inner.Stat(ctx, p.real(name))
	return p.entry(e), err
}

// OpenRead implements remotefs.RemoteFS.
func (p prefixed) OpenRead(ctx context.Context, name string, offset int64) (io.ReadCloser, error) {
	return p.inner.OpenRead(ctx, p.real(name), offset)
}

// OpenWrite implements remotefs.RemoteFS.
func (p prefixed) OpenWrite(ctx context.Context, name string, offset int64, mode remotefs.WriteMode) (io.WriteCloser, error) {
	return p.inner.OpenWrite(ctx, p.real(name), offset, mode)
}

// Mkdir implements remotefs.RemoteFS.
func (p prefixed) Mkdir(ctx context.Context, name string) error {
	return p.inner.Mkdir(ctx, p.real(name))
}

// Remove implements remotefs.RemoteFS; the root of the test directory cannot be removed.
func (p prefixed) Remove(ctx context.Context, name string) error {
	if path.Clean("/"+name) == "/" {
		return remotefs.ErrPermission
	}
	return p.inner.Remove(ctx, p.real(name))
}

// RemoveAll implements remotefs.RemoteFS; the root of the test directory cannot be removed.
func (p prefixed) RemoveAll(ctx context.Context, name string) error {
	if path.Clean("/"+name) == "/" {
		return remotefs.ErrPermission
	}
	return p.inner.RemoveAll(ctx, p.real(name))
}

// Rename implements remotefs.RemoteFS.
func (p prefixed) Rename(ctx context.Context, from, to string) error {
	return p.inner.Rename(ctx, p.real(from), p.real(to))
}

// Chmod implements remotefs.RemoteFS.
func (p prefixed) Chmod(ctx context.Context, name string, mode fs.FileMode) error {
	return p.inner.Chmod(ctx, p.real(name), mode)
}

// Chtimes implements remotefs.RemoteFS.
func (p prefixed) Chtimes(ctx context.Context, name string, mtime time.Time) error {
	return p.inner.Chtimes(ctx, p.real(name), mtime)
}

// Capabilities implements remotefs.RemoteFS.
func (p prefixed) Capabilities() remotefs.Capabilities { return p.inner.Capabilities() }

// Ping implements remotefs.RemoteFS.
func (p prefixed) Ping(ctx context.Context) error { return p.inner.Ping(ctx) }

// Close implements remotefs.RemoteFS, removing the test directory first.
func (p prefixed) Close() error {
	_ = p.inner.RemoveAll(context.Background(), p.prefix)
	return p.inner.Close()
}

// TestConformance_OpenSSH runs the shared suite against a real OpenSSH server, to catch status codes that the
// in-process server does not imitate. It is skipped unless SENTINEL_TEST_SFTP_ADDR names a server that has a
// writable directory called upload for the user sentinel, as the sftp service of the testenv profile does.
func TestConformance_OpenSSH(t *testing.T) {
	address := os.Getenv("SENTINEL_TEST_SFTP_ADDR")
	if address == "" {
		t.Skip("SENTINEL_TEST_SFTP_ADDR is not set; start the testenv profile to run this test")
	}
	remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS {
		fsys, err := sftpfs.Dial(t.Context(), sftpfs.Config{Address: address, Username: "sentinel", Password: "sentinel", VerifyHostKey: trustAll})
		if err != nil {
			t.Fatalf("Dial() = %v", err)
		}
		root := "/upload/" + uuid.NewString()
		if err := fsys.Mkdir(t.Context(), root); err != nil {
			t.Fatalf("Mkdir(%s) = %v", root, err)
		}
		return prefixed{inner: fsys, prefix: root}
	})
}
