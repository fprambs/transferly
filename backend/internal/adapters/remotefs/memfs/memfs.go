// Package memfs is an in-memory [remotefs.RemoteFS]. It is the reference implementation of the
// conformance suite and a test double for code that needs a remote file system.
package memfs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// node is a file or directory.
type node struct {
	mode    fs.FileMode
	data    []byte
	modTime time.Time
}

// FS is an in-memory file system safe for concurrent use.
type FS struct {
	clock  ports.Clock
	mu     sync.Mutex
	nodes  map[string]*node
	closed bool
}

// New returns an empty file system whose modification times come from clock.
func New(clock ports.Clock) *FS {
	return &FS{
		clock: clock,
		nodes: map[string]*node{"/": {mode: fs.ModeDir | 0o755, modTime: clock.Now()}},
	}
}

var _ remotefs.RemoteFS = (*FS)(nil)

// clean returns the absolute, normalized form of p.
func clean(p string) string {
	return path.Clean("/" + p)
}

// begin validates the context and the open state, then locks the file system.
// The caller must unlock. It returns the context error, or ErrConnectionLost after Close.
func (f *FS) begin(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return remotefs.ErrConnectionLost
	}
	return nil
}

// entry builds the public description of a node.
func entry(p string, n *node) remotefs.Entry {
	return remotefs.Entry{
		Name:             path.Base(p),
		Path:             p,
		Size:             int64(len(n.data)),
		Mode:             n.mode,
		ModTime:          n.modTime,
		ModTimePrecision: remotefs.PrecisionSecond,
	}
}

// fail wraps a sentinel error with the operation and path.
func fail(op, p string, err error) error {
	return fmt.Errorf("%s %s: %w", op, p, err)
}

// parentDir checks that the parent of p exists and is a directory.
func (f *FS) parentDir(op, p string) error {
	parent, ok := f.nodes[path.Dir(p)]
	if !ok {
		return fail(op, p, remotefs.ErrNotFound)
	}
	if !parent.mode.IsDir() {
		return fail(op, p, remotefs.ErrNotDirectory)
	}
	return nil
}

// List implements [remotefs.RemoteFS].
func (f *FS) List(ctx context.Context, dir string) ([]remotefs.Entry, error) {
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	defer f.mu.Unlock()

	dir = clean(dir)
	n, ok := f.nodes[dir]
	if !ok {
		return nil, fail("list", dir, remotefs.ErrNotFound)
	}
	if !n.mode.IsDir() {
		return nil, fail("list", dir, remotefs.ErrNotDirectory)
	}
	var entries []remotefs.Entry
	for p, child := range f.nodes {
		if p != "/" && path.Dir(p) == dir {
			entries = append(entries, entry(p, child))
		}
	}
	return entries, nil
}

// Stat implements [remotefs.RemoteFS].
func (f *FS) Stat(ctx context.Context, p string) (remotefs.Entry, error) {
	if err := f.begin(ctx); err != nil {
		return remotefs.Entry{}, err
	}
	defer f.mu.Unlock()

	p = clean(p)
	n, ok := f.nodes[p]
	if !ok {
		return remotefs.Entry{}, fail("stat", p, remotefs.ErrNotFound)
	}
	return entry(p, n), nil
}

// OpenRead implements [remotefs.RemoteFS].
func (f *FS) OpenRead(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	defer f.mu.Unlock()

	p = clean(p)
	n, ok := f.nodes[p]
	if !ok {
		return nil, fail("open", p, remotefs.ErrNotFound)
	}
	if n.mode.IsDir() {
		return nil, fail("open", p, remotefs.ErrInvalid)
	}
	if offset < 0 || offset > int64(len(n.data)) {
		return nil, fail("open", p, remotefs.ErrInvalid)
	}
	return io.NopCloser(bytes.NewReader(bytes.Clone(n.data[offset:]))), nil
}

// writer writes into a file at a moving position.
type writer struct {
	fs   *FS
	path string
	pos  int64
}

// Write stores p at the current position, growing the file with zeros if needed.
func (w *writer) Write(p []byte) (int, error) {
	w.fs.mu.Lock()
	defer w.fs.mu.Unlock()

	if w.fs.closed {
		return 0, remotefs.ErrConnectionLost
	}
	n, ok := w.fs.nodes[w.path]
	if !ok {
		return 0, fail("write", w.path, remotefs.ErrNotFound)
	}
	end := w.pos + int64(len(p))
	if end > int64(len(n.data)) {
		n.data = append(n.data, make([]byte, end-int64(len(n.data)))...)
	}
	copy(n.data[w.pos:], p)
	n.modTime = w.fs.clock.Now()
	w.pos = end
	return len(p), nil
}

// Close ends the write.
func (w *writer) Close() error { return nil }

// OpenWrite implements [remotefs.RemoteFS].
func (f *FS) OpenWrite(ctx context.Context, p string, offset int64, mode remotefs.WriteMode) (io.WriteCloser, error) {
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	defer f.mu.Unlock()

	p = clean(p)
	if err := f.parentDir("open", p); err != nil {
		return nil, err
	}
	n, exists := f.nodes[p]
	if exists && n.mode.IsDir() {
		return nil, fail("open", p, remotefs.ErrInvalid)
	}

	switch mode {
	case remotefs.WriteCreate:
		if exists {
			return nil, fail("open", p, remotefs.ErrExists)
		}
		f.nodes[p] = &node{mode: 0o644, modTime: f.clock.Now()}
		offset = 0
	case remotefs.WriteOverwrite:
		f.nodes[p] = &node{mode: 0o644, modTime: f.clock.Now()}
		offset = 0
	case remotefs.WriteResume:
		if !exists {
			return nil, fail("open", p, remotefs.ErrNotFound)
		}
		if offset < 0 || offset > int64(len(n.data)) {
			return nil, fail("open", p, remotefs.ErrInvalid)
		}
	default:
		return nil, fail("open", p, remotefs.ErrInvalid)
	}
	return &writer{fs: f, path: p, pos: offset}, nil
}

// Mkdir implements [remotefs.RemoteFS].
func (f *FS) Mkdir(ctx context.Context, p string) error {
	if err := f.begin(ctx); err != nil {
		return err
	}
	defer f.mu.Unlock()

	p = clean(p)
	if _, exists := f.nodes[p]; exists {
		return fail("mkdir", p, remotefs.ErrExists)
	}
	if err := f.parentDir("mkdir", p); err != nil {
		return err
	}
	f.nodes[p] = &node{mode: fs.ModeDir | 0o755, modTime: f.clock.Now()}
	return nil
}

// hasChildren reports whether any node lives below dir.
func (f *FS) hasChildren(dir string) bool {
	for p := range f.nodes {
		if p != dir && strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}

// Remove implements [remotefs.RemoteFS].
func (f *FS) Remove(ctx context.Context, p string) error {
	if err := f.begin(ctx); err != nil {
		return err
	}
	defer f.mu.Unlock()

	p = clean(p)
	n, ok := f.nodes[p]
	if !ok {
		return fail("remove", p, remotefs.ErrNotFound)
	}
	if p == "/" {
		return fail("remove", p, remotefs.ErrPermission)
	}
	if n.mode.IsDir() && f.hasChildren(p) {
		return fail("remove", p, remotefs.ErrNotEmpty)
	}
	delete(f.nodes, p)
	return nil
}

// RemoveAll implements [remotefs.RemoteFS].
func (f *FS) RemoveAll(ctx context.Context, p string) error {
	if err := f.begin(ctx); err != nil {
		return err
	}
	defer f.mu.Unlock()

	p = clean(p)
	if p == "/" {
		return fail("remove", p, remotefs.ErrPermission)
	}
	for existing := range f.nodes {
		if existing == p || strings.HasPrefix(existing, p+"/") {
			delete(f.nodes, existing)
		}
	}
	return nil
}

// Rename implements [remotefs.RemoteFS].
func (f *FS) Rename(ctx context.Context, from, to string) error {
	if err := f.begin(ctx); err != nil {
		return err
	}
	defer f.mu.Unlock()

	from, to = clean(from), clean(to)
	if _, ok := f.nodes[from]; !ok {
		return fail("rename", from, remotefs.ErrNotFound)
	}
	if _, exists := f.nodes[to]; exists {
		return fail("rename", to, remotefs.ErrExists)
	}
	if from == "/" || strings.HasPrefix(to, from+"/") {
		return fail("rename", from, remotefs.ErrInvalid)
	}
	if err := f.parentDir("rename", to); err != nil {
		return err
	}
	moved := map[string]*node{}
	for p, n := range f.nodes {
		if p == from || strings.HasPrefix(p, from+"/") {
			moved[to+strings.TrimPrefix(p, from)] = n
			delete(f.nodes, p)
		}
	}
	for p, n := range moved {
		f.nodes[p] = n
	}
	return nil
}

// Chmod implements [remotefs.RemoteFS].
func (f *FS) Chmod(ctx context.Context, p string, mode fs.FileMode) error {
	if err := f.begin(ctx); err != nil {
		return err
	}
	defer f.mu.Unlock()

	p = clean(p)
	n, ok := f.nodes[p]
	if !ok {
		return fail("chmod", p, remotefs.ErrNotFound)
	}
	n.mode = n.mode&fs.ModeType | mode.Perm()
	return nil
}

// Chtimes implements [remotefs.RemoteFS].
func (f *FS) Chtimes(ctx context.Context, p string, mtime time.Time) error {
	if err := f.begin(ctx); err != nil {
		return err
	}
	defer f.mu.Unlock()

	p = clean(p)
	n, ok := f.nodes[p]
	if !ok {
		return fail("chtimes", p, remotefs.ErrNotFound)
	}
	n.modTime = mtime.UTC()
	return nil
}

// Capabilities implements [remotefs.RemoteFS]; every optional feature is supported.
func (f *FS) Capabilities() remotefs.Capabilities {
	return remotefs.Capabilities{Resume: true, Rewrite: true, Chmod: true, Chtimes: true}
}

// Ping implements [remotefs.RemoteFS].
func (f *FS) Ping(ctx context.Context) error {
	if err := f.begin(ctx); err != nil {
		return err
	}
	f.mu.Unlock()
	return nil
}

// Close implements [remotefs.RemoteFS]. It is idempotent.
func (f *FS) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}
