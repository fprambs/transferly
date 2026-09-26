package remotefstest

import (
	"context"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// Prefixed presents a directory of another file system as its root, so a shared server can run isolated tests.
// Closing it removes the directory first.
type Prefixed struct {
	// Inner is the shared file system.
	Inner remotefs.RemoteFS
	// Prefix is the directory of Inner that becomes the root.
	Prefix string
}

// real maps a path of the test root to a path of the shared server.
func (p Prefixed) real(name string) string { return path.Join(p.Prefix, path.Clean("/"+name)) }

// entry maps an entry of the shared server back to the test root.
func (p Prefixed) entry(e remotefs.Entry) remotefs.Entry {
	e.Path = "/" + strings.TrimPrefix(strings.TrimPrefix(e.Path, p.Prefix), "/")
	return e
}

// List implements remotefs.RemoteFS.
func (p Prefixed) List(ctx context.Context, dir string) ([]remotefs.Entry, error) {
	entries, err := p.Inner.List(ctx, p.real(dir))
	for i := range entries {
		entries[i] = p.entry(entries[i])
	}
	return entries, err
}

// Stat implements remotefs.RemoteFS.
func (p Prefixed) Stat(ctx context.Context, name string) (remotefs.Entry, error) {
	e, err := p.Inner.Stat(ctx, p.real(name))
	return p.entry(e), err
}

// OpenRead implements remotefs.RemoteFS.
func (p Prefixed) OpenRead(ctx context.Context, name string, offset int64) (io.ReadCloser, error) {
	return p.Inner.OpenRead(ctx, p.real(name), offset)
}

// OpenWrite implements remotefs.RemoteFS.
func (p Prefixed) OpenWrite(ctx context.Context, name string, offset int64, mode remotefs.WriteMode) (io.WriteCloser, error) {
	return p.Inner.OpenWrite(ctx, p.real(name), offset, mode)
}

// Mkdir implements remotefs.RemoteFS.
func (p Prefixed) Mkdir(ctx context.Context, name string) error {
	return p.Inner.Mkdir(ctx, p.real(name))
}

// Remove implements remotefs.RemoteFS; the root of the test directory cannot be removed.
func (p Prefixed) Remove(ctx context.Context, name string) error {
	if path.Clean("/"+name) == "/" {
		return remotefs.ErrPermission
	}
	return p.Inner.Remove(ctx, p.real(name))
}

// RemoveAll implements remotefs.RemoteFS; the root of the test directory cannot be removed.
func (p Prefixed) RemoveAll(ctx context.Context, name string) error {
	if path.Clean("/"+name) == "/" {
		return remotefs.ErrPermission
	}
	return p.Inner.RemoveAll(ctx, p.real(name))
}

// Rename implements remotefs.RemoteFS.
func (p Prefixed) Rename(ctx context.Context, from, to string) error {
	return p.Inner.Rename(ctx, p.real(from), p.real(to))
}

// Chmod implements remotefs.RemoteFS.
func (p Prefixed) Chmod(ctx context.Context, name string, mode fs.FileMode) error {
	return p.Inner.Chmod(ctx, p.real(name), mode)
}

// Chtimes implements remotefs.RemoteFS.
func (p Prefixed) Chtimes(ctx context.Context, name string, mtime time.Time) error {
	return p.Inner.Chtimes(ctx, p.real(name), mtime)
}

// Capabilities implements remotefs.RemoteFS.
func (p Prefixed) Capabilities() remotefs.Capabilities { return p.Inner.Capabilities() }

// Ping implements remotefs.RemoteFS.
func (p Prefixed) Ping(ctx context.Context) error { return p.Inner.Ping(ctx) }

// Close implements remotefs.RemoteFS, removing the test directory first.
func (p Prefixed) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = p.Inner.RemoveAll(ctx, p.Prefix)
	return p.Inner.Close()
}
