// Package azfilefs implements [remotefs.RemoteFS] on a share of Azure Files (ADR-0025): real directories, files written by
// ranges at any offset, so writes resume, native renames, and last write times that can be read and set.
package azfilefs

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/azure"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// Config tells how to reach a share.
type Config struct {
	// Service is how to reach the Files service of the account.
	Service azure.Config
	// Share is the share the file system shows.
	Share string
}

// rangeSize is the largest range Azure Files writes in one request, and the size of the chunks of uploads.
var rangeSize = 4 << 20

// pageSize is how many entries a page of a listing asks for, the most the service gives.
var pageSize = 5000

// timeFormat is how Azure Files writes times.
const timeFormat = "2006-01-02T15:04:05.0000000Z"

// Modes of the entries: Azure Files keeps ACLs, which the port does not model, so files and folders read as the usual defaults.
const (
	dirMode  = fs.ModeDir | 0o755
	fileMode = 0o644
)

// FS is a share seen as a file system. It is safe for concurrent use.
type FS struct {
	client *azure.Client
	share  string
	closed atomic.Bool
	idle   func()
}

// Dial checks that the share exists and can be reached with the credentials of cfg, and returns its file system.
func Dial(ctx context.Context, cfg Config) (*FS, error) {
	client, err := azure.New(cfg.Service)
	if err != nil {
		return nil, err
	}
	f := &FS{client: client, share: cfg.Share, idle: cfg.Service.Client.CloseIdleConnections}
	if err := f.Ping(ctx); err != nil {
		return nil, err
	}
	return f, nil
}

// begin starts an operation on p: it fails when ctx is done or the file system is closed, and returns p cleaned.
func (f *FS) begin(ctx context.Context, op, p string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if f.closed.Load() {
		return "", fmt.Errorf("%s %q: %w", op, p, remotefs.ErrConnectionLost)
	}
	if p == "" || p[0] != '/' || strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("%s %q: %w: the path must be absolute", op, p, remotefs.ErrInvalid)
	}
	return path.Clean(p), nil
}

// resource is the resource of a path of the share.
func (f *FS) resource(p string) string { return f.share + strings.TrimSuffix(p, "/") }

// do sends a request for a path, closes its answer and returns its headers.
func (f *FS) do(ctx context.Context, method, p string, query url.Values, header http.Header, body []byte) (http.Header, error) {
	resp, err := f.client.Do(ctx, method, f.resource(p), query, header, body)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()
	return resp.Header, nil
}

// directory is the query of the operations on directories, with the operation comp when given.
func directory(comp string) url.Values {
	q := url.Values{"restype": {"directory"}}
	if comp != "" {
		q.Set("comp", comp)
	}
	return q
}

// state is what a path names.
type state int

const (
	none state = iota
	file
	folder
)

// lookup tells what p names: a file, with its size and last write time, a directory, or nothing.
func (f *FS) lookup(ctx context.Context, op, p string) (state, int64, time.Time, error) {
	if p == "/" {
		return folder, 0, time.Time{}, nil
	}
	header, err := f.do(ctx, http.MethodHead, p, nil, nil, nil)
	if err == nil {
		size, _ := strconv.ParseInt(header.Get("Content-Length"), 10, 64)
		written, _ := time.Parse(timeFormat, header.Get("x-ms-file-last-write-time"))
		return file, size, written, nil
	}
	if !missing(err) {
		return none, 0, time.Time{}, azure.Wrap(op, p, err)
	}
	if _, err := f.do(ctx, http.MethodGet, p, directory(""), nil, nil); err != nil {
		if missing(err) {
			return none, 0, time.Time{}, nil
		}
		return none, 0, time.Time{}, azure.Wrap(op, p, err)
	}
	return folder, 0, time.Time{}, nil
}

// missing reports whether a failure means the path, or its parent, does not exist.
func missing(err error) bool {
	code := azure.Code(err)
	return code == "ResourceNotFound" || code == "ParentNotFound"
}

// needFolder checks that the parent of p is a directory that exists.
func (f *FS) needFolder(ctx context.Context, op, p string) error {
	switch kind, _, _, err := f.lookup(ctx, op, path.Dir(p)); {
	case err != nil:
		return err
	case kind == file:
		return fmt.Errorf("%s %q: %w: %q is a file", op, p, remotefs.ErrNotDirectory, path.Dir(p))
	case kind == none:
		return fmt.Errorf("%s %q: %w: %q does not exist", op, p, remotefs.ErrNotFound, path.Dir(p))
	}
	return nil
}

// entry describes the file or the directory at p.
func entry(p string, dir bool, size int64, written time.Time) remotefs.Entry {
	e := remotefs.Entry{Name: path.Base(p), Path: p, Size: size, Mode: fileMode, ModTime: written, ModTimePrecision: remotefs.PrecisionSecond}
	if dir {
		e.Mode, e.Size = dirMode, 0
	}
	return e
}

// item is a file or a directory of a listing.
type item struct {
	Name       string `xml:"Name"`
	Properties struct {
		ContentLength int64  `xml:"Content-Length"`
		LastWriteTime string `xml:"LastWriteTime"`
	} `xml:"Properties"`
}

// children lists the files and the directories of a directory, with their sizes and times.
func (f *FS) children(ctx context.Context, dir string) ([]item, []item, error) {
	q := directory("list")
	q.Set("include", "Timestamps")
	q.Set("maxresults", strconv.Itoa(pageSize))
	var files, dirs []item
	for {
		resp, err := f.client.Do(ctx, http.MethodGet, f.resource(dir), q, nil, nil)
		if err != nil {
			return nil, nil, err
		}
		var page struct {
			Entries struct {
				File      []item `xml:"File"`
				Directory []item `xml:"Directory"`
			} `xml:"Entries"`
			Next string `xml:"NextMarker"`
		}
		err = xml.NewDecoder(resp.Body).Decode(&page)
		_ = resp.Body.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("%w: unreadable listing: %w", remotefs.ErrConnectionLost, err)
		}
		files, dirs = append(files, page.Entries.File...), append(dirs, page.Entries.Directory...)
		if page.Next == "" {
			return files, dirs, nil
		}
		q.Set("marker", page.Next)
	}
}

// List implements remotefs.RemoteFS.
func (f *FS) List(ctx context.Context, dir string) ([]remotefs.Entry, error) {
	p, err := f.begin(ctx, "list", dir)
	if err != nil {
		return nil, err
	}
	switch kind, _, _, err := f.lookup(ctx, "list", p); {
	case err != nil:
		return nil, err
	case kind == file:
		return nil, fmt.Errorf("list %q: %w", dir, remotefs.ErrNotDirectory)
	case kind == none:
		return nil, fmt.Errorf("list %q: %w", dir, remotefs.ErrNotFound)
	}
	files, dirs, err := f.children(ctx, p)
	if err != nil {
		return nil, azure.Wrap("list", dir, err)
	}
	entries := make([]remotefs.Entry, 0, len(files)+len(dirs))
	for _, i := range files {
		written, _ := time.Parse(timeFormat, i.Properties.LastWriteTime)
		entries = append(entries, entry(path.Join(p, i.Name), false, i.Properties.ContentLength, written))
	}
	for _, i := range dirs {
		written, _ := time.Parse(timeFormat, i.Properties.LastWriteTime)
		entries = append(entries, entry(path.Join(p, i.Name), true, 0, written))
	}
	return entries, nil
}

// Stat implements remotefs.RemoteFS.
func (f *FS) Stat(ctx context.Context, name string) (remotefs.Entry, error) {
	p, err := f.begin(ctx, "stat", name)
	if err != nil {
		return remotefs.Entry{}, err
	}
	kind, size, written, err := f.lookup(ctx, "stat", p)
	switch {
	case err != nil:
		return remotefs.Entry{}, err
	case kind == none:
		return remotefs.Entry{}, fmt.Errorf("stat %q: %w", name, remotefs.ErrNotFound)
	case p == "/":
		return remotefs.Entry{Name: "/", Path: "/", Mode: dirMode}, nil
	}
	return entry(p, kind == folder, size, written), nil
}

// OpenRead implements remotefs.RemoteFS.
func (f *FS) OpenRead(ctx context.Context, name string, offset int64) (io.ReadCloser, error) {
	p, err := f.begin(ctx, "read", name)
	if err != nil {
		return nil, err
	}
	kind, size, _, err := f.lookup(ctx, "read", p)
	switch {
	case err != nil:
		return nil, err
	case kind == folder:
		return nil, fmt.Errorf("read %q: %w: it is a directory", name, remotefs.ErrInvalid)
	case kind == none:
		return nil, fmt.Errorf("read %q: %w", name, remotefs.ErrNotFound)
	case offset < 0 || offset > size:
		return nil, fmt.Errorf("read %q at %d: %w: the file has %d bytes", name, offset, remotefs.ErrInvalid, size)
	case offset == size:
		return io.NopCloser(strings.NewReader("")), nil
	}
	header := http.Header{}
	if offset > 0 {
		header.Set("x-ms-range", fmt.Sprintf("bytes=%d-", offset))
	}
	body, err := f.stream(ctx, f.resource(p), header)
	if err != nil {
		return nil, azure.Wrap("read", name, err)
	}
	return &reader{body: body, path: name}, nil
}

// stream sends a GET request for a resource and hands over the body of its answer, which the caller closes.
func (f *FS) stream(ctx context.Context, resource string, header http.Header) (io.ReadCloser, error) {
	resp, err := f.client.Do(ctx, http.MethodGet, resource, nil, header, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// reader streams a file, turning its failures into the errors of the port.
type reader struct {
	body io.ReadCloser
	path string
}

// Read implements io.Reader.
func (r *reader) Read(b []byte) (int, error) {
	n, err := r.body.Read(b)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, azure.Wrap("read", r.path, err)
	}
	return n, err
}

// Close implements io.Closer.
func (r *reader) Close() error { return r.body.Close() }

// OpenWrite implements remotefs.RemoteFS. Create and overwrite make the file anew, empty; resume writes from offset, which
// may be before the end of the file, leaving the bytes after what is written as they were.
func (f *FS) OpenWrite(ctx context.Context, name string, offset int64, mode remotefs.WriteMode) (io.WriteCloser, error) {
	p, err := f.begin(ctx, "write", name)
	if err != nil {
		return nil, err
	}
	if mode != remotefs.WriteCreate && mode != remotefs.WriteOverwrite && mode != remotefs.WriteResume {
		return nil, fmt.Errorf("write %q: %w: unknown write mode %d", name, remotefs.ErrInvalid, mode)
	}
	if p == "/" {
		return nil, fmt.Errorf("write %q: %w: it is a directory", name, remotefs.ErrInvalid)
	}
	if err := f.needFolder(ctx, "write", p); err != nil {
		return nil, err
	}
	kind, size, _, err := f.lookup(ctx, "write", p)
	switch {
	case err != nil:
		return nil, err
	case kind == folder:
		return nil, fmt.Errorf("write %q: %w: it is a directory", name, remotefs.ErrInvalid)
	case kind == file && mode == remotefs.WriteCreate:
		return nil, fmt.Errorf("write %q: %w", name, remotefs.ErrExists)
	case kind == none && mode == remotefs.WriteResume:
		return nil, fmt.Errorf("write %q: %w", name, remotefs.ErrNotFound)
	case mode == remotefs.WriteResume && (offset < 0 || offset > size):
		return nil, fmt.Errorf("write %q at %d: %w: the file has %d bytes", name, offset, remotefs.ErrInvalid, size)
	}
	if mode != remotefs.WriteResume {
		offset, size = 0, 0
		now := time.Now().UTC().Format(timeFormat)
		header := http.Header{
			"x-ms-type": {"file"}, "x-ms-content-length": {"0"}, "x-ms-file-attributes": {"None"},
			"x-ms-file-creation-time": {now}, "x-ms-file-last-write-time": {now}, "x-ms-file-permission": {"inherit"},
		}
		if _, err := f.do(ctx, http.MethodPut, p, nil, header, nil); err != nil {
			return nil, azure.Wrap("write", name, err)
		}
	}
	return &writer{ctx: ctx, fs: f, p: p, path: name, at: offset, size: size}, nil
}

// writer writes what is written to the file in ranges, growing it as needed.
type writer struct {
	ctx      context.Context
	fs       *FS
	p, path  string
	at, size int64
	buf      []byte
	done     bool
}

// Write implements io.Writer.
func (w *writer) Write(b []byte) (int, error) {
	switch {
	case w.fs.closed.Load():
		return 0, fmt.Errorf("write %q: %w", w.path, remotefs.ErrConnectionLost)
	case w.done:
		return 0, fmt.Errorf("write %q: %w: the upload is closed", w.path, remotefs.ErrInvalid)
	}
	w.buf = append(w.buf, b...)
	for len(w.buf) >= rangeSize {
		if err := w.flush(w.buf[:rangeSize]); err != nil {
			return 0, err
		}
		w.buf = append(w.buf[:0], w.buf[rangeSize:]...)
	}
	return len(b), nil
}

// flush writes data at the current offset, growing the file first when it would go past its end.
func (w *writer) flush(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	end := w.at + int64(len(data))
	if end > w.size {
		header := http.Header{
			"x-ms-content-length": {strconv.FormatInt(end, 10)}, "x-ms-file-attributes": {"preserve"},
			"x-ms-file-creation-time": {"preserve"}, "x-ms-file-last-write-time": {"now"}, "x-ms-file-permission": {"preserve"},
		}
		if _, err := w.fs.do(w.ctx, http.MethodPut, w.p, url.Values{"comp": {"properties"}}, header, nil); err != nil {
			return azure.Wrap("write", w.path, err)
		}
		w.size = end
	}
	header := http.Header{"x-ms-range": {fmt.Sprintf("bytes=%d-%d", w.at, end-1)}, "x-ms-write": {"update"}}
	if _, err := w.fs.do(w.ctx, http.MethodPut, w.p, url.Values{"comp": {"range"}}, header, data); err != nil {
		return azure.Wrap("write", w.path, err)
	}
	w.at = end
	return nil
}

// Close implements io.Closer: it writes what is left. When the context of the write ended, the rest is not written.
func (w *writer) Close() error {
	if w.done {
		return nil
	}
	w.done = true
	if err := w.ctx.Err(); err != nil {
		return err
	}
	return w.flush(w.buf)
}

// Mkdir implements remotefs.RemoteFS.
func (f *FS) Mkdir(ctx context.Context, name string) error {
	p, err := f.begin(ctx, "mkdir", name)
	if err != nil {
		return err
	}
	switch kind, _, _, err := f.lookup(ctx, "mkdir", p); {
	case err != nil:
		return err
	case kind != none:
		return fmt.Errorf("mkdir %q: %w", name, remotefs.ErrExists)
	}
	if err := f.needFolder(ctx, "mkdir", p); err != nil {
		return err
	}
	if _, err := f.do(ctx, http.MethodPut, p, directory(""), nil, nil); err != nil {
		return azure.Wrap("mkdir", name, err)
	}
	return nil
}

// Remove implements remotefs.RemoteFS.
func (f *FS) Remove(ctx context.Context, name string) error {
	p, err := f.begin(ctx, "remove", name)
	if err != nil {
		return err
	}
	if p == "/" {
		return fmt.Errorf("remove %q: %w: the root cannot be removed", name, remotefs.ErrPermission)
	}
	kind, _, _, err := f.lookup(ctx, "remove", p)
	switch {
	case err != nil:
		return err
	case kind == none:
		return fmt.Errorf("remove %q: %w", name, remotefs.ErrNotFound)
	}
	var query url.Values
	if kind == folder {
		query = directory("")
	}
	if _, err := f.do(ctx, http.MethodDelete, p, query, nil, nil); err != nil {
		return azure.Wrap("remove", name, err)
	}
	return nil
}

// removeTree deletes a directory and everything in it, files first and directories after their contents.
func (f *FS) removeTree(ctx context.Context, name, dir string) error {
	files, dirs, err := f.children(ctx, dir)
	if err != nil {
		return azure.Wrap("remove", name, err)
	}
	for _, i := range files {
		if _, err := f.do(ctx, http.MethodDelete, path.Join(dir, i.Name), nil, nil, nil); err != nil {
			return azure.Wrap("remove", name, err)
		}
	}
	for _, i := range dirs {
		if err := f.removeTree(ctx, name, path.Join(dir, i.Name)); err != nil {
			return err
		}
	}
	if _, err := f.do(ctx, http.MethodDelete, dir, directory(""), nil, nil); err != nil {
		return azure.Wrap("remove", name, err)
	}
	return nil
}

// RemoveAll implements remotefs.RemoteFS.
func (f *FS) RemoveAll(ctx context.Context, name string) error {
	p, err := f.begin(ctx, "remove", name)
	if err != nil {
		return err
	}
	if p == "/" {
		return fmt.Errorf("remove %q: %w: the root cannot be removed", name, remotefs.ErrPermission)
	}
	kind, _, _, err := f.lookup(ctx, "remove", p)
	switch {
	case err != nil:
		return err
	case kind == file:
		if _, err := f.do(ctx, http.MethodDelete, p, nil, nil, nil); err != nil {
			return azure.Wrap("remove", name, err)
		}
	case kind == folder:
		return f.removeTree(ctx, name, p)
	}
	return nil
}

// Rename implements remotefs.RemoteFS: Azure Files renames files and directories itself, never replacing what is there.
func (f *FS) Rename(ctx context.Context, from, to string) error {
	src, err := f.begin(ctx, "rename", from)
	if err != nil {
		return err
	}
	dst, err := f.begin(ctx, "rename", to)
	if err != nil {
		return err
	}
	if src == "/" || strings.HasPrefix(dst+"/", src+"/") {
		return fmt.Errorf("rename %q to %q: %w: a directory cannot move into itself", from, to, remotefs.ErrInvalid)
	}
	kind, _, _, err := f.lookup(ctx, "rename", src)
	switch {
	case err != nil:
		return err
	case kind == none:
		return fmt.Errorf("rename %q: %w", from, remotefs.ErrNotFound)
	}
	switch existing, _, _, err := f.lookup(ctx, "rename", dst); {
	case err != nil:
		return err
	case existing != none:
		return fmt.Errorf("rename %q to %q: %w", from, to, remotefs.ErrExists)
	}
	if err := f.needFolder(ctx, "rename", dst); err != nil {
		return err
	}
	query := url.Values{"comp": {"rename"}}
	if kind == folder {
		query = directory("rename")
	}
	header := http.Header{"x-ms-file-rename-source": {f.client.URL(f.resource(src), nil)}, "x-ms-file-rename-replace-if-exists": {"false"}}
	if _, err := f.do(ctx, http.MethodPut, dst, query, header, nil); err != nil {
		return azure.Wrap("rename", from, err)
	}
	return nil
}

// Chmod implements remotefs.RemoteFS; Azure Files keeps ACLs, which the port does not model.
func (f *FS) Chmod(ctx context.Context, name string, _ fs.FileMode) error {
	if _, err := f.begin(ctx, "chmod", name); err != nil {
		return err
	}
	return fmt.Errorf("chmod %q: %w: Azure Files keeps ACLs, not modes", name, remotefs.ErrInvalid)
}

// Chtimes implements remotefs.RemoteFS: it sets the last write time of a file.
func (f *FS) Chtimes(ctx context.Context, name string, mtime time.Time) error {
	p, err := f.begin(ctx, "chtimes", name)
	if err != nil {
		return err
	}
	switch kind, _, _, err := f.lookup(ctx, "chtimes", p); {
	case err != nil:
		return err
	case kind != file:
		return fmt.Errorf("chtimes %q: %w", name, remotefs.ErrNotFound)
	}
	header := http.Header{
		"x-ms-file-last-write-time": {mtime.UTC().Format(timeFormat)}, "x-ms-file-attributes": {"preserve"},
		"x-ms-file-creation-time": {"preserve"}, "x-ms-file-permission": {"preserve"},
	}
	if _, err := f.do(ctx, http.MethodPut, p, url.Values{"comp": {"properties"}}, header, nil); err != nil {
		return azure.Wrap("chtimes", name, err)
	}
	return nil
}

// Capabilities implements remotefs.RemoteFS: writes resume at any offset and times can be set; files have no modes.
func (f *FS) Capabilities() remotefs.Capabilities {
	return remotefs.Capabilities{Resume: true, Rewrite: true, Chtimes: true}
}

// Ping implements remotefs.RemoteFS: it checks that the share can still be reached.
func (f *FS) Ping(ctx context.Context) error {
	if _, err := f.begin(ctx, "ping", "/"); err != nil {
		return err
	}
	resp, err := f.client.Do(ctx, http.MethodGet, f.share, url.Values{"restype": {"share"}}, nil, nil)
	if err != nil {
		return azure.Wrap("share", f.share, err)
	}
	_ = resp.Body.Close()
	return nil
}

// Close implements remotefs.RemoteFS. Later operations, and uploads still open, fail with [remotefs.ErrConnectionLost].
func (f *FS) Close() error {
	f.closed.Store(true)
	f.idle()
	return nil
}
