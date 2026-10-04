// Package davfs implements [remotefs.RemoteFS] on a WebDAV server (RFC 4918, ADR-0026): listings with PROPFIND, reads by
// ranges, uploads streamed in one PUT, real folders with MKCOL and renames with MOVE that never replace what is there.
package davfs

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
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

	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// CertificateKeyType is the key type under which the fingerprint of a certificate the authorities do not accept is offered to
// the user, the same as for FTPS certificates.
const CertificateKeyType = "tls-certificate"

// Config tells how to reach the root of a site on a WebDAV server.
type Config struct {
	// Endpoint is the URL of the root of the site, such as https://cloud.example.com/remote.php/dav/files/alice/.
	Endpoint string
	// Username and Password are sent with HTTP Basic authentication; an empty username sends none, for public shares.
	Username, Password string
	// Client sends the requests. Redirects are never followed: the adapter handles the ones that add a trailing slash.
	Client *http.Client
}

// Modes of the entries: WebDAV has no modes, so files and folders read as the usual defaults.
const (
	dirMode  = fs.ModeDir | 0o755
	fileMode = 0o644
)

// propfind is the body of the listings: only the properties the adapter reads.
const propfind = `<?xml version="1.0" encoding="utf-8"?><D:propfind xmlns:D="DAV:"><D:prop><D:resourcetype/><D:getcontentlength/><D:getlastmodified/></D:prop></D:propfind>`

// FS is the root of a site seen as a file system. It is safe for concurrent use.
type FS struct {
	client             *http.Client
	base               *url.URL
	username, password string
	life               context.Context
	end                context.CancelFunc
	closed             atomic.Bool
}

// Dial checks that the root of cfg is a folder that can be reached with its credentials, and returns its file system.
func Dial(ctx context.Context, cfg Config) (*FS, error) {
	base, err := url.Parse(cfg.Endpoint)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("%w: the endpoint %q is not an http or https URL", remotefs.ErrInvalid, cfg.Endpoint)
	}
	base.Path = strings.TrimSuffix(path.Clean("/"+base.Path), "/") + "/"
	base.RawPath, base.RawQuery, base.Fragment = "", "", ""
	client := *cfg.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	life, end := context.WithCancel(context.Background())
	f := &FS{client: &client, base: base, username: cfg.Username, password: cfg.Password, life: life, end: end}
	if err := f.Ping(ctx); err != nil {
		end()
		return nil, err
	}
	return f, nil
}

// TLSConfig returns the TLS configuration of a site: a certificate the authorities in roots accept, or the system ones when
// roots is nil, is trusted; any other is judged by verify from its fingerprint under [CertificateKeyType], as an FTPS
// certificate is, so self-signed servers work once the user trusts them.
func TLSConfig(roots *x509.CertPool, verify func(keyType, fingerprint string) error) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		//nolint:gosec // VerifyConnection verifies the chain against the authorities, and pins the certificate when they refuse it.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("the server presented no certificate")
			}
			intermediates := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				intermediates.AddCert(c)
			}
			leaf := cs.PeerCertificates[0]
			if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: cs.ServerName, Intermediates: intermediates}); err == nil {
				return nil
			}
			return verify(CertificateKeyType, Fingerprint(leaf.Raw))
		},
	}
}

// Fingerprint renders the SHA-256 fingerprint of a DER certificate in the notation Transferly uses for host keys.
func Fingerprint(der []byte) string {
	digest := sha256.Sum256(der)
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(digest[:]), "=")
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

// target is the URL of a path of the site, ending in a slash for folders.
func (f *FS) target(p string, folder bool) string {
	u := *f.base
	u.Path = f.base.Path + strings.TrimPrefix(p, "/")
	if folder && !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return u.String()
}

// StatusError is an answer of the server that is not a success.
type StatusError struct {
	// Method is the method of the request.
	Method string
	// Status is the HTTP status of the answer.
	Status int
}

// Error implements error.
func (e *StatusError) Error() string { return fmt.Sprintf("%s answered %d", e.Method, e.Status) }

// statusOf returns the status a failure carries, or zero when the server did not answer.
func statusOf(err error) int {
	var answer *StatusError
	if errors.As(err, &answer) {
		return answer.Status
	}
	return 0
}

// send sends a request to the server, authenticated when the site has a username, and returns its answer when it succeeded;
// an answer of 300 or more is a [*StatusError]. ctx bounds the request, and so does the life of the file system.
func (f *FS) send(ctx context.Context, method, target string, header http.Header, body io.Reader) (*http.Response, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(f.life, cancel)
	if body == nil {
		body = http.NoBody
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		stop()
		cancel()
		return nil, fmt.Errorf("%w: %w", remotefs.ErrInvalid, err)
	}
	for name, values := range header {
		req.Header[http.CanonicalHeaderKey(name)] = values
	}
	if f.username != "" {
		req.SetBasicAuth(f.username, f.password)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		stop()
		cancel()
		return nil, err
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		stop()
		cancel()
		return nil, &StatusError{Method: method, Status: resp.StatusCode}
	}
	resp.Body = &release{ReadCloser: resp.Body, done: func() { stop(); cancel() }}
	return resp, nil
}

// release is the body of an answer that frees the context of its request when it is closed.
type release struct {
	io.ReadCloser
	done func()
}

// Close implements io.Closer.
func (r *release) Close() error {
	err := r.ReadCloser.Close()
	r.done()
	return err
}

// do sends a request, with body when it is not nil, and closes its answer.
func (f *FS) do(ctx context.Context, method, target string, header http.Header, body io.Reader) error {
	resp, err := f.send(ctx, method, target, header, body)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

// Wrap turns a failure into the errors of the port. Cancellations, invalid arguments and server keys that are not trusted stay
// as they are, and destinations the network guard refused stay [netguard.ErrBlocked], which trying again cannot fix.
func Wrap(op, p string, err error) error {
	var key *remotefs.HostKeyError
	switch {
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, remotefs.ErrInvalid):
		return err
	case errors.As(err, &key):
		return fmt.Errorf("%s %q: %w", op, p, key)
	case errors.Is(err, netguard.ErrBlocked):
		return fmt.Errorf("%s %q: %w", op, p, err)
	}
	kind := remotefs.ErrConnectionLost
	switch status := statusOf(err); {
	case status == 0:
	case status == http.StatusUnauthorized:
		kind = remotefs.ErrAuth
	case status == http.StatusForbidden || status == http.StatusLocked:
		kind = remotefs.ErrPermission
	case status == http.StatusNotFound || status == http.StatusConflict || status == http.StatusGone:
		kind = remotefs.ErrNotFound
	case status == http.StatusPreconditionFailed:
		kind = remotefs.ErrExists
	case status == http.StatusInsufficientStorage:
		kind = remotefs.ErrQuota
	case status < http.StatusInternalServerError && status != http.StatusTooManyRequests:
		kind = remotefs.ErrInvalid
	}
	return fmt.Errorf("%s %q: %w: %w", op, p, kind, err)
}

// item is a resource of a multistatus answer, with the properties the server found for it.
type item struct {
	Href     string `xml:"href"`
	Propstat []struct {
		Status string `xml:"status"`
		Prop   struct {
			ResourceType struct {
				Collection *struct{} `xml:"collection"`
			} `xml:"resourcetype"`
			Length   string `xml:"getcontentlength"`
			Modified string `xml:"getlastmodified"`
		} `xml:"prop"`
	} `xml:"propstat"`
}

// resource is what a multistatus says of one path.
type resource struct {
	path   string
	folder bool
	size   int64
	time   time.Time
}

// find sends a PROPFIND of the given depth to target and returns the resources of its answer below the root of the site, by
// their paths. A redirect that only adds a trailing slash, which some servers answer for folders, is followed once.
func (f *FS) find(ctx context.Context, target, depth string) ([]resource, error) {
	header := http.Header{"Depth": {depth}, "Content-Type": {`application/xml; charset="utf-8"`}}
	resp, err := f.send(ctx, "PROPFIND", target, header, strings.NewReader(propfind))
	if status := statusOf(err); status >= 300 && status < 400 && !strings.HasSuffix(target, "/") {
		resp, err = f.send(ctx, "PROPFIND", target+"/", header, strings.NewReader(propfind))
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var answer struct {
		Responses []item `xml:"response"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return nil, fmt.Errorf("%w: unreadable listing: %w", remotefs.ErrConnectionLost, err)
	}
	out := make([]resource, 0, len(answer.Responses))
	for _, i := range answer.Responses {
		if r, ok := f.resourceOf(i); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// resourceOf reads an item of a multistatus: its path relative to the root of the site, and the properties found with
// success. Items outside the root, or without found properties, are left out.
func (f *FS) resourceOf(i item) (resource, bool) {
	u, err := url.Parse(strings.TrimSpace(i.Href))
	if err != nil {
		return resource{}, false
	}
	decoded := path.Clean("/" + u.Path)
	root := strings.TrimSuffix(f.base.Path, "/")
	if decoded != root && !strings.HasPrefix(decoded, root+"/") {
		return resource{}, false
	}
	r := resource{path: path.Clean("/" + strings.TrimPrefix(decoded, root))}
	found := false
	for _, ps := range i.Propstat {
		if !strings.Contains(ps.Status, " 200") {
			continue
		}
		found = true
		r.folder = r.folder || ps.Prop.ResourceType.Collection != nil
		if size, err := strconv.ParseInt(strings.TrimSpace(ps.Prop.Length), 10, 64); err == nil {
			r.size = size
		}
		if t, err := http.ParseTime(strings.TrimSpace(ps.Prop.Modified)); err == nil {
			r.time = t.UTC()
		}
	}
	return r, found
}

// absent reports whether a PROPFIND failed because nothing is there: servers answer 404, 409 or 410, and some 405 for a path
// below a file.
func absent(err error) bool {
	switch statusOf(err) {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusConflict, http.StatusGone:
		return true
	}
	return false
}

// lookup describes the resource at p, or reports that there is none.
func (f *FS) lookup(ctx context.Context, op, p string) (resource, bool, error) {
	found, err := f.find(ctx, f.target(p, false), "0")
	if err != nil {
		if absent(err) {
			return resource{}, false, nil
		}
		return resource{}, false, Wrap(op, p, err)
	}
	for _, r := range found {
		if r.path == p {
			return r, true, nil
		}
	}
	if len(found) == 1 {
		found[0].path = p
		return found[0], true, nil
	}
	return resource{}, false, fmt.Errorf("%s %q: %w: the server did not describe it", op, p, remotefs.ErrConnectionLost)
}

// needFolder checks that the parent of p is a folder that exists.
func (f *FS) needFolder(ctx context.Context, op, p string) error {
	parent, ok, err := f.lookup(ctx, op, path.Dir(p))
	switch {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("%s %q: %w: %q does not exist", op, p, remotefs.ErrNotFound, path.Dir(p))
	case !parent.folder:
		return fmt.Errorf("%s %q: %w: %q is a file", op, p, remotefs.ErrNotDirectory, path.Dir(p))
	}
	return nil
}

// entry describes a resource.
func entry(r resource) remotefs.Entry {
	e := remotefs.Entry{Name: path.Base(r.path), Path: r.path, Size: r.size, Mode: fileMode, ModTime: r.time, ModTimePrecision: remotefs.PrecisionSecond}
	if r.folder {
		e.Mode, e.Size = dirMode, 0
	}
	return e
}

// children lists the resources directly in the folder dir.
func (f *FS) children(ctx context.Context, dir string) ([]resource, error) {
	found, err := f.find(ctx, f.target(dir, true), "1")
	if err != nil {
		return nil, err
	}
	out := make([]resource, 0, len(found))
	for _, r := range found {
		if r.path != dir && path.Dir(r.path) == dir {
			out = append(out, r)
		}
	}
	return out, nil
}

// List implements remotefs.RemoteFS.
func (f *FS) List(ctx context.Context, dir string) ([]remotefs.Entry, error) {
	p, err := f.begin(ctx, "list", dir)
	if err != nil {
		return nil, err
	}
	r, ok, err := f.lookup(ctx, "list", p)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return nil, fmt.Errorf("list %q: %w", dir, remotefs.ErrNotFound)
	case !r.folder:
		return nil, fmt.Errorf("list %q: %w", dir, remotefs.ErrNotDirectory)
	}
	found, err := f.children(ctx, p)
	if err != nil {
		return nil, Wrap("list", dir, err)
	}
	entries := make([]remotefs.Entry, 0, len(found))
	for _, r := range found {
		entries = append(entries, entry(r))
	}
	return entries, nil
}

// Stat implements remotefs.RemoteFS.
func (f *FS) Stat(ctx context.Context, name string) (remotefs.Entry, error) {
	p, err := f.begin(ctx, "stat", name)
	if err != nil {
		return remotefs.Entry{}, err
	}
	r, ok, err := f.lookup(ctx, "stat", p)
	switch {
	case err != nil:
		return remotefs.Entry{}, err
	case !ok:
		return remotefs.Entry{}, fmt.Errorf("stat %q: %w", name, remotefs.ErrNotFound)
	case p == "/":
		return remotefs.Entry{Name: "/", Path: "/", Mode: dirMode, ModTime: r.time}, nil
	}
	return entry(r), nil
}

// OpenRead implements remotefs.RemoteFS. A server that ignores the range answers the whole file, whose first bytes are skipped.
func (f *FS) OpenRead(ctx context.Context, name string, offset int64) (io.ReadCloser, error) {
	p, err := f.begin(ctx, "read", name)
	if err != nil {
		return nil, err
	}
	r, ok, err := f.lookup(ctx, "read", p)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return nil, fmt.Errorf("read %q: %w", name, remotefs.ErrNotFound)
	case r.folder:
		return nil, fmt.Errorf("read %q: %w: it is a directory", name, remotefs.ErrInvalid)
	case offset < 0 || offset > r.size:
		return nil, fmt.Errorf("read %q at %d: %w: the file has %d bytes", name, offset, remotefs.ErrInvalid, r.size)
	case offset == r.size:
		return io.NopCloser(strings.NewReader("")), nil
	}
	header := http.Header{}
	if offset > 0 {
		header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := f.send(ctx, http.MethodGet, f.target(p, false), header, nil)
	if err != nil {
		return nil, Wrap("read", name, err)
	}
	if offset > 0 && resp.StatusCode != http.StatusPartialContent {
		if _, err := io.CopyN(io.Discard, resp.Body, offset); err != nil {
			_ = resp.Body.Close()
			return nil, Wrap("read", name, err)
		}
	}
	return &reader{body: resp.Body, path: name}, nil
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
		return n, Wrap("read", r.path, err)
	}
	return n, err
}

// Close implements io.Closer.
func (r *reader) Close() error { return r.body.Close() }

// OpenWrite implements remotefs.RemoteFS: what is written streams in one PUT, which Close finishes. Writes do not resume.
// Creating sends If-None-Match: *, so a file made meanwhile is not replaced. A write that fails removes the file it was creating.
func (f *FS) OpenWrite(ctx context.Context, name string, _ int64, mode remotefs.WriteMode) (io.WriteCloser, error) {
	p, err := f.begin(ctx, "write", name)
	if err != nil {
		return nil, err
	}
	if mode != remotefs.WriteCreate && mode != remotefs.WriteOverwrite {
		return nil, fmt.Errorf("write %q: %w: WebDAV writes cannot resume (mode %d)", name, remotefs.ErrInvalid, mode)
	}
	if p == "/" {
		return nil, fmt.Errorf("write %q: %w: it is a directory", name, remotefs.ErrInvalid)
	}
	if err := f.needFolder(ctx, "write", p); err != nil {
		return nil, err
	}
	r, ok, err := f.lookup(ctx, "write", p)
	switch {
	case err != nil:
		return nil, err
	case ok && r.folder:
		return nil, fmt.Errorf("write %q: %w: it is a directory", name, remotefs.ErrInvalid)
	case ok && mode == remotefs.WriteCreate:
		return nil, fmt.Errorf("write %q: %w", name, remotefs.ErrExists)
	}
	header := http.Header{"Content-Type": {"application/octet-stream"}}
	if mode == remotefs.WriteCreate {
		header.Set("If-None-Match", "*")
	}
	pr, pw := io.Pipe()
	w := &writer{ctx: ctx, fs: f, path: name, target: f.target(p, false), pipe: pw, fresh: !ok, result: make(chan error, 1)}
	go func() {
		err := f.do(ctx, http.MethodPut, w.target, header, pr)
		_ = pr.CloseWithError(err)
		w.result <- err
	}()
	return w, nil
}

// writer streams what is written into the body of a PUT.
type writer struct {
	ctx    context.Context
	fs     *FS
	path   string
	target string
	pipe   *io.PipeWriter
	fresh  bool
	result chan error
	done   bool
	err    error
}

// Write implements io.Writer.
func (w *writer) Write(b []byte) (int, error) {
	switch {
	case w.fs.closed.Load():
		_ = w.pipe.CloseWithError(remotefs.ErrConnectionLost)
		return 0, fmt.Errorf("write %q: %w", w.path, remotefs.ErrConnectionLost)
	case w.done:
		return 0, fmt.Errorf("write %q: %w: the upload is closed", w.path, remotefs.ErrInvalid)
	}
	n, err := w.pipe.Write(b)
	if err != nil {
		return n, Wrap("write", w.path, err)
	}
	return n, nil
}

// Close implements io.Closer: it ends the body and waits for the answer. When the context of the write ended, the upload is
// aborted, and a file it was creating is removed.
func (w *writer) Close() error {
	if w.done {
		return w.err
	}
	w.done = true
	if err := w.ctx.Err(); err != nil {
		_ = w.pipe.CloseWithError(err)
		<-w.result
		w.discard()
		w.err = err
		return err
	}
	_ = w.pipe.Close()
	if err := <-w.result; err != nil {
		w.discard()
		w.err = Wrap("write", w.path, err)
	}
	return w.err
}

// discard removes, as well as it can, the file a failed write was creating, which some servers keep with what arrived.
func (w *writer) discard() {
	if !w.fresh || w.fs.closed.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(w.ctx), 10*time.Second)
	defer cancel()
	_ = w.fs.do(ctx, http.MethodDelete, w.target, nil, nil)
}

// Mkdir implements remotefs.RemoteFS.
func (f *FS) Mkdir(ctx context.Context, name string) error {
	p, err := f.begin(ctx, "mkdir", name)
	if err != nil {
		return err
	}
	if err := f.needFolder(ctx, "mkdir", p); err != nil {
		return err
	}
	switch _, ok, err := f.lookup(ctx, "mkdir", p); {
	case err != nil:
		return err
	case ok:
		return fmt.Errorf("mkdir %q: %w", name, remotefs.ErrExists)
	}
	if err := f.do(ctx, "MKCOL", f.target(p, true), nil, nil); err != nil {
		if statusOf(err) == http.StatusMethodNotAllowed {
			return fmt.Errorf("mkdir %q: %w", name, remotefs.ErrExists)
		}
		return Wrap("mkdir", name, err)
	}
	return nil
}

// Remove implements remotefs.RemoteFS. DELETE of a folder removes everything in it, so a folder that is not empty is refused first.
func (f *FS) Remove(ctx context.Context, name string) error {
	p, err := f.begin(ctx, "remove", name)
	if err != nil {
		return err
	}
	if p == "/" {
		return fmt.Errorf("remove %q: %w: the root cannot be removed", name, remotefs.ErrPermission)
	}
	r, ok, err := f.lookup(ctx, "remove", p)
	switch {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("remove %q: %w", name, remotefs.ErrNotFound)
	}
	if r.folder {
		found, err := f.children(ctx, p)
		if err != nil {
			return Wrap("remove", name, err)
		}
		if len(found) > 0 {
			return fmt.Errorf("remove %q: %w", name, remotefs.ErrNotEmpty)
		}
	}
	if err := f.do(ctx, http.MethodDelete, f.target(p, r.folder), nil, nil); err != nil {
		return Wrap("remove", name, err)
	}
	return nil
}

// RemoveAll implements remotefs.RemoteFS: DELETE of a folder removes it with everything in it.
func (f *FS) RemoveAll(ctx context.Context, name string) error {
	p, err := f.begin(ctx, "remove", name)
	if err != nil {
		return err
	}
	if p == "/" {
		return fmt.Errorf("remove %q: %w: the root cannot be removed", name, remotefs.ErrPermission)
	}
	r, ok, err := f.lookup(ctx, "remove", p)
	if err != nil || !ok {
		return err
	}
	if err := f.do(ctx, http.MethodDelete, f.target(p, r.folder), nil, nil); err != nil && statusOf(err) != http.StatusNotFound {
		return Wrap("remove", name, err)
	}
	return nil
}

// Rename implements remotefs.RemoteFS: MOVE with Overwrite: F, so what is at the target is never replaced.
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
	r, ok, err := f.lookup(ctx, "rename", src)
	switch {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("rename %q: %w", from, remotefs.ErrNotFound)
	}
	if err := f.needFolder(ctx, "rename", dst); err != nil {
		return err
	}
	switch _, exists, err := f.lookup(ctx, "rename", dst); {
	case err != nil:
		return err
	case exists:
		return fmt.Errorf("rename %q to %q: %w", from, to, remotefs.ErrExists)
	}
	header := http.Header{"Destination": {f.target(dst, r.folder)}, "Overwrite": {"F"}}
	if err := f.do(ctx, "MOVE", f.target(src, r.folder), header, nil); err != nil {
		return Wrap("rename", from, err)
	}
	return nil
}

// Chmod implements remotefs.RemoteFS; WebDAV has no modes.
func (f *FS) Chmod(ctx context.Context, name string, _ fs.FileMode) error {
	if _, err := f.begin(ctx, "chmod", name); err != nil {
		return err
	}
	return fmt.Errorf("chmod %q: %w: WebDAV has no modes", name, remotefs.ErrInvalid)
}

// Chtimes implements remotefs.RemoteFS; RFC 4918 makes the modification time a property the server keeps.
func (f *FS) Chtimes(ctx context.Context, name string, _ time.Time) error {
	if _, err := f.begin(ctx, "chtimes", name); err != nil {
		return err
	}
	return fmt.Errorf("chtimes %q: %w: WebDAV servers keep modification times themselves", name, remotefs.ErrInvalid)
}

// Capabilities implements remotefs.RemoteFS: folders are real, but writes do not resume and files have no modes nor times
// that can be set.
func (f *FS) Capabilities() remotefs.Capabilities { return remotefs.Capabilities{} }

// Ping implements remotefs.RemoteFS: it checks that the root of the site is a folder that can be reached.
func (f *FS) Ping(ctx context.Context) error {
	if _, err := f.begin(ctx, "ping", "/"); err != nil {
		return err
	}
	found, err := f.find(ctx, f.base.String(), "0")
	if err != nil {
		return Wrap("ping", "/", err)
	}
	for _, r := range found {
		if r.path == "/" && r.folder {
			return nil
		}
	}
	return fmt.Errorf("ping %q: %w: the root of the site is not a folder", f.base.Path, remotefs.ErrNotDirectory)
}

// Close implements remotefs.RemoteFS. Requests that are running, uploads among them, are aborted, and later operations fail
// with [remotefs.ErrConnectionLost].
func (f *FS) Close() error {
	f.closed.Store(true)
	f.end()
	f.client.CloseIdleConnections()
	return nil
}
