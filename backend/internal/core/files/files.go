// Package files implements the operations users perform on the files of a remote site: browsing, downloading,
// uploading and changing them. It checks the user's permissions, borrows connections from the pool and keeps
// them borrowed only as long as a stream is open.
package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

const maxPathLength = 4096

// ErrRangeNotSatisfiable means the requested byte range lies outside the file.
var ErrRangeNotSatisfiable = errors.New("range not satisfiable")

// RangeError is the [ErrRangeNotSatisfiable] of a file of a known size, which the answer to the client carries.
type RangeError struct {
	// Size is the size of the file.
	Size int64
}

// Error describes the failure.
func (e *RangeError) Error() string {
	return fmt.Sprintf("%v: the file has %d bytes", ErrRangeNotSatisfiable, e.Size)
}

// Is matches [ErrRangeNotSatisfiable].
func (e *RangeError) Is(target error) bool { return target == ErrRangeNotSatisfiable }

// Authorizer checks that a user may perform an action in a workspace.
type Authorizer interface {
	// Authorize returns the role of the user or an error that says why the action is refused.
	Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action workspace.Action) (workspace.Role, error)
}

// Pool lends connections to remote servers.
type Pool interface {
	// Acquire borrows a connection for the site of key; the caller releases it.
	Acquire(ctx context.Context, key connections.Key, session string) (*connections.Lease, error)
}

var _ Pool = (*connections.Manager)(nil)

// Observer follows the data of an upload or download.
type Observer interface {
	// Started is called when the transfer got its connection and begins to move data. A non-negative total is
	// the number of bytes it will move.
	Started(total int64)
	// Progress is called with the number of bytes that just moved.
	Progress(n int64)
}

// started tells an observer, if there is one, that data begins to move.
func started(o Observer, total int64) {
	if o != nil {
		o.Started(total)
	}
}

// progress tells an observer, if there is one, that bytes moved.
func progress(o Observer, n int64) {
	if o != nil {
		o.Progress(n)
	}
}

// Target says who acts on which site.
type Target struct {
	// Actor is the user performing the operation.
	Actor uuid.UUID
	// Workspace is the workspace of the site.
	Workspace uuid.UUID
	// Site is the site whose files are used.
	Site uuid.UUID
	// Session identifies the actor's session, whose unlocked vault key opens the site credentials.
	Session string
	// Observer follows uploads and downloads; nil follows nothing.
	Observer Observer
	// Ready is called once the connection is open and the operation is about to run, so the connection log can tell
	// what runs after connecting; nil calls nothing.
	Ready func()
}

// key returns the pool key of the target.
func (t Target) key() connections.Key {
	return connections.Key{UserID: t.Actor, WorkspaceID: t.Workspace, SiteID: t.Site}
}

// Service performs file operations.
type Service struct {
	authz  Authorizer
	pool   Pool
	limits treeLimits
	now    func() time.Time
}

// NewService returns a service that checks permissions with authz and borrows connections from pool.
func NewService(authz Authorizer, pool Pool) *Service {
	return &Service{authz: authz, pool: pool, limits: treeLimits{depth: defaultTreeDepth, entries: defaultTreeEntries}, now: time.Now}
}

// checkPath refuses paths that cannot be valid: empty, too long, or holding control characters.
func checkPath(paths ...string) error {
	for _, p := range paths {
		if p == "" || len(p) > maxPathLength || strings.ContainsFunc(p, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return fmt.Errorf("%w: the path is empty, too long or has control characters", remotefs.ErrInvalid)
		}
	}
	return nil
}

// Check verifies that the actor may perform an action in the workspace and that the paths are valid,
// without connecting. Callers use it to refuse a request before they record anything about it.
func (s *Service) Check(ctx context.Context, t Target, action workspace.Action, paths ...string) error {
	if _, err := s.authz.Authorize(ctx, t.Actor, t.Workspace, action); err != nil {
		return err
	}
	return checkPath(paths...)
}

// begin authorizes the action, validates the paths and borrows a connection.
func (s *Service) begin(ctx context.Context, t Target, action workspace.Action, paths ...string) (*connections.Lease, error) {
	if _, err := s.authz.Authorize(ctx, t.Actor, t.Workspace, action); err != nil {
		return nil, err
	}
	if err := checkPath(paths...); err != nil {
		return nil, err
	}
	return s.pool.Acquire(ctx, t.key(), t.Session)
}

// run performs a short operation on a borrowed connection and gives it back.
func run[T any](ctx context.Context, s *Service, t Target, action workspace.Action, paths []string, op func(remotefs.RemoteFS) (T, error)) (T, error) {
	var zero T
	lease, err := s.begin(ctx, t, action, paths...)
	if err != nil {
		return zero, err
	}
	if t.Ready != nil {
		t.Ready()
	}
	result, err := op(lease.FS())
	lease.Release(err)
	if err != nil {
		return zero, err
	}
	return result, nil
}

// List returns the entries of a directory.
func (s *Service) List(ctx context.Context, t Target, dir string) ([]remotefs.Entry, error) {
	return run(ctx, s, t, workspace.ActionBrowse, []string{dir}, func(fsys remotefs.RemoteFS) ([]remotefs.Entry, error) {
		return fsys.List(ctx, dir)
	})
}

// Stat describes one path.
func (s *Service) Stat(ctx context.Context, t Target, p string) (remotefs.Entry, error) {
	return run(ctx, s, t, workspace.ActionBrowse, []string{p}, func(fsys remotefs.RemoteFS) (remotefs.Entry, error) {
		return fsys.Stat(ctx, p)
	})
}

// Mkdir creates a directory and returns it.
func (s *Service) Mkdir(ctx context.Context, t Target, p string) (remotefs.Entry, error) {
	return run(ctx, s, t, workspace.ActionModify, []string{p}, func(fsys remotefs.RemoteFS) (remotefs.Entry, error) {
		if err := fsys.Mkdir(ctx, p); err != nil {
			return remotefs.Entry{}, err
		}
		return fsys.Stat(ctx, p)
	})
}

// Rename moves a file or directory and returns it at its new path. An existing destination is never replaced.
func (s *Service) Rename(ctx context.Context, t Target, from, to string) (remotefs.Entry, error) {
	return run(ctx, s, t, workspace.ActionModify, []string{from, to}, func(fsys remotefs.RemoteFS) (remotefs.Entry, error) {
		if err := fsys.Rename(ctx, from, to); err != nil {
			return remotefs.Entry{}, err
		}
		return fsys.Stat(ctx, to)
	})
}

// Delete removes a file or an empty directory, or a whole tree when recursive is set.
func (s *Service) Delete(ctx context.Context, t Target, p string, recursive bool) error {
	_, err := run(ctx, s, t, workspace.ActionModify, []string{p}, func(fsys remotefs.RemoteFS) (struct{}, error) {
		if recursive {
			return struct{}{}, fsys.RemoveAll(ctx, p)
		}
		return struct{}{}, fsys.Remove(ctx, p)
	})
	return err
}

// Range selects bytes of a file. The zero value selects all of it.
type Range struct {
	// Set is true when a range was requested.
	Set bool
	// Start is the first byte; ignored for suffix ranges.
	Start int64
	// End is the last byte, inclusive; negative means the end of the file.
	End int64
	// Suffix, when positive, selects the last Suffix bytes.
	Suffix int64
}

// resolve returns the first and last byte to send for a file of the given size, or a [RangeError].
func (r Range) resolve(size int64) (start, end int64, err error) {
	if !r.Set {
		return 0, size - 1, nil
	}
	if r.Suffix > 0 {
		if size == 0 {
			return 0, 0, &RangeError{Size: size}
		}
		return max(size-r.Suffix, 0), size - 1, nil
	}
	end = r.End
	if end < 0 || end >= size {
		end = size - 1
	}
	if r.Start < 0 || r.Start >= size || r.Start > end {
		return 0, 0, &RangeError{Size: size}
	}
	return r.Start, end, nil
}

// Download is an open stream of a file's bytes. Closing it gives the connection back.
type Download struct {
	// Entry describes the whole file.
	Entry remotefs.Entry
	// Start and End are the first and last byte of the stream, inclusive.
	Start, End int64
	// Partial is true when the stream is a range of the file.
	Partial bool
	// Body is the stream; the caller closes it.
	Body io.ReadCloser
}

// Length returns the number of bytes the stream holds.
func (d *Download) Length() int64 { return d.End - d.Start + 1 }

// body reads a limited part of a remote file and returns the connection when closed.
type body struct {
	ctx     context.Context
	src     io.Reader
	whole   io.WriterTo
	closer  io.Closer
	lease   *connections.Lease
	obs     Observer
	readErr error
	closed  bool
}

// Read reads from the file, counts what it read and remembers a failure of the connection. It stops with the
// context error once the context ended.
func (b *body) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := b.src.Read(p)
	progress(b.obs, int64(n))
	if err != nil && !errors.Is(err, io.EOF) {
		b.readErr = err
	}
	return n, err
}

// WriteTo implements [io.WriterTo]. When the stream runs to the end of the file and the file can write itself, the destination is handed
// to it: SFTP then reads ahead with several requests in flight instead of one chunk per round trip, which is what makes a download as fast
// as other clients over a link with latency (4.13). Progress, bandwidth caps and the end of the context still apply to every chunk written.
// Otherwise it copies chunk by chunk through [body.Read].
func (b *body) WriteTo(w io.Writer) (int64, error) {
	if b.whole == nil {
		return io.Copy(w, struct{ io.Reader }{b})
	}
	sink := &progressWriter{ctx: b.ctx, dst: w, obs: b.obs}
	n, err := b.whole.WriteTo(sink)
	if err != nil && (sink.err == nil || !errors.Is(err, sink.err)) {
		b.readErr = err
	}
	return n, err
}

// progressWriter writes a download to its destination, reporting every chunk and stopping once the context ended. It remembers its own
// failure, so a failure of the destination is told apart from one of the file.
type progressWriter struct {
	ctx context.Context
	dst io.Writer
	obs Observer
	err error
}

// Write implements [io.Writer].
func (w *progressWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		w.err = err
		return 0, err
	}
	n, err := w.dst.Write(p)
	progress(w.obs, int64(n))
	if err != nil {
		w.err = err
	}
	return n, err
}

// Close closes the remote file and gives the connection back; a lost connection is discarded.
func (b *body) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true
	err := b.closer.Close()
	b.lease.Release(errors.Join(b.readErr, err))
	return err
}

// Download opens a file, or a range of it, for streaming. Directories cannot be downloaded.
func (s *Service) Download(ctx context.Context, t Target, p string, r Range) (*Download, error) {
	lease, err := s.begin(ctx, t, workspace.ActionDownload, p)
	if err != nil {
		return nil, err
	}
	entry, err := lease.FS().Stat(ctx, p)
	if err == nil && entry.IsDir() {
		err = fmt.Errorf("%w: %s is a directory", remotefs.ErrInvalid, p)
	}
	if err != nil {
		lease.Release(err)
		return nil, err
	}
	start, end, err := r.resolve(entry.Size)
	if err != nil {
		lease.Release(nil)
		return nil, err
	}
	src, err := lease.FS().OpenRead(ctx, p, start)
	if err != nil {
		lease.Release(err)
		return nil, err
	}
	d := &Download{Entry: entry, Start: start, End: end, Partial: r.Set}
	started(t.Observer, d.Length())
	b := &body{ctx: ctx, src: io.LimitReader(src, d.Length()), closer: src, lease: lease, obs: t.Observer}
	if whole, ok := src.(io.WriterTo); ok && end == entry.Size-1 {
		b.whole = whole
	}
	d.Body = b
	return d, nil
}

// partSuffix names the temporary file an upload is staged into, so a reader never sees a half-written file at
// the real path and an interrupted upload leaves something a resume can find and finish.
const partSuffix = ".part"

// Upload writes the request body to a temporary "<path>.part" file and, once it is complete, renames it onto
// path, returning the file afterwards. The mode says what happens when path exists: create refuses, overwrite
// replaces it, resume continues an interrupted upload at offset, which is the size of the ".part" file already
// staged there and must not exceed it. Offsets are only valid for resume. A body that ends early, or a failure
// while renaming, leaves the ".part" file in place so a resumed upload can complete it; path itself is only ever
// created or replaced at the very end, atomically, once the whole body has been written.
func (s *Service) Upload(ctx context.Context, t Target, p string, mode remotefs.WriteMode, offset int64, data io.Reader) (remotefs.Entry, error) {
	if offset < 0 || (offset != 0 && mode != remotefs.WriteResume) {
		return remotefs.Entry{}, fmt.Errorf("%w: an offset is only valid when resuming", remotefs.ErrInvalid)
	}
	lease, err := s.begin(ctx, t, workspace.ActionUpload, p)
	if err != nil {
		return remotefs.Entry{}, err
	}
	part := p + partSuffix
	if mode == remotefs.WriteCreate {
		if _, statErr := lease.FS().Stat(ctx, p); statErr == nil {
			lease.Release(nil)
			return remotefs.Entry{}, fmt.Errorf("%w: %s", remotefs.ErrExists, p)
		} else if !errors.Is(statErr, remotefs.ErrNotFound) {
			lease.Release(statErr)
			return remotefs.Entry{}, statErr
		}
	}
	openMode := mode
	if openMode != remotefs.WriteResume {
		openMode = remotefs.WriteOverwrite
	}
	w, err := lease.FS().OpenWrite(ctx, part, offset, openMode)
	if err != nil {
		lease.Release(err)
		return remotefs.Entry{}, err
	}
	started(t.Observer, -1)
	_, err = io.Copy(w, &countingReader{ctx: ctx, src: data, obs: t.Observer})
	if closeErr := w.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		lease.Release(err)
		return remotefs.Entry{}, err
	}
	if err := s.finish(ctx, lease.FS(), part, p, mode); err != nil {
		lease.Release(err)
		return remotefs.Entry{}, err
	}
	entry, err := lease.FS().Stat(ctx, p)
	lease.Release(err)
	return entry, err
}

// finish makes the completed ".part" file take the place of path. When mode is not create, any file already at
// path is removed first so the rename, which never replaces an existing target, can succeed. A create whose
// rename loses a race to a file that appeared at path in the meantime discards the ".part" file, since the
// upload it staged must not land under a different name.
func (s *Service) finish(ctx context.Context, fsys remotefs.RemoteFS, part, p string, mode remotefs.WriteMode) error {
	if mode != remotefs.WriteCreate {
		if err := fsys.Remove(ctx, p); err != nil && !errors.Is(err, remotefs.ErrNotFound) {
			return err
		}
	}
	if err := fsys.Rename(ctx, part, p); err != nil {
		if mode == remotefs.WriteCreate && errors.Is(err, remotefs.ErrExists) {
			_ = fsys.Remove(ctx, part)
		}
		return err
	}
	return nil
}

// countingReader counts what it reads and stops with the context error once the context ended.
type countingReader struct {
	ctx context.Context
	src io.Reader
	obs Observer
}

// Read reads from the source and reports the bytes.
func (c *countingReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.src.Read(p)
	progress(c.obs, int64(n))
	return n, err
}
