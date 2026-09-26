// Package ftpfs implements [remotefs.RemoteFS] over FTP, explicit FTPS (AUTH TLS) and implicit FTPS, using jlaffaye/ftp.
//
// The adapter connects in passive mode only, and always to the address of the control connection: the address a server
// announces in PASV is never used, so a server cannot steer the data connection to a third party. Every connection,
// control and data, is opened through the dialer given in the configuration, so the network guard vets each one.
//
// FTP has one control connection that carries one command at a time. The adapter serializes its operations and keeps the
// connection reserved while a transfer stream is open; to work in parallel, open one adapter per connection.
//
// A server certificate is not checked against certificate authorities. Like an SSH host key it is pinned: its SHA-256
// fingerprint goes to [Config.VerifyHostKey] under the key type [CertificateKeyType], and the caller decides whether to trust it.
package ftpfs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jlaffaye/ftp"

	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// CertificateKeyType is the key type under which the fingerprint of a server certificate is reported and trusted.
const CertificateKeyType = "tls-certificate"

// TLSMode selects how the connection is protected.
type TLSMode int

const (
	// TLSNone sends everything in clear text, credentials included.
	TLSNone TLSMode = iota
	// TLSExplicit starts in clear text and upgrades the control connection with AUTH TLS.
	TLSExplicit
	// TLSImplicit expects TLS from the first byte, as on port 990.
	TLSImplicit
)

// maxTLSVersion caps FTPS at TLS 1.2. Servers such as vsftpd 3.0.3 demand session reuse on the data connections, and
// under TLS 1.3 they fail every upload of more than one record with "426 Failure reading network stream".
const maxTLSVersion = tls.VersionTLS12

const (
	sessionCacheSize = 8
	dirPermissions   = fs.ModeDir | 0o755
	filePermissions  = 0o644
	linkPermissions  = fs.ModeSymlink | 0o777
)

// Config describes how to connect to a server.
type Config struct {
	// Address is the host:port of the server.
	Address string
	// Username is the account name; "anonymous" for public servers.
	Username string
	// Password is the account password; it may be empty.
	Password string
	// TLS selects clear text, explicit FTPS or implicit FTPS.
	TLS TLSMode
	// VerifyHostKey judges the fingerprint of the certificate the server presents, under [CertificateKeyType]. It returns nil
	// for a trusted certificate and a [remotefs.HostKeyError] for an unknown or changed one. It is mandatory with TLS.
	VerifyHostKey func(keyType, fingerprint string) error
	// Location is the time zone of the times in LIST replies, which carry none; nil means UTC. MLSD needs no zone.
	Location *time.Location
	// DisableEPSV skips EPSV and uses PASV, for servers that announce EPSV but do not honor it.
	DisableEPSV bool
	// DialContext opens every TCP connection; nil uses a plain dialer. Give the dialer of the network guard here.
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
	// Timeout bounds establishing each TCP connection and, as a whole, connecting: the greeting and the login too, so a
	// server that accepts the connection and never answers, or answers in another dialect of TLS, cannot hang it. Zero means no limit.
	Timeout time.Duration
}

// FS is a connection to an FTP server. It is safe for concurrent use; operations take turns.
type FS struct {
	turn    chan struct{}
	conn    *ftp.ServerConn
	link    *link
	closed  atomic.Bool
	broken  atomic.Bool
	chtimes bool
}

var _ remotefs.RemoteFS = (*FS)(nil)

// link tracks the connections of one FTP session and dials them on behalf of the client library.
type link struct {
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	shut     bool
	ctx      atomic.Pointer[context.Context]
	dial     func(ctx context.Context, network, address string) (net.Conn, error)
	tls      *tls.Config
	mode     TLSMode
	control  bool
	verifier *verifier
}

// tracked is a connection that removes itself from its link when closed.
type tracked struct {
	net.Conn
	owner *link
}

// Close forgets the connection and closes it.
func (c *tracked) Close() error {
	c.owner.mu.Lock()
	delete(c.owner.conns, c.Conn)
	c.owner.mu.Unlock()
	return c.Conn.Close()
}

// verifier remembers why a certificate was rejected, so the caller can report the fingerprint that was refused.
type verifier struct {
	mu       sync.Mutex
	check    func(keyType, fingerprint string) error
	rejected error
}

// verify judges the certificate chain a server presented; only the first certificate matters, since it is pinned.
func (v *verifier) verify(chain [][]byte, _ [][]*x509.Certificate) error {
	if len(chain) == 0 {
		return errors.New("the server presented no certificate")
	}
	err := v.check(CertificateKeyType, fingerprintOf(chain[0]))
	v.mu.Lock()
	v.rejected = err
	v.mu.Unlock()
	return err
}

// reason returns the error that made the last certificate check fail, if any.
func (v *verifier) reason() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.rejected
}

// currentContext returns the context of the operation that is running, which bounds the connections it opens.
func (l *link) currentContext() context.Context {
	return *l.ctx.Load()
}

// open dials one connection for the client library. The first is the control connection; the rest carry data. Under
// implicit FTPS the control connection starts with TLS; data connections always do once TLS is on, since the library
// asks for protected data (PROT P) in both FTPS modes. The TLS handshake is left to the first read or write: the library
// opens a data connection before it sends the command that makes the server start its side of the handshake.
func (l *link) open(_, address string) (net.Conn, error) {
	ctx := l.currentContext()
	raw, err := l.dial(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	conn := &tracked{Conn: raw, owner: l}
	l.mu.Lock()
	if l.shut {
		l.mu.Unlock()
		_ = conn.Close()
		return nil, net.ErrClosed
	}
	l.conns[raw] = struct{}{}
	control := !l.control
	l.control = true
	l.mu.Unlock()

	secure := l.mode == TLSImplicit || (l.mode == TLSExplicit && !control)
	if !secure {
		return conn, nil
	}
	return tls.Client(conn, l.tls), nil
}

// shutdown closes every connection of the session and refuses new ones.
func (l *link) shutdown() {
	l.mu.Lock()
	l.shut = true
	open := make([]net.Conn, 0, len(l.conns))
	for conn := range l.conns {
		open = append(open, conn)
	}
	l.mu.Unlock()
	for _, conn := range open {
		_ = conn.Close()
	}
}

// Dial connects, checks the certificate when TLS is on, authenticates and reads the features of the server.
// Failures are [remotefs.ErrHostKeyUnknown] or [remotefs.ErrHostKeyMismatch] for certificates, [remotefs.ErrAuth] for
// credentials, the context error when canceled, [netguard.ErrBlocked] for forbidden destinations and
// [remotefs.ErrConnectionLost] otherwise. Only a lost connection is worth retrying.
func Dial(ctx context.Context, cfg Config) (*FS, error) {
	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil || host == "" {
		return nil, fmt.Errorf("%w: %q is not a host:port address", remotefs.ErrInvalid, cfg.Address)
	}
	if cfg.Username == "" {
		return nil, fmt.Errorf("%w: no user name given", remotefs.ErrAuth)
	}
	if cfg.TLS != TLSNone && cfg.VerifyHostKey == nil {
		return nil, fmt.Errorf("%w: certificate verification is required with TLS", remotefs.ErrInvalid)
	}

	session := &link{conns: map[net.Conn]struct{}{}, mode: cfg.TLS, dial: cfg.DialContext}
	if session.dial == nil {
		session.dial = (&net.Dialer{Timeout: cfg.Timeout}).DialContext
	}
	session.ctx.Store(&ctx)
	options := []ftp.DialOption{
		ftp.DialWithDialFunc(session.open),
		ftp.DialWithLocation(location(cfg.Location)),
		ftp.DialWithDisabledEPSV(cfg.DisableEPSV),
		ftp.DialWithShutTimeout(cfg.Timeout),
	}
	if cfg.TLS != TLSNone {
		session.verifier = &verifier{check: cfg.VerifyHostKey}
		session.tls = &tls.Config{
			ServerName:         host,
			MinVersion:         tls.VersionTLS12,
			MaxVersion:         maxTLSVersion,
			ClientSessionCache: tls.NewLRUClientSessionCache(sessionCacheSize),
			//nolint:gosec // The certificate is pinned by fingerprint in VerifyPeerCertificate, as an SSH host key is.
			InsecureSkipVerify:    true,
			VerifyPeerCertificate: session.verifier.verify,
		}
		if cfg.TLS == TLSExplicit {
			options = append(options, ftp.DialWithExplicitTLS(session.tls))
		} else {
			options = append(options, ftp.DialWithTLS(session.tls))
		}
	}

	parent := ctx
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
		session.ctx.Store(&ctx)
	}
	stop := context.AfterFunc(ctx, session.shutdown)
	conn, err := ftp.Dial(cfg.Address, options...)
	if err == nil {
		err = conn.Login(cfg.Username, cfg.Password)
		if err != nil {
			_ = conn.Quit()
		}
	}
	if !stop() {
		session.shutdown()
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		return nil, fmt.Errorf("connecting to %s: %w: no answer within %v", cfg.Address, remotefs.ErrConnectionLost, cfg.Timeout)
	}
	if err != nil {
		session.shutdown()
		return nil, dialError(cfg.Address, session.verifier, err)
	}
	session.ctx.Store(&parent)
	return &FS{turn: make(chan struct{}, 1), conn: conn, link: session, chtimes: conn.IsSetTimeSupported()}, nil
}

// location returns the zone of LIST times, UTC when none is given.
func location(l *time.Location) *time.Location {
	if l == nil {
		return time.UTC
	}
	return l
}

// dialError explains why connecting or logging in failed.
func dialError(address string, v *verifier, err error) error {
	if v != nil {
		if rejected := v.reason(); rejected != nil {
			return fmt.Errorf("verifying the certificate of %s: %w", address, rejected)
		}
	}
	if errors.Is(err, netguard.ErrBlocked) {
		return fmt.Errorf("connecting to %s: %w", address, err)
	}
	var reply interface{ Error() string }
	if errors.As(err, &reply) && strings.Contains(err.Error(), "534") {
		return fmt.Errorf("%w: %s does not accept AUTH TLS: %w", remotefs.ErrInvalid, address, err)
	}
	return fmt.Errorf("connecting to %s: %w", address, classify(err))
}

// guard fails when the file system is closed, its connection was cut, or the context ended.
func (f *FS) guard(ctx context.Context) error {
	if f.closed.Load() {
		return fmt.Errorf("%w: the file system is closed", remotefs.ErrConnectionLost)
	}
	if f.broken.Load() {
		return fmt.Errorf("%w: the connection was cut", remotefs.ErrConnectionLost)
	}
	return ctx.Err()
}

// abort cuts the connections: an FTP command cannot be interrupted any other way, and the session is unusable after.
func (f *FS) abort() {
	f.broken.Store(true)
	f.link.shutdown()
}

// begin waits for its turn on the control connection, honoring the context, and starts watching the context. The returned function ends the
// operation, releases the connection, and reports whether the context ended it.
func (f *FS) begin(ctx context.Context) (end func() bool, err error) {
	if err := f.guard(ctx); err != nil {
		return nil, err
	}
	select {
	case f.turn <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := f.guard(ctx); err != nil {
		<-f.turn
		return nil, err
	}
	f.link.ctx.Store(&ctx)
	stop := context.AfterFunc(ctx, f.abort)
	return func() bool {
		ok := stop()
		<-f.turn
		return ok
	}, nil
}

// run takes its turn on the control connection and runs one operation on it. When the context ends the operation, the
// result is the context error; otherwise it is what fn returned, whose errors must already be classified.
func (f *FS) run(ctx context.Context, fn func() error) error {
	end, err := f.begin(ctx)
	if err != nil {
		return err
	}
	err = fn()
	if !end() {
		return ctx.Err()
	}
	return err
}

// do runs one operation on a path, wrapping its error with the operation and path.
func (f *FS) do(ctx context.Context, op, p string, fn func() error) error {
	if err := validPath(p); err != nil {
		return fmt.Errorf("%s %q: %w", op, p, err)
	}
	if err := f.run(ctx, fn); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return wrap(op, p, err)
	}
	return nil
}

// wrap names the operation and path in an error that is already classified.
func wrap(op, p string, err error) error {
	return fmt.Errorf("%s %s: %w", op, p, err)
}

// validPath rejects paths that could smuggle a second command into the control connection or are not absolute.
func validPath(p string) error {
	if p == "" || p[0] != '/' {
		return fmt.Errorf("%w: the path must be absolute", remotefs.ErrInvalid)
	}
	if strings.ContainsAny(p, "\r\n\x00") {
		return fmt.Errorf("%w: the path has control characters", remotefs.ErrInvalid)
	}
	return nil
}

// toEntry converts a listed entry of dir.
func toEntry(dir string, e *ftp.Entry, precise bool) remotefs.Entry {
	mode := fs.FileMode(filePermissions)
	switch e.Type {
	case ftp.EntryTypeFolder:
		mode = dirPermissions
	case ftp.EntryTypeLink:
		mode = linkPermissions
	}
	precision := remotefs.PrecisionSecond
	if !precise {
		precision = remotefs.PrecisionMinute
		if e.Time.Hour() == 0 && e.Time.Minute() == 0 && e.Time.Second() == 0 {
			precision = remotefs.PrecisionDay
		}
	}
	return remotefs.Entry{
		Name:             e.Name,
		Path:             path.Join(dir, e.Name),
		Size:             int64(e.Size), //nolint:gosec // a file size fits an int64
		Mode:             mode,
		ModTime:          e.Time.UTC(),
		ModTimePrecision: precision,
	}
}

// rootEntry describes the root directory, which no listing shows.
func rootEntry() remotefs.Entry {
	return remotefs.Entry{Name: "/", Path: "/", Mode: dirPermissions}
}

// pathState is what a path currently is on the server.
type pathState int

const (
	stateMissing pathState = iota
	stateFile
	stateDir
)

// listing lists a directory, mapping the entries; the caller holds the connection.
func (f *FS) listing(dir string) ([]remotefs.Entry, error) {
	raw, err := f.conn.List(dir)
	if err != nil {
		return nil, err
	}
	return convert(dir, raw, f.conn.IsTimePreciseInList()), nil
}

// convert maps the entries of a listing, leaving out the entries that stand for the directory itself and its parent.
func convert(dir string, raw []*ftp.Entry, precise bool) []remotefs.Entry {
	entries := make([]remotefs.Entry, 0, len(raw))
	for _, e := range raw {
		if e.Name == "" || e.Name == "." || e.Name == ".." {
			continue
		}
		entries = append(entries, toEntry(dir, e, precise))
	}
	return entries
}

// lookup describes one path; the caller holds the connection. A missing path is [remotefs.ErrNotFound].
func (f *FS) lookup(p string) (remotefs.Entry, error) {
	if p == "/" {
		return rootEntry(), nil
	}
	if f.conn.IsTimePreciseInList() {
		e, err := f.conn.GetEntry(p)
		if err != nil {
			return remotefs.Entry{}, classify(err)
		}
		e.Name = path.Base(p)
		return toEntry(path.Dir(p), e, true), nil
	}
	siblings, err := f.listing(path.Dir(p))
	if err != nil {
		return remotefs.Entry{}, classify(err)
	}
	for _, e := range siblings {
		if e.Name == path.Base(p) {
			return e, nil
		}
	}
	return remotefs.Entry{}, fmt.Errorf("%w: %s", remotefs.ErrNotFound, p)
}

// state tells what a path is; the caller holds the connection. Only "missing" turns a not found reply into a state.
func (f *FS) state(p string) (pathState, remotefs.Entry, error) {
	e, err := f.lookup(p)
	switch {
	case errors.Is(err, remotefs.ErrNotFound):
		return stateMissing, remotefs.Entry{}, nil
	case err != nil:
		return stateMissing, remotefs.Entry{}, err
	case e.IsDir():
		return stateDir, e, nil
	}
	return stateFile, e, nil
}

// parentProblem explains why a path cannot be created below its parent: the parent is missing or is not a directory.
func (f *FS) parentProblem(p string) error {
	st, _, err := f.state(path.Dir(p))
	switch {
	case err != nil:
		return classify(err)
	case st == stateMissing:
		return fmt.Errorf("%w: the parent directory of %s", remotefs.ErrNotFound, p)
	case st == stateFile:
		return fmt.Errorf("%w: the parent of %s is a file", remotefs.ErrNotDirectory, p)
	}
	return nil
}

// List returns the entries of a directory. A server that only offers LIST answers a listing of a file with the file
// itself, and one of a missing path with nothing; those answers are checked, so a file is [remotefs.ErrNotDirectory] and a
// missing path is [remotefs.ErrNotFound].
func (f *FS) List(ctx context.Context, dir string) ([]remotefs.Entry, error) {
	var entries []remotefs.Entry
	err := f.do(ctx, "list", dir, func() error {
		var err error
		if entries, err = f.listing(dir); err != nil {
			return f.refine(dir, err)
		}
		if dir != "/" && !f.conn.IsTimePreciseInList() && (len(entries) == 0 || (len(entries) == 1 && entries[0].Name == path.Base(dir))) {
			return f.checkDirectory(dir)
		}
		return nil
	})
	return entries, err
}

// checkDirectory tells whether dir is a directory, for the listings of servers that only have LIST. Such a server
// answers a listing of a missing path with nothing, and a listing of a file with the file itself, so an empty answer
// and an answer that is one entry named like the path are checked. The caller holds the connection.
func (f *FS) checkDirectory(dir string) error {
	st, _, err := f.state(dir)
	switch {
	case err != nil:
		return classify(err)
	case st == stateMissing:
		return fmt.Errorf("%w: %s", remotefs.ErrNotFound, dir)
	case st == stateFile:
		return fmt.Errorf("%w: %s", remotefs.ErrNotDirectory, dir)
	}
	return nil
}

// refine improves the classification of a failed command on p by looking at what p is; the caller holds the connection.
func (f *FS) refine(p string, err error) error {
	class := classify(err)
	if !errors.Is(class, remotefs.ErrPermission) && !errors.Is(class, remotefs.ErrNotFound) && !errors.Is(class, remotefs.ErrInvalid) {
		return class
	}
	st, _, probeErr := f.state(p)
	switch {
	case probeErr != nil:
		return class
	case st == stateMissing:
		return fmt.Errorf("%w: %s: %w", remotefs.ErrNotFound, p, err)
	case st == stateFile:
		return fmt.Errorf("%w: %s: %w", remotefs.ErrNotDirectory, p, err)
	}
	return class
}

// Stat describes a single path.
func (f *FS) Stat(ctx context.Context, p string) (remotefs.Entry, error) {
	var entry remotefs.Entry
	err := f.do(ctx, "stat", p, func() error {
		var err error
		entry, err = f.lookup(p)
		return err
	})
	return entry, err
}

// Mkdir creates one directory.
func (f *FS) Mkdir(ctx context.Context, p string) error {
	return f.do(ctx, "mkdir", p, func() error {
		err := f.conn.MakeDir(p)
		if err == nil {
			return nil
		}
		if st, _, probeErr := f.state(p); probeErr == nil && st != stateMissing {
			return fmt.Errorf("%w: %s", remotefs.ErrExists, p)
		}
		if problem := f.parentProblem(p); problem != nil && !errors.Is(problem, remotefs.ErrPermission) {
			return problem
		}
		return classify(err)
	})
}

// Remove deletes a file or an empty directory.
func (f *FS) Remove(ctx context.Context, p string) error {
	if p == "/" {
		return fmt.Errorf("remove /: %w: the root cannot be removed", remotefs.ErrPermission)
	}
	return f.do(ctx, "remove", p, func() error {
		st, _, err := f.state(p)
		switch {
		case err != nil:
			return classify(err)
		case st == stateMissing:
			return fmt.Errorf("%w: %s", remotefs.ErrNotFound, p)
		case st == stateFile:
			return classify(f.conn.Delete(p))
		}
		if err := f.conn.RemoveDir(p); err != nil {
			if children, listErr := f.listing(p); listErr == nil && len(children) > 0 {
				return fmt.Errorf("%w: %s", remotefs.ErrNotEmpty, p)
			}
			return classify(err)
		}
		return nil
	})
}

// RemoveAll deletes a path and everything below it; a missing path is not an error.
func (f *FS) RemoveAll(ctx context.Context, p string) error {
	if p == "/" {
		return fmt.Errorf("remove all /: %w: the root cannot be removed", remotefs.ErrPermission)
	}
	return f.do(ctx, "remove all", p, func() error {
		st, _, err := f.state(p)
		switch {
		case err != nil:
			return classify(err)
		case st == stateMissing:
			return nil
		case st == stateFile:
			return classify(f.conn.Delete(p))
		}
		return classify(f.conn.RemoveDirRecur(p))
	})
}

// Rename moves a path; the target must not exist.
func (f *FS) Rename(ctx context.Context, from, to string) error {
	if err := validPath(to); err != nil {
		return fmt.Errorf("rename to %q: %w", to, err)
	}
	if strings.HasPrefix(to+"/", from+"/") {
		return fmt.Errorf("rename %s: %w: a path cannot move into itself", from, remotefs.ErrInvalid)
	}
	return f.do(ctx, "rename", from, func() error {
		if st, _, err := f.state(from); err != nil {
			return classify(err)
		} else if st == stateMissing {
			return fmt.Errorf("%w: %s", remotefs.ErrNotFound, from)
		}
		if st, _, err := f.state(to); err != nil {
			return classify(err)
		} else if st != stateMissing {
			return fmt.Errorf("%w: %s", remotefs.ErrExists, to)
		}
		if err := f.conn.Rename(from, to); err != nil {
			if problem := f.parentProblem(to); problem != nil && !errors.Is(problem, remotefs.ErrPermission) {
				return problem
			}
			return classify(err)
		}
		return nil
	})
}

// Chmod is not supported: FTP has no standard command to change permissions.
func (f *FS) Chmod(ctx context.Context, _ string, _ fs.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("chmod: %w: FTP cannot change permissions", remotefs.ErrInvalid)
}

// Chtimes changes the modification time with MFMT, when the server offers it.
func (f *FS) Chtimes(ctx context.Context, p string, mtime time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !f.chtimes {
		return fmt.Errorf("chtimes %s: %w: the server does not support MFMT", p, remotefs.ErrInvalid)
	}
	return f.do(ctx, "chtimes", p, func() error {
		return f.refine(p, f.conn.SetTime(p, mtime.UTC()))
	})
}

// Capabilities reports that transfers can resume, but only at the end of the file, that permissions cannot change and
// whether times can.
func (f *FS) Capabilities() remotefs.Capabilities {
	return remotefs.Capabilities{Resume: true, Rewrite: false, Chmod: false, Chtimes: f.chtimes}
}

// Ping checks that the control connection answers.
func (f *FS) Ping(ctx context.Context) error {
	return f.run(ctx, func() error {
		if err := f.conn.NoOp(); err != nil {
			return fmt.Errorf("ping: %w", classify(err))
		}
		return nil
	})
}

// Close ends the session; a transfer in flight is cut, and later operations report a lost connection. When no operation is
// running the server is told goodbye first.
func (f *FS) Close() error {
	if !f.closed.CompareAndSwap(false, true) {
		return nil
	}
	select {
	case f.turn <- struct{}{}:
		if !f.broken.Load() {
			_ = f.conn.Quit()
		}
		<-f.turn
	default:
	}
	f.link.shutdown()
	return nil
}

// readStream is a download that keeps the control connection reserved until it is closed.
type readStream struct {
	response *ftp.Response
	end      func() bool
	ctx      context.Context
	done     bool
	finished bool
}

// Read reads the next bytes of the file.
func (r *readStream) Read(p []byte) (int, error) {
	n, err := r.response.Read(p)
	if errors.Is(err, io.EOF) {
		r.done = true
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, classify(err)
	}
	return n, err
}

// Close ends the transfer and releases the connection. Stopping before the end is not an error.
func (r *readStream) Close() error {
	if r.finished {
		return nil
	}
	r.finished = true
	err := r.response.Close()
	if !r.end() {
		return r.ctx.Err()
	}
	if err != nil && r.done {
		return classify(err)
	}
	return nil
}

// OpenRead streams a file starting at offset. The control connection stays reserved until the stream is closed.
func (f *FS) OpenRead(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	if err := validPath(p); err != nil {
		return nil, fmt.Errorf("read %q: %w", p, err)
	}
	if offset < 0 {
		return nil, fmt.Errorf("read %s: %w: negative offset", p, remotefs.ErrInvalid)
	}
	end, err := f.begin(ctx)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (io.ReadCloser, error) {
		if !end() {
			return nil, ctx.Err()
		}
		return nil, wrap("read", p, err)
	}
	st, entry, err := f.state(p)
	switch {
	case err != nil:
		return fail(classify(err))
	case st == stateMissing:
		return fail(fmt.Errorf("%w: %s", remotefs.ErrNotFound, p))
	case st == stateDir:
		return fail(fmt.Errorf("%w: %s is a directory", remotefs.ErrInvalid, p))
	case offset > entry.Size:
		return fail(fmt.Errorf("%w: offset %d is past the %d bytes of %s", remotefs.ErrInvalid, offset, entry.Size, p))
	}
	response, err := f.conn.RetrFrom(p, uint64(offset)) //nolint:gosec // checked not to be negative
	if err != nil {
		return fail(classify(err))
	}
	return &readStream{response: response, end: end, ctx: ctx}, nil
}

// writeStream is an upload that keeps the control connection reserved until it is closed.
type writeStream struct {
	owner    *FS
	pipe     *io.PipeWriter
	result   chan error
	end      func() bool
	ctx      context.Context
	p        string
	finished bool
}

// Write sends bytes to the server.
func (w *writeStream) Write(b []byte) (int, error) {
	if w.owner.closed.Load() || w.owner.broken.Load() {
		return 0, fmt.Errorf("%w: the connection was closed", remotefs.ErrConnectionLost)
	}
	n, err := w.pipe.Write(b)
	if err != nil {
		return n, classify(err)
	}
	return n, nil
}

// Close finishes the upload, waits for the server to confirm it and releases the connection.
func (w *writeStream) Close() error {
	if w.finished {
		return nil
	}
	w.finished = true
	_ = w.pipe.Close()
	err := <-w.result
	if !w.end() {
		return w.ctx.Err()
	}
	if err != nil {
		return wrap("write", w.p, classify(err))
	}
	return nil
}

// OpenWrite streams data into a file according to mode. The control connection stays reserved until the stream is closed.
func (f *FS) OpenWrite(ctx context.Context, p string, offset int64, mode remotefs.WriteMode) (io.WriteCloser, error) {
	if err := validPath(p); err != nil {
		return nil, fmt.Errorf("write %q: %w", p, err)
	}
	if mode < remotefs.WriteCreate || mode > remotefs.WriteResume {
		return nil, fmt.Errorf("write %s: %w: unknown write mode %d", p, remotefs.ErrInvalid, mode)
	}
	end, err := f.begin(ctx)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (io.WriteCloser, error) {
		if !end() {
			return nil, ctx.Err()
		}
		return nil, wrap("write", p, err)
	}

	st, entry, err := f.state(p)
	if err != nil {
		return fail(classify(err))
	}
	switch {
	case st == stateDir:
		return fail(fmt.Errorf("%w: %s is a directory", remotefs.ErrInvalid, p))
	case mode == remotefs.WriteCreate && st == stateFile:
		return fail(fmt.Errorf("%w: %s", remotefs.ErrExists, p))
	case mode == remotefs.WriteResume && st == stateMissing:
		return fail(fmt.Errorf("%w: %s", remotefs.ErrNotFound, p))
	case mode == remotefs.WriteResume && offset != entry.Size:
		return fail(fmt.Errorf("%w: FTP can only resume at the end of the file: offset %d, size %d of %s", remotefs.ErrInvalid, offset, entry.Size, p))
	case st == stateMissing:
		if problem := f.parentProblem(p); problem != nil && !errors.Is(problem, remotefs.ErrPermission) {
			return fail(problem)
		}
	}

	reader, writer := io.Pipe()
	result := make(chan error, 1)
	go func() {
		var err error
		if mode == remotefs.WriteResume {
			err = f.conn.Append(p, reader)
		} else {
			err = f.conn.Stor(p, reader)
		}
		_ = reader.CloseWithError(err)
		result <- err
	}()
	return &writeStream{owner: f, pipe: writer, result: result, end: end, ctx: ctx, p: p}, nil
}
