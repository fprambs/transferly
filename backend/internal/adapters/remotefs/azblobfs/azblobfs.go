// Package azblobfs implements [remotefs.RemoteFS] on a container of Azure Blob Storage (ADR-0025). Paths are blob names;
// folders are emulated with the prefixes names share and with empty marker blobs whose name ends in a slash, as for S3
// (ADR-0023). Uploads stage blocks and commit them when they finish, so a blob appears whole or not at all.
package azblobfs

import (
	"bytes"
	"context"
	"encoding/base64"
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

// Config tells how to reach a container.
type Config struct {
	// Service is how to reach the Blob service of the account.
	Service azure.Config
	// Container is the container the file system shows.
	Container string
}

// blockSize is the size of the blocks uploads are staged in, which bounds the memory of each upload.
var blockSize = 8 << 20

// pageSize is how many blobs a page of a listing asks for, the most the service gives.
var pageSize = 5000

// copyWait is how long to wait before asking again about a copy the service reports as pending.
var copyWait = time.Second

// Modes of the entries: blobs have none, so files and folders read as the usual defaults.
const (
	dirMode  = fs.ModeDir | 0o755
	fileMode = 0o644
)

// FS is a container seen as a file system. It is safe for concurrent use.
type FS struct {
	client    *azure.Client
	container string
	closed    atomic.Bool
	idle      func()
}

// Dial checks that the container exists and can be reached with the credentials of cfg, and returns its file system.
func Dial(ctx context.Context, cfg Config) (*FS, error) {
	client, err := azure.New(cfg.Service)
	if err != nil {
		return nil, err
	}
	f := &FS{client: client, container: cfg.Container, idle: cfg.Service.Client.CloseIdleConnections}
	if err := f.Ping(ctx); err != nil {
		return nil, err
	}
	return f, nil
}

// begin starts an operation on p: it fails when ctx is done or the file system is closed, and returns the name of p, empty for
// the root.
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
	return strings.TrimPrefix(path.Clean(p), "/"), nil
}

// blob is the resource of a blob.
func (f *FS) blob(name string) string { return f.container + "/" + name }

// do sends a request for a blob, or for the container when name is empty, and closes its answer, keeping its headers.
func (f *FS) do(ctx context.Context, method, name string, query url.Values, header http.Header, body []byte) (http.Header, error) {
	resource := f.container
	if name != "" {
		resource = f.blob(name)
	}
	resp, err := f.client.Do(ctx, method, resource, query, header, body)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()
	return resp.Header, nil
}

// item is a blob or a prefix of a listing.
type item struct {
	Name       string `xml:"Name"`
	Properties struct {
		LastModified  string `xml:"Last-Modified"`
		ContentLength int64  `xml:"Content-Length"`
	} `xml:"Properties"`
}

// page is one page of a listing.
type page struct {
	Blobs struct {
		Blob   []item `xml:"Blob"`
		Prefix []item `xml:"BlobPrefix"`
	} `xml:"Blobs"`
	Next string `xml:"NextMarker"`
}

// list returns the blobs under prefix, and their common prefixes one level down unless the listing is recursive, at most
// limit of them when limit is above zero.
func (f *FS) list(ctx context.Context, prefix string, recursive bool, limit int) ([]item, []item, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	q := url.Values{"restype": {"container"}, "comp": {"list"}, "prefix": {prefix}, "maxresults": {strconv.Itoa(pageSize)}}
	if !recursive {
		q.Set("delimiter", "/")
	}
	if limit > 0 {
		q.Set("maxresults", strconv.Itoa(limit))
	}
	var blobs, prefixes []item
	seen := map[string]bool{}
	for {
		resp, err := f.client.Do(ctx, http.MethodGet, f.container, q, nil, nil)
		if err != nil {
			return nil, nil, err
		}
		var p page
		err = xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&p)
		_ = resp.Body.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("%w: unreadable listing: %w", remotefs.ErrConnectionLost, err)
		}
		blobs, prefixes = append(blobs, p.Blobs.Blob...), append(prefixes, p.Blobs.Prefix...)
		if len(blobs)+len(prefixes) > 100000 || (p.Next != "" && seen[p.Next]) {
			return nil, nil, remotefs.ErrInvalid
		}
		seen[p.Next] = true
		if p.Next == "" || limit > 0 {
			return blobs, prefixes, nil
		}
		q.Set("marker", p.Next)
	}
}

// state is what a name names.
type state int

const (
	none state = iota
	file
	folder
)

// lookup tells what name names: a file, with its size and time, a folder, or nothing. A folder exists when its marker does or
// when any name starts with its prefix.
func (f *FS) lookup(ctx context.Context, op, p, name string) (state, int64, time.Time, error) {
	if name == "" {
		return folder, 0, time.Time{}, nil
	}
	header, err := f.do(ctx, http.MethodHead, name, nil, nil, nil)
	if err == nil {
		size, _ := strconv.ParseInt(header.Get("Content-Length"), 10, 64)
		modified, _ := time.Parse(http.TimeFormat, header.Get("Last-Modified"))
		return file, size, modified.UTC(), nil
	}
	if azure.Code(err) != "BlobNotFound" {
		return none, 0, time.Time{}, azure.Wrap(op, p, err)
	}
	blobs, _, err := f.list(ctx, name+"/", true, 1)
	if err != nil {
		return none, 0, time.Time{}, azure.Wrap(op, p, err)
	}
	if len(blobs) > 0 {
		return folder, 0, time.Time{}, nil
	}
	return none, 0, time.Time{}, nil
}

// needFolder checks that the parent of name is a folder that exists.
func (f *FS) needFolder(ctx context.Context, op, p, name string) error {
	parent := path.Dir(name)
	if parent == "." {
		return nil
	}
	switch kind, _, _, err := f.lookup(ctx, op, p, parent); {
	case err != nil:
		return err
	case kind == file:
		return fmt.Errorf("%s %q: %w: %q is a file", op, p, remotefs.ErrNotDirectory, "/"+parent)
	case kind == none:
		return fmt.Errorf("%s %q: %w: %q does not exist", op, p, remotefs.ErrNotFound, "/"+parent)
	}
	return nil
}

// entry describes the blob or the folder at p.
func entry(p string, dir bool, size int64, modified time.Time) remotefs.Entry {
	e := remotefs.Entry{Name: path.Base(p), Path: p, Size: size, Mode: fileMode, ModTime: modified, ModTimePrecision: remotefs.PrecisionSecond}
	if dir {
		e.Mode, e.Size = dirMode, 0
	}
	return e
}

// List implements remotefs.RemoteFS.
func (f *FS) List(ctx context.Context, dir string) ([]remotefs.Entry, error) {
	name, err := f.begin(ctx, "list", dir)
	if err != nil {
		return nil, err
	}
	switch kind, _, _, err := f.lookup(ctx, "list", dir, name); {
	case err != nil:
		return nil, err
	case kind == file:
		return nil, fmt.Errorf("list %q: %w", dir, remotefs.ErrNotDirectory)
	case kind == none:
		return nil, fmt.Errorf("list %q: %w", dir, remotefs.ErrNotFound)
	}
	prefix := name
	if prefix != "" {
		prefix += "/"
	}
	blobs, prefixes, err := f.list(ctx, prefix, false, 0)
	if err != nil {
		return nil, azure.Wrap("list", dir, err)
	}
	base := "/" + name
	entries := []remotefs.Entry{}
	seen := map[string]bool{}
	for _, b := range blobs {
		child := strings.TrimPrefix(b.Name, prefix)
		if child == "" {
			continue
		}
		seen[child] = true
		modified, _ := time.Parse(http.TimeFormat, b.Properties.LastModified)
		entries = append(entries, entry(path.Join(base, child), false, b.Properties.ContentLength, modified.UTC()))
	}
	for _, p := range prefixes {
		child := strings.TrimSuffix(strings.TrimPrefix(p.Name, prefix), "/")
		if child == "" || seen[child] {
			continue
		}
		seen[child] = true
		entries = append(entries, entry(path.Join(base, child), true, 0, time.Time{}))
	}
	return entries, nil
}

// Stat implements remotefs.RemoteFS.
func (f *FS) Stat(ctx context.Context, p string) (remotefs.Entry, error) {
	name, err := f.begin(ctx, "stat", p)
	if err != nil {
		return remotefs.Entry{}, err
	}
	kind, size, modified, err := f.lookup(ctx, "stat", p, name)
	switch {
	case err != nil:
		return remotefs.Entry{}, err
	case kind == none:
		return remotefs.Entry{}, fmt.Errorf("stat %q: %w", p, remotefs.ErrNotFound)
	case name == "":
		return remotefs.Entry{Name: "/", Path: "/", Mode: dirMode}, nil
	}
	return entry("/"+name, kind == folder, size, modified), nil
}

// OpenRead implements remotefs.RemoteFS.
func (f *FS) OpenRead(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	name, err := f.begin(ctx, "read", p)
	if err != nil {
		return nil, err
	}
	kind, size, _, err := f.lookup(ctx, "read", p, name)
	switch {
	case err != nil:
		return nil, err
	case kind == folder:
		return nil, fmt.Errorf("read %q: %w: it is a folder", p, remotefs.ErrInvalid)
	case kind == none:
		return nil, fmt.Errorf("read %q: %w", p, remotefs.ErrNotFound)
	case offset < 0 || offset > size:
		return nil, fmt.Errorf("read %q at %d: %w: the file has %d bytes", p, offset, remotefs.ErrInvalid, size)
	case offset == size:
		return io.NopCloser(strings.NewReader("")), nil
	}
	header := http.Header{}
	if offset > 0 {
		header.Set("x-ms-range", fmt.Sprintf("bytes=%d-", offset))
	}
	body, err := f.stream(ctx, f.blob(name), header)
	if err != nil {
		return nil, azure.Wrap("read", p, err)
	}
	return &reader{body: body, path: p}, nil
}

// stream sends a GET request for a resource and hands over the body of its answer, which the caller closes.
func (f *FS) stream(ctx context.Context, resource string, header http.Header) (io.ReadCloser, error) {
	resp, err := f.client.Do(ctx, http.MethodGet, resource, nil, header, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// reader streams a blob, turning its failures into the errors of the port.
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

// OpenWrite implements remotefs.RemoteFS. Blobs are written whole, so resuming is refused; creating sends If-None-Match, so a
// blob written meanwhile is not replaced.
func (f *FS) OpenWrite(ctx context.Context, p string, offset int64, mode remotefs.WriteMode) (io.WriteCloser, error) {
	name, err := f.begin(ctx, "write", p)
	if err != nil {
		return nil, err
	}
	switch {
	case name == "":
		return nil, fmt.Errorf("write %q: %w: it is a folder", p, remotefs.ErrInvalid)
	case mode == remotefs.WriteResume:
		return nil, fmt.Errorf("write %q at %d: %w: blobs cannot resume a write", p, offset, remotefs.ErrInvalid)
	case mode != remotefs.WriteCreate && mode != remotefs.WriteOverwrite:
		return nil, fmt.Errorf("write %q: %w: unknown write mode %d", p, remotefs.ErrInvalid, mode)
	}
	if err := f.needFolder(ctx, "write", p, name); err != nil {
		return nil, err
	}
	switch kind, _, _, err := f.lookup(ctx, "write", p, name); {
	case err != nil:
		return nil, err
	case kind == folder:
		return nil, fmt.Errorf("write %q: %w: it is a folder", p, remotefs.ErrInvalid)
	case kind == file && mode == remotefs.WriteCreate:
		return nil, fmt.Errorf("write %q: %w", p, remotefs.ErrExists)
	}
	return &writer{ctx: ctx, fs: f, name: name, path: p, create: mode == remotefs.WriteCreate}, nil
}

// writer stages what is written in blocks; closing it commits them, unless its context ended first.
type writer struct {
	ctx    context.Context
	fs     *FS
	name   string
	path   string
	create bool
	buf    []byte
	blocks []string
	done   bool
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
	for len(w.buf) >= blockSize {
		if err := w.stage(w.buf[:blockSize]); err != nil {
			return 0, err
		}
		w.buf = append(w.buf[:0], w.buf[blockSize:]...)
	}
	return len(b), nil
}

// stage uploads one block of the blob.
func (w *writer) stage(data []byte) error {
	id := base64.StdEncoding.EncodeToString(fmt.Appendf(nil, "block-%06d", len(w.blocks)))
	if _, err := w.fs.do(w.ctx, http.MethodPut, w.name, url.Values{"comp": {"block"}, "blockid": {id}}, nil, data); err != nil {
		return azure.Wrap("write", w.path, err)
	}
	w.blocks = append(w.blocks, id)
	return nil
}

// Close implements io.Closer: it commits the blob, in one request when it fits in one block. When the context of the write ended,
// as when the data being uploaded failed half way, nothing is committed and the blob keeps what it had.
func (w *writer) Close() error {
	if w.done {
		return nil
	}
	w.done = true
	if err := w.ctx.Err(); err != nil {
		return err
	}
	header := http.Header{}
	if w.create {
		header.Set("If-None-Match", "*")
	}
	var err error
	if len(w.blocks) == 0 {
		header.Set("x-ms-blob-type", "BlockBlob")
		_, err = w.fs.do(w.ctx, http.MethodPut, w.name, nil, header, w.buf)
	} else if err = w.stage(w.buf); err == nil {
		var list bytes.Buffer
		list.WriteString(`<?xml version="1.0" encoding="utf-8"?><BlockList>`)
		for _, id := range w.blocks {
			list.WriteString("<Latest>" + id + "</Latest>")
		}
		list.WriteString("</BlockList>")
		_, err = w.fs.do(w.ctx, http.MethodPut, w.name, url.Values{"comp": {"blocklist"}}, header, list.Bytes())
	}
	if err != nil {
		return azure.Wrap("write", w.path, err)
	}
	return nil
}

// Mkdir implements remotefs.RemoteFS: it writes the marker of the folder.
func (f *FS) Mkdir(ctx context.Context, p string) error {
	name, err := f.begin(ctx, "mkdir", p)
	if err != nil {
		return err
	}
	switch kind, _, _, err := f.lookup(ctx, "mkdir", p, name); {
	case err != nil:
		return err
	case kind != none:
		return fmt.Errorf("mkdir %q: %w", p, remotefs.ErrExists)
	}
	if err := f.needFolder(ctx, "mkdir", p, name); err != nil {
		return err
	}
	if _, err := f.do(ctx, http.MethodPut, name+"/", nil, http.Header{"x-ms-blob-type": {"BlockBlob"}}, nil); err != nil {
		return azure.Wrap("mkdir", p, err)
	}
	return nil
}

// Remove implements remotefs.RemoteFS.
func (f *FS) Remove(ctx context.Context, p string) error {
	name, err := f.begin(ctx, "remove", p)
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("remove %q: %w: the root cannot be removed", p, remotefs.ErrPermission)
	}
	kind, _, _, err := f.lookup(ctx, "remove", p, name)
	switch {
	case err != nil:
		return err
	case kind == none:
		return fmt.Errorf("remove %q: %w", p, remotefs.ErrNotFound)
	case kind == folder:
		blobs, _, err := f.list(ctx, name+"/", true, 2)
		if err != nil {
			return azure.Wrap("remove", p, err)
		}
		for _, b := range blobs {
			if b.Name != name+"/" {
				return fmt.Errorf("remove %q: %w", p, remotefs.ErrNotEmpty)
			}
		}
		name += "/"
	}
	if _, err := f.do(ctx, http.MethodDelete, name, nil, nil, nil); err != nil {
		return azure.Wrap("remove", p, err)
	}
	return nil
}

// tree returns the names of a blob at name, if there is one, and of every blob under it, with their sizes.
func (f *FS) tree(ctx context.Context, op, p, name string) ([]item, error) {
	var names []item
	switch kind, size, _, err := f.lookup(ctx, op, p, name); {
	case err != nil:
		return nil, err
	case kind == file:
		one := item{Name: name}
		one.Properties.ContentLength = size
		names = append(names, one)
	}
	blobs, _, err := f.list(ctx, name+"/", true, 0)
	if err != nil {
		return nil, azure.Wrap(op, p, err)
	}
	return append(names, blobs...), nil
}

// remove deletes blobs one by one.
func (f *FS) remove(ctx context.Context, op, p string, names []item) error {
	for _, b := range names {
		if _, err := f.do(ctx, http.MethodDelete, b.Name, nil, nil, nil); err != nil && azure.Code(err) != "BlobNotFound" {
			return azure.Wrap(op, p, err)
		}
	}
	return nil
}

// RemoveAll implements remotefs.RemoteFS.
func (f *FS) RemoveAll(ctx context.Context, p string) error {
	name, err := f.begin(ctx, "remove", p)
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("remove %q: %w: the root cannot be removed", p, remotefs.ErrPermission)
	}
	names, err := f.tree(ctx, "remove", p, name)
	if err != nil {
		return err
	}
	return f.remove(ctx, "remove", p, names)
}

// copy copies a blob on the server and waits while the service reports the copy as pending. A folder marker is written anew.
func (f *FS) copy(ctx context.Context, from, to string) error {
	if strings.HasSuffix(from, "/") {
		_, err := f.do(ctx, http.MethodPut, to, nil, http.Header{"x-ms-blob-type": {"BlockBlob"}}, nil)
		return err
	}
	header, err := f.do(ctx, http.MethodPut, to, nil, http.Header{"x-ms-copy-source": {f.client.URL(f.blob(from), nil)}}, nil)
	for err == nil && header.Get("x-ms-copy-status") == "pending" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(copyWait):
		}
		header, err = f.do(ctx, http.MethodHead, to, nil, nil, nil)
	}
	if err == nil && header.Get("x-ms-copy-status") != "success" {
		err = fmt.Errorf("%w: the copy ended %s", remotefs.ErrConnectionLost, header.Get("x-ms-copy-status"))
	}
	return err
}

// Rename implements remotefs.RemoteFS: blobs are copied on the server and the originals deleted, so renaming a folder is not
// atomic.
func (f *FS) Rename(ctx context.Context, from, to string) error {
	src, err := f.begin(ctx, "rename", from)
	if err != nil {
		return err
	}
	dst, err := f.begin(ctx, "rename", to)
	if err != nil {
		return err
	}
	if src == "" || strings.HasPrefix(dst+"/", src+"/") {
		return fmt.Errorf("rename %q to %q: %w: a folder cannot move into itself", from, to, remotefs.ErrInvalid)
	}
	names, err := f.tree(ctx, "rename", from, src)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("rename %q: %w", from, remotefs.ErrNotFound)
	}
	switch kind, _, _, err := f.lookup(ctx, "rename", to, dst); {
	case err != nil:
		return err
	case kind != none:
		return fmt.Errorf("rename %q to %q: %w", from, to, remotefs.ErrExists)
	}
	if err := f.needFolder(ctx, "rename", to, dst); err != nil {
		return err
	}
	for _, b := range names {
		if err := f.copy(ctx, b.Name, dst+strings.TrimPrefix(b.Name, src)); err != nil {
			return azure.Wrap("rename", from, err)
		}
	}
	return f.remove(ctx, "rename", from, names)
}

// Chmod implements remotefs.RemoteFS; blobs have no modes.
func (f *FS) Chmod(ctx context.Context, p string, _ fs.FileMode) error {
	if _, err := f.begin(ctx, "chmod", p); err != nil {
		return err
	}
	return fmt.Errorf("chmod %q: %w: blobs have no permissions", p, remotefs.ErrInvalid)
}

// Chtimes implements remotefs.RemoteFS; the service sets the time of a blob when it is written.
func (f *FS) Chtimes(ctx context.Context, p string, _ time.Time) error {
	if _, err := f.begin(ctx, "chtimes", p); err != nil {
		return err
	}
	return fmt.Errorf("chtimes %q: %w: Azure sets the time of a blob itself", p, remotefs.ErrInvalid)
}

// Capabilities implements remotefs.RemoteFS: blobs are written whole and have no modes nor times that can be set.
func (f *FS) Capabilities() remotefs.Capabilities { return remotefs.Capabilities{} }

// Ping implements remotefs.RemoteFS: it checks that the container can still be reached.
func (f *FS) Ping(ctx context.Context) error {
	if _, err := f.begin(ctx, "ping", "/"); err != nil {
		return err
	}
	if _, err := f.do(ctx, http.MethodGet, "", url.Values{"restype": {"container"}}, nil, nil); err != nil {
		return azure.Wrap("container", f.container, err)
	}
	return nil
}

// Close implements remotefs.RemoteFS. Later operations, and uploads still open, fail with [remotefs.ErrConnectionLost].
func (f *FS) Close() error {
	f.closed.Store(true)
	f.idle()
	return nil
}
