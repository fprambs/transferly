// Package s3fs implements [remotefs.RemoteFS] on a bucket of S3 or a compatible service (ADR-0023). Paths are keys; folders
// are emulated with the prefixes keys share and with empty marker objects whose key ends in a slash, so an empty folder
// survives and buckets filled by other tools browse normally.
package s3fs

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"net"
	"net/http"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/s3utils"

	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// Config tells how to reach a bucket.
type Config struct {
	// Endpoint is the host:port of the service, such as s3.amazonaws.com:443.
	Endpoint string
	// Secure selects HTTPS; false speaks plain HTTP.
	Secure bool
	// RootCAs are the certificate authorities HTTPS is verified against; nil means those of the system.
	RootCAs *x509.CertPool
	// Region is the region of the bucket; empty lets the service tell it.
	Region string
	// Bucket is the bucket the file system shows.
	Bucket string
	// AccessKeyID and SecretAccessKey sign the requests; both empty means anonymous requests, for public buckets.
	AccessKeyID, SecretAccessKey string
	// DialContext opens every TCP connection; nil uses a plain dialer. Give the dialer of the network guard here.
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
	// Timeout bounds the TLS handshake and the wait for the headers of each answer. Zero means no limit.
	Timeout time.Duration
}

// partSize is the size of the parts uploads are streamed in. It bounds the memory of each upload and, with the limit of
// 10,000 parts, the size of an object to about 156 GiB.
const partSize = 16 << 20

// copyLimit is the largest object S3 copies in one request; larger ones are copied in parts.
var copyLimit int64 = 5 << 30

// maxRetries is how many times a failed request is tried; the client of the service waits between tries.
const maxRetries = 3

// Modes of the entries: S3 has none, so files and folders read as the usual defaults.
const (
	dirMode  = fs.ModeDir | 0o755
	fileMode = 0o644
)

// FS is a bucket seen as a file system. It is safe for concurrent use.
type FS struct {
	client    *minio.Client
	bucket    string
	transport *http.Transport
	closed    atomic.Bool
}

// Dial checks that the bucket exists and can be reached with the credentials of cfg, and returns its file system.
func Dial(ctx context.Context, cfg Config) (*FS, error) {
	creds := credentials.NewStatic(cfg.AccessKeyID, cfg.SecretAccessKey, "", credentials.SignatureV4)
	if cfg.AccessKeyID == "" && cfg.SecretAccessKey == "" {
		creds = credentials.NewStatic("", "", "", credentials.SignatureAnonymous)
	}
	transport := &http.Transport{
		DialContext:           cfg.DialContext,
		TLSClientConfig:       &tls.Config{RootCAs: cfg.RootCAs, MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   cfg.Timeout,
		ResponseHeaderTimeout: cfg.Timeout,
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds: creds, Secure: cfg.Secure, Region: cfg.Region, Transport: transport,
		BucketLookup: minio.BucketLookupAuto, MaxRetries: maxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 endpoint %q: %w: %w", cfg.Endpoint, remotefs.ErrInvalid, err)
	}
	f := &FS{client: client, bucket: cfg.Bucket, transport: transport}
	if err := f.Ping(ctx); err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	return f, nil
}

// begin starts an operation on p: it fails when ctx is done or the file system is closed, and returns the key of p, empty
// for the root. Keys S3 does not accept, such as names that are not UTF-8, are refused here, so later calls never are.
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
	key := strings.TrimPrefix(path.Clean(p), "/")
	if key == "" {
		return "", nil
	}
	if err := s3utils.CheckValidObjectName(key); err != nil {
		return "", fmt.Errorf("%s %q: %w: %w", op, p, remotefs.ErrInvalid, err)
	}
	return key, nil
}

// wrap turns a failure of the service into the errors of the port, keeping cancellations as they are and destinations the
// network guard refused as [netguard.ErrBlocked], which trying again cannot fix.
func wrap(op, p string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, netguard.ErrBlocked) {
		return fmt.Errorf("%s %q: %w", op, p, err)
	}
	var response minio.ErrorResponse
	kind := remotefs.ErrConnectionLost
	if errors.As(err, &response) {
		switch {
		case response.Code == "NoSuchKey" || response.Code == "NoSuchBucket":
			kind = remotefs.ErrNotFound
		case response.Code == "InvalidAccessKeyId" || response.Code == "SignatureDoesNotMatch":
			kind = remotefs.ErrAuth
		case response.Code == "AccessDenied":
			kind = remotefs.ErrPermission
		case response.StatusCode > 0 && response.StatusCode < http.StatusInternalServerError:
			kind = remotefs.ErrInvalid
		}
	}
	return fmt.Errorf("%s %q: %w: %w", op, p, kind, err)
}

// missing reports whether a failure means the key does not exist.
func missing(err error) bool {
	var response minio.ErrorResponse
	return errors.As(err, &response) && response.Code == "NoSuchKey"
}

// state is what a key names.
type state int

const (
	none state = iota
	file
	folder
)

// objects lists the keys under prefix: all of them when recursive, otherwise one level, with the deeper keys folded into
// their common prefixes. The client stops quietly when ctx is done, so that ends the listing with the error of ctx.
func (f *FS) objects(ctx context.Context, prefix string, recursive bool) iter.Seq[minio.ObjectInfo] {
	owner := false
	listed := f.client.ListObjectsIter(ctx, f.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: recursive, FetchOwner: &owner})
	return func(yield func(minio.ObjectInfo) bool) {
		for object := range listed {
			if !yield(object) {
				return
			}
		}
		if err := ctx.Err(); err != nil {
			yield(minio.ObjectInfo{Err: err})
		}
	}
}

// lookup tells what key names: a file, with its object, a folder, with its marker when it has one, or nothing. A folder
// exists when its marker does or when any key starts with its prefix.
func (f *FS) lookup(ctx context.Context, op, p, key string) (state, minio.ObjectInfo, error) {
	if key == "" {
		return folder, minio.ObjectInfo{}, nil
	}
	info, err := f.client.StatObject(ctx, f.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return file, info, nil
	}
	if !missing(err) {
		return none, info, wrap(op, p, err)
	}
	for object := range f.objects(ctx, key+"/", true) {
		if object.Err != nil {
			return none, object, wrap(op, p, object.Err)
		}
		return folder, object, nil
	}
	return none, minio.ObjectInfo{}, nil
}

// needFolder checks that the parent of key is a folder that exists.
func (f *FS) needFolder(ctx context.Context, op, p, key string) error {
	parent := path.Dir(key)
	if parent == "." {
		return nil
	}
	switch kind, _, err := f.lookup(ctx, op, p, parent); {
	case err != nil:
		return err
	case kind == file:
		return fmt.Errorf("%s %q: %w: %q is a file", op, p, remotefs.ErrNotDirectory, "/"+parent)
	case kind == none:
		return fmt.Errorf("%s %q: %w: %q does not exist", op, p, remotefs.ErrNotFound, "/"+parent)
	}
	return nil
}

// fileEntry describes the object of a file at p.
func fileEntry(p string, info minio.ObjectInfo) remotefs.Entry {
	return remotefs.Entry{Name: path.Base(p), Path: p, Size: info.Size, Mode: fileMode, ModTime: info.LastModified.UTC(), ModTimePrecision: remotefs.PrecisionSecond}
}

// folderEntry describes the folder at p; its time is the one of its marker, or none for a folder that is only a prefix.
func folderEntry(p string, marker minio.ObjectInfo) remotefs.Entry {
	return remotefs.Entry{Name: path.Base(p), Path: p, Mode: dirMode, ModTime: marker.LastModified.UTC(), ModTimePrecision: remotefs.PrecisionSecond}
}

// List implements remotefs.RemoteFS.
func (f *FS) List(ctx context.Context, dir string) ([]remotefs.Entry, error) {
	key, err := f.begin(ctx, "list", dir)
	if err != nil {
		return nil, err
	}
	switch kind, _, err := f.lookup(ctx, "list", dir, key); {
	case err != nil:
		return nil, err
	case kind == file:
		return nil, fmt.Errorf("list %q: %w", dir, remotefs.ErrNotDirectory)
	case kind == none:
		return nil, fmt.Errorf("list %q: %w", dir, remotefs.ErrNotFound)
	}
	prefix := key
	if prefix != "" {
		prefix += "/"
	}
	base := "/" + key
	entries := []remotefs.Entry{}
	seen := map[string]bool{}
	for object := range f.objects(ctx, prefix, false) {
		if object.Err != nil {
			return nil, wrap("list", dir, object.Err)
		}
		name, isFolder := strings.CutSuffix(strings.TrimPrefix(object.Key, prefix), "/")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if isFolder {
			entries = append(entries, folderEntry(path.Join(base, name), object))
		} else {
			entries = append(entries, fileEntry(path.Join(base, name), object))
		}
	}
	return entries, nil
}

// Stat implements remotefs.RemoteFS.
func (f *FS) Stat(ctx context.Context, p string) (remotefs.Entry, error) {
	key, err := f.begin(ctx, "stat", p)
	if err != nil {
		return remotefs.Entry{}, err
	}
	kind, info, err := f.lookup(ctx, "stat", p, key)
	switch {
	case err != nil:
		return remotefs.Entry{}, err
	case kind == file:
		return fileEntry("/"+key, info), nil
	case kind == folder && key == "":
		return remotefs.Entry{Name: "/", Path: "/", Mode: dirMode}, nil
	case kind == folder && info.Key == key+"/":
		return folderEntry("/"+key, info), nil
	case kind == folder:
		return folderEntry("/"+key, minio.ObjectInfo{}), nil
	}
	return remotefs.Entry{}, fmt.Errorf("stat %q: %w", p, remotefs.ErrNotFound)
}

// OpenRead implements remotefs.RemoteFS.
func (f *FS) OpenRead(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	key, err := f.begin(ctx, "read", p)
	if err != nil {
		return nil, err
	}
	kind, info, err := f.lookup(ctx, "read", p, key)
	switch {
	case err != nil:
		return nil, err
	case kind == folder:
		return nil, fmt.Errorf("read %q: %w: it is a folder", p, remotefs.ErrInvalid)
	case kind == none:
		return nil, fmt.Errorf("read %q: %w", p, remotefs.ErrNotFound)
	case offset < 0 || offset > info.Size:
		return nil, fmt.Errorf("read %q at %d: %w: the file has %d bytes", p, offset, remotefs.ErrInvalid, info.Size)
	case offset == info.Size:
		return io.NopCloser(strings.NewReader("")), nil
	}
	return &reader{object: f.open(ctx, key, offset), path: p}, nil
}

// open starts streaming the object at key from offset. The client only refuses names there, which [FS.begin] already
// refused, so every failure surfaces when the stream is read.
func (f *FS) open(ctx context.Context, key string, offset int64) *minio.Object {
	opts := minio.GetObjectOptions{}
	if offset > 0 {
		_ = opts.SetRange(offset, 0)
	}
	object, _ := f.client.GetObject(ctx, f.bucket, key, opts)
	return object
}

// reader streams an object, turning its failures into the errors of the port.
type reader struct {
	object *minio.Object
	path   string
}

// Read implements io.Reader.
func (r *reader) Read(b []byte) (int, error) {
	n, err := r.object.Read(b)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, wrap("read", r.path, err)
	}
	return n, err
}

// Close implements io.Closer.
func (r *reader) Close() error { return r.object.Close() }

// OpenWrite implements remotefs.RemoteFS. Objects are written whole, so resuming is refused.
func (f *FS) OpenWrite(ctx context.Context, p string, offset int64, mode remotefs.WriteMode) (io.WriteCloser, error) {
	key, err := f.begin(ctx, "write", p)
	if err != nil {
		return nil, err
	}
	switch {
	case key == "":
		return nil, fmt.Errorf("write %q: %w: it is a folder", p, remotefs.ErrInvalid)
	case mode == remotefs.WriteResume:
		return nil, fmt.Errorf("write %q at %d: %w: S3 cannot resume a write", p, offset, remotefs.ErrInvalid)
	case mode != remotefs.WriteCreate && mode != remotefs.WriteOverwrite:
		return nil, fmt.Errorf("write %q: %w: unknown write mode %d", p, remotefs.ErrInvalid, mode)
	}
	if err := f.needFolder(ctx, "write", p, key); err != nil {
		return nil, err
	}
	switch kind, _, err := f.lookup(ctx, "write", p, key); {
	case err != nil:
		return nil, err
	case kind == folder:
		return nil, fmt.Errorf("write %q: %w: it is a folder", p, remotefs.ErrInvalid)
	case kind == file && mode == remotefs.WriteCreate:
		return nil, fmt.Errorf("write %q: %w", p, remotefs.ErrExists)
	}
	pr, pw := io.Pipe()
	w := &writer{ctx: ctx, fs: f, pipe: pw, path: p, done: make(chan error, 1)}
	go func() {
		_, err := f.client.PutObject(ctx, f.bucket, key, pr, -1, minio.PutObjectOptions{PartSize: partSize})
		_ = pr.CloseWithError(err)
		w.done <- err
	}()
	return w, nil
}

// writer streams what is written into an upload, which completes when it is closed, unless its context ended first.
type writer struct {
	ctx  context.Context
	fs   *FS
	pipe *io.PipeWriter
	path string
	done chan error
}

// Write implements io.Writer.
func (w *writer) Write(b []byte) (int, error) {
	if w.fs.closed.Load() {
		err := fmt.Errorf("write %q: %w", w.path, remotefs.ErrConnectionLost)
		_ = w.pipe.CloseWithError(err)
		return 0, err
	}
	n, err := w.pipe.Write(b)
	if err != nil {
		return n, wrap("write", w.path, err)
	}
	return n, nil
}

// Close implements io.Closer: it ends the upload and waits for the service to store the object. When the context of the
// write ended, as when the data being uploaded failed half way, the upload is abandoned and nothing is stored.
func (w *writer) Close() error {
	_ = w.pipe.CloseWithError(w.ctx.Err())
	if err := <-w.done; err != nil {
		return wrap("write", w.path, err)
	}
	return nil
}

// Mkdir implements remotefs.RemoteFS: it writes the marker of the folder.
func (f *FS) Mkdir(ctx context.Context, p string) error {
	key, err := f.begin(ctx, "mkdir", p)
	if err != nil {
		return err
	}
	switch kind, _, err := f.lookup(ctx, "mkdir", p, key); {
	case err != nil:
		return err
	case kind != none:
		return fmt.Errorf("mkdir %q: %w", p, remotefs.ErrExists)
	}
	if err := f.needFolder(ctx, "mkdir", p, key); err != nil {
		return err
	}
	if _, err := f.client.PutObject(ctx, f.bucket, key+"/", bytes.NewReader(nil), 0, minio.PutObjectOptions{}); err != nil {
		return wrap("mkdir", p, err)
	}
	return nil
}

// Remove implements remotefs.RemoteFS.
func (f *FS) Remove(ctx context.Context, p string) error {
	key, err := f.begin(ctx, "remove", p)
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("remove %q: %w: the root cannot be removed", p, remotefs.ErrPermission)
	}
	kind, _, err := f.lookup(ctx, "remove", p, key)
	switch {
	case err != nil:
		return err
	case kind == none:
		return fmt.Errorf("remove %q: %w", p, remotefs.ErrNotFound)
	case kind == folder:
		for object := range f.objects(ctx, key+"/", true) {
			if object.Err != nil {
				return wrap("remove", p, object.Err)
			}
			if object.Key != key+"/" {
				return fmt.Errorf("remove %q: %w", p, remotefs.ErrNotEmpty)
			}
		}
		key += "/"
	}
	if err := f.client.RemoveObject(ctx, f.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return wrap("remove", p, err)
	}
	return nil
}

// tree returns the key of a file at key, if there is one, and every key under it.
func (f *FS) tree(ctx context.Context, op, p, key string) ([]minio.ObjectInfo, error) {
	var keys []minio.ObjectInfo
	switch kind, info, err := f.lookup(ctx, op, p, key); {
	case err != nil:
		return nil, err
	case kind == file:
		keys = append(keys, info)
	}
	for object := range f.objects(ctx, key+"/", true) {
		if object.Err != nil {
			return nil, wrap(op, p, object.Err)
		}
		keys = append(keys, object)
	}
	return keys, nil
}

// remove deletes keys in batches.
func (f *FS) remove(ctx context.Context, op, p string, keys []minio.ObjectInfo) error {
	queue := make(chan minio.ObjectInfo, len(keys))
	for _, k := range keys {
		queue <- minio.ObjectInfo{Key: k.Key}
	}
	close(queue)
	var first error
	for failure := range f.client.RemoveObjects(ctx, f.bucket, queue, minio.RemoveObjectsOptions{}) {
		if first == nil {
			first = wrap(op, p, failure.Err)
		}
	}
	if first != nil {
		return first
	}
	return ctx.Err()
}

// RemoveAll implements remotefs.RemoteFS.
func (f *FS) RemoveAll(ctx context.Context, p string) error {
	key, err := f.begin(ctx, "remove", p)
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("remove %q: %w: the root cannot be removed", p, remotefs.ErrPermission)
	}
	keys, err := f.tree(ctx, "remove", p, key)
	if err != nil {
		return err
	}
	return f.remove(ctx, "remove", p, keys)
}

// copy copies an object on the server, in parts when it is larger than one request can copy. A folder marker is written
// anew instead, since some services keep markers as directories that cannot be copied.
func (f *FS) copy(ctx context.Context, from, to string, size int64) error {
	dst := minio.CopyDestOptions{Bucket: f.bucket, Object: to}
	src := minio.CopySrcOptions{Bucket: f.bucket, Object: from}
	var err error
	switch {
	case strings.HasSuffix(from, "/"):
		_, err = f.client.PutObject(ctx, f.bucket, to, bytes.NewReader(nil), 0, minio.PutObjectOptions{})
	case size > copyLimit:
		_, err = f.client.ComposeObject(ctx, dst, src)
	default:
		_, err = f.client.CopyObject(ctx, dst, src)
	}
	return err
}

// Rename implements remotefs.RemoteFS: objects are copied on the server and the originals deleted, so renaming a folder
// is not atomic.
func (f *FS) Rename(ctx context.Context, from, to string) error {
	src, err := f.begin(ctx, "rename", from)
	if err != nil {
		return err
	}
	dst, err := f.begin(ctx, "rename", to)
	if err != nil {
		return err
	}
	if src == "" || dst == src || strings.HasPrefix(dst+"/", src+"/") {
		return fmt.Errorf("rename %q to %q: %w: a folder cannot move into itself", from, to, remotefs.ErrInvalid)
	}
	keys, err := f.tree(ctx, "rename", from, src)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return fmt.Errorf("rename %q: %w", from, remotefs.ErrNotFound)
	}
	switch kind, _, err := f.lookup(ctx, "rename", to, dst); {
	case err != nil:
		return err
	case kind != none:
		return fmt.Errorf("rename %q to %q: %w", from, to, remotefs.ErrExists)
	}
	if err := f.needFolder(ctx, "rename", to, dst); err != nil {
		return err
	}
	for _, k := range keys {
		if err := f.copy(ctx, k.Key, dst+strings.TrimPrefix(k.Key, src), k.Size); err != nil {
			return wrap("rename", from, err)
		}
	}
	return f.remove(ctx, "rename", from, keys)
}

// Chmod implements remotefs.RemoteFS; objects have no modes.
func (f *FS) Chmod(ctx context.Context, p string, _ fs.FileMode) error {
	if _, err := f.begin(ctx, "chmod", p); err != nil {
		return err
	}
	return fmt.Errorf("chmod %q: %w: S3 objects have no permissions", p, remotefs.ErrInvalid)
}

// Chtimes implements remotefs.RemoteFS; the service sets the time of an object when it is written.
func (f *FS) Chtimes(ctx context.Context, p string, _ time.Time) error {
	if _, err := f.begin(ctx, "chtimes", p); err != nil {
		return err
	}
	return fmt.Errorf("chtimes %q: %w: S3 sets the time of an object itself", p, remotefs.ErrInvalid)
}

// Capabilities implements remotefs.RemoteFS: objects are written whole and have no modes nor times that can be set.
func (f *FS) Capabilities() remotefs.Capabilities { return remotefs.Capabilities{} }

// Ping implements remotefs.RemoteFS: it checks that the bucket can still be reached.
func (f *FS) Ping(ctx context.Context) error {
	if _, err := f.begin(ctx, "ping", "/"); err != nil {
		return err
	}
	found, err := f.client.BucketExists(ctx, f.bucket)
	if err != nil {
		return wrap("bucket", f.bucket, err)
	}
	if !found {
		return fmt.Errorf("bucket %q: %w", f.bucket, remotefs.ErrNotFound)
	}
	return nil
}

// Close implements remotefs.RemoteFS. Later operations, and writes still open, fail with [remotefs.ErrConnectionLost].
func (f *FS) Close() error {
	f.closed.Store(true)
	f.transport.CloseIdleConnections()
	return nil
}
