// Package sftpfs implements [remotefs.RemoteFS] over SFTP, using SSH from golang.org/x/crypto and pkg/sftp.
//
// SFTP version 3 reports many conditions as one generic failure. The adapter looks at the remote path after a
// failure to tell "already exists", "not a directory" and "directory not empty" apart, so callers see the
// same errors as with any other protocol.
package sftpfs

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/prompts"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

const (
	maxPacket = 32768
	// bulkPacket is how many bytes each read of a strict server asks for, the most a reply of pkg/sftp can carry: strict servers
	// are read one request at a time, so larger requests are what makes them fast on links with a long round trip.
	bulkPacket            = 255 << 10
	maxConcurrentRequests = 64
)

// Config describes how to connect to a server.
type Config struct {
	// Address is the host:port of the server.
	Address string
	// Username is the account name.
	Username string
	// Password enables password authentication, also offered through keyboard-interactive.
	Password string
	// PrivateKey enables public key authentication; it holds a PEM or OpenSSH private key.
	PrivateKey []byte
	// Passphrase decrypts PrivateKey when it is encrypted.
	Passphrase []byte
	// VerifyHostKey judges the key the server presents. It returns nil for a trusted key and a
	// [remotefs.HostKeyError] for an unknown or changed one. It is mandatory: a connection never skips it.
	VerifyHostKey func(keyType, fingerprint string) error
	// HostKeyAlgorithms restricts the accepted key types, so a server cannot present a type that was never trusted.
	HostKeyAlgorithms []string
	// DialContext opens the TCP connection; nil uses a plain dialer. Give the dialer of the network guard here.
	// Ignored when Conn is set.
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
	// Conn, when set, is used as the connection instead of dialing Address: the channel a jump host's
	// [ssh.Client.DialContext] opened to the real server, already past the network guard when the jump host itself
	// was dialed.
	Conn net.Conn
	// Timeout bounds establishing the TCP connection; zero means no limit.
	Timeout time.Duration
	// Prompt, when set, asks the person connecting whatever the server asks (ADR-0016): each keyboard-interactive
	// challenge, then the password for servers that only take that. Its failures are refused credentials.
	Prompt func(prompts.Challenge) ([]string, error)
	// Strict remembers the servers that proved strict across connections; nil remembers nothing.
	Strict *StrictServers
	// InexactSizes treats the server as strict from the start, as the site asks when its sizes are known not to hold.
	InexactSizes bool
	// Sudo, when set, runs the SFTP server of the host through sudo as another user (ADR-0027) instead of the sftp subsystem.
	Sudo *Sudo
}

// maxStrictServers bounds how many servers a [StrictServers] remembers; once full it forgets them all and learns again.
const maxStrictServers = 1024

// StrictStore keeps the servers that proved strict across restarts of Transferly.
type StrictStore interface {
	// StrictServers lists the addresses of the servers that proved strict.
	StrictServers(ctx context.Context) ([]string, error)
	// RememberStrictServer records that the server at address proved strict.
	RememberStrictServer(ctx context.Context, address string) error
}

// StrictServers remembers, by address, the SFTP servers that proved strict: a whole file did not have the size they reported,
// or they dropped the session at once under pipelined reads. Connections to them read one request at a time and do not trust
// reported sizes. It outlives connections, since a dropped session is discarded with what it learned, and with a [StrictStore]
// it outlives Transferly too: it reads the store once, on first use, and writes each server it learns. The zero value is ready,
// remembers in memory only, and it is safe for concurrent use.
type StrictServers struct {
	// Store keeps what is learned across restarts; nil keeps it in memory only.
	Store StrictStore

	mu      sync.Mutex
	loaded  bool
	servers map[string]struct{}
}

// load reads the store the first time it is needed; the caller holds the lock. A store that cannot be read is tried again later.
func (s *StrictServers) load() {
	if s.loaded || s.Store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), strictStoreTimeout)
	defer cancel()
	addresses, err := s.Store.StrictServers(ctx)
	if err != nil {
		return
	}
	s.loaded = true
	if s.servers == nil {
		s.servers = map[string]struct{}{}
	}
	for _, address := range addresses {
		s.servers[address] = struct{}{}
	}
}

// Has reports whether the server at address proved strict.
func (s *StrictServers) Has(address string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	_, ok := s.servers[address]
	return ok
}

// add remembers that the server at address proved strict, and tells the store when it is new.
func (s *StrictServers) add(address string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.load()
	if s.servers == nil || len(s.servers) >= maxStrictServers {
		s.servers = map[string]struct{}{}
	}
	_, known := s.servers[address]
	s.servers[address] = struct{}{}
	store := s.Store
	s.mu.Unlock()
	if known || store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), strictStoreTimeout)
	defer cancel()
	_ = store.RememberStrictServer(ctx, address)
}

// strictStoreTimeout bounds each read and write of the [StrictStore].
const strictStoreTimeout = 5 * time.Second

// FS is a connection to an SFTP server. It is safe for concurrent use.
type FS struct {
	ssh      *ssh.Client
	client   *sftp.Client
	address  string
	known    *StrictServers
	strict   atomic.Bool
	closed   atomic.Bool
	once     sync.Once
	cmds     commands
	elevated bool
	bulkOnce sync.Once
	bulk     *sftp.Client
}

var (
	_ remotefs.RemoteFS    = (*FS)(nil)
	_ remotefs.SizeChecker = (*FS)(nil)
	_ remotefs.Homer       = (*FS)(nil)
)

// authMethods builds the authentication methods for the credentials in cfg.
func authMethods(cfg Config) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if len(cfg.PrivateKey) > 0 {
		var signer ssh.Signer
		var err error
		if len(cfg.Passphrase) > 0 {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(cfg.PrivateKey, cfg.Passphrase)
		} else {
			signer, err = ssh.ParsePrivateKey(cfg.PrivateKey)
		}
		var missing *ssh.PassphraseMissingError
		switch {
		case errors.As(err, &missing):
			return nil, fmt.Errorf("%w: the private key needs a passphrase", remotefs.ErrAuth)
		case errors.Is(err, x509.IncorrectPasswordError):
			return nil, fmt.Errorf("%w: wrong passphrase for the private key", remotefs.ErrAuth)
		case err != nil:
			return nil, fmt.Errorf("%w: unreadable private key", remotefs.ErrAuth)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		methods = append(methods, ssh.Password(cfg.Password), ssh.KeyboardInteractive(passwordAnswers(cfg.Password)))
	}
	if cfg.Prompt != nil {
		methods = append(methods, ssh.KeyboardInteractive(relay(cfg.Prompt)), ssh.PasswordCallback(askPassword(cfg.Prompt)))
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("%w: no password or private key given", remotefs.ErrAuth)
	}
	return methods, nil
}

// passwordAnswers answers the first keyboard-interactive challenge that asks something with the password, every question
// alike. A second one means the server did not take it: answering again would only repeat the same password until the
// server gives up and disconnects, which reads as a lost connection, so the challenge fails as refused credentials
// instead. Challenges without questions, such as a banner, are acknowledged.
func passwordAnswers(password string) ssh.KeyboardInteractiveChallenge {
	asked := false
	return func(_, _ string, questions []string, _ []bool) ([]string, error) {
		if len(questions) == 0 {
			return nil, nil
		}
		if asked {
			return nil, fmt.Errorf("%w: the server asked again after the password", remotefs.ErrAuth)
		}
		asked = true
		answers := make([]string, len(questions))
		for i := range answers {
			answers[i] = password
		}
		return answers, nil
	}
}

// relay hands each keyboard-interactive challenge to the person connecting, as the server words it. Challenges without
// questions, such as a banner, are acknowledged without asking.
func relay(prompt func(prompts.Challenge) ([]string, error)) ssh.KeyboardInteractiveChallenge {
	return func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		if len(questions) == 0 {
			return nil, nil
		}
		challenge := prompts.Challenge{Name: name, Instruction: instruction, Questions: make([]prompts.Question, len(questions))}
		for i, text := range questions {
			challenge.Questions[i] = prompts.Question{Text: text, Echo: i < len(echos) && echos[i]}
		}
		answers, err := prompt(challenge)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", remotefs.ErrAuth, err)
		}
		return answers, nil
	}
}

// passwordQuestion is how the password is asked for when the server takes the password method.
const passwordQuestion = "Password:"

// askPassword asks the person connecting for the password, for servers that take the password method but not
// keyboard-interactive.
func askPassword(prompt func(prompts.Challenge) ([]string, error)) func() (string, error) {
	return func() (string, error) {
		answers, err := prompt(prompts.Challenge{Questions: []prompts.Question{{Text: passwordQuestion}}})
		if err != nil {
			return "", fmt.Errorf("%w: %w", remotefs.ErrAuth, err)
		}
		return answers[0], nil
	}
}

// keyExchanges are the key exchange algorithms offered to servers, best first. They all use SHA-2 or elliptic curves: the
// SHA-1 Diffie-Hellman groups that the library still offers by default are left out, so a server that only speaks them is
// refused instead of negotiating a weaker session.
var keyExchanges = []string{
	"mlkem768x25519-sha256",
	"curve25519-sha256",
	"curve25519-sha256@libssh.org",
	"ecdh-sha2-nistp256",
	"ecdh-sha2-nistp384",
	"ecdh-sha2-nistp521",
	"diffie-hellman-group14-sha256",
	"diffie-hellman-group16-sha512",
}

// ciphers are offered in the order OpenSSH prefers them, ChaCha20-Poly1305 and CTR before AES-GCM. The library puts AES-GCM first,
// and some enterprise servers that pick it then close the connection during authentication.
var ciphers = []string{
	"chacha20-poly1305@openssh.com",
	"aes128-ctr", "aes192-ctr", "aes256-ctr",
	"aes128-gcm@openssh.com", "aes256-gcm@openssh.com",
}

// Dial connects, checks the host key, authenticates and starts the SFTP subsystem.
// Failures are [remotefs.ErrHostKeyUnknown] or [remotefs.ErrHostKeyMismatch] for host keys,
// [remotefs.ErrAuth] for credentials, the context error when canceled, [netguard.ErrBlocked] for forbidden
// destinations and [remotefs.ErrConnectionLost] otherwise. Only a lost connection is worth retrying.
func Dial(ctx context.Context, cfg Config) (*FS, error) {
	sshClient, err := DialSSH(ctx, cfg)
	if err != nil {
		return nil, err
	}
	strict := cfg.InexactSizes || cfg.Strict.Has(cfg.Address)
	opts := []sftp.ClientOption{
		sftp.UseConcurrentReads(!strict), sftp.UseConcurrentWrites(true),
		sftp.MaxConcurrentRequestsPerFile(maxConcurrentRequests), sftp.MaxPacketChecked(maxPacket),
	}
	var client *sftp.Client
	if cfg.Sudo != nil {
		client, err = sudoClient(ctx, sshClient, *cfg.Sudo, cfg.Timeout, opts...)
	} else {
		client, err = sftp.NewClient(sshClient, opts...)
		if err != nil {
			err = fmt.Errorf("starting the sftp subsystem: %w: %w", remotefs.ErrConnectionLost, err)
		}
	}
	if err != nil {
		_ = sshClient.Close()
		return nil, err
	}
	f := &FS{ssh: sshClient, client: client, address: cfg.Address, known: cfg.Strict, elevated: cfg.Sudo != nil}
	f.strict.Store(strict)
	return f, nil
}

// DialSSH connects, checks the host key and authenticates, like [Dial], but stops short of starting the SFTP
// subsystem: it returns the bare SSH client. A jump host uses this to reach the site that tunnels through it, whose
// own [Dial] then runs entirely over the channel [ssh.Client.DialContext] opens on it, with cfg.Conn set to that
// channel instead of cfg.Address being dialed again.
//
// Failures are [remotefs.ErrHostKeyUnknown] or [remotefs.ErrHostKeyMismatch] for host keys, [remotefs.ErrAuth] for
// credentials, the context error when canceled, [netguard.ErrBlocked] for forbidden destinations and
// [remotefs.ErrConnectionLost] otherwise.
func DialSSH(ctx context.Context, cfg Config) (*ssh.Client, error) {
	if cfg.VerifyHostKey == nil {
		return nil, fmt.Errorf("%w: host key verification is required", remotefs.ErrInvalid)
	}
	methods, err := authMethods(cfg)
	if err != nil {
		return nil, err
	}

	var rejected error
	verified := false
	clientConfig := &ssh.ClientConfig{
		Config: ssh.Config{KeyExchanges: keyExchanges, Ciphers: ciphers},
		User:   cfg.Username,
		Auth:   methods,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			rejected = cfg.VerifyHostKey(key.Type(), ssh.FingerprintSHA256(key))
			verified = rejected == nil
			return rejected
		},
		HostKeyAlgorithms: cfg.HostKeyAlgorithms,
		Timeout:           cfg.Timeout,
	}

	conn := cfg.Conn
	if conn == nil {
		dial := cfg.DialContext
		if dial == nil {
			dial = (&net.Dialer{Timeout: cfg.Timeout}).DialContext
		}
		conn, err = dial(ctx, "tcp", cfg.Address)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if errors.Is(err, netguard.ErrBlocked) {
				return nil, fmt.Errorf("connecting to %s: %w", cfg.Address, err)
			}
			return nil, fmt.Errorf("connecting to %s: %w: %w", cfg.Address, remotefs.ErrConnectionLost, err)
		}
	}

	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	sshConn, channels, requests, err := ssh.NewClientConn(conn, cfg.Address, clientConfig)
	stop()
	if ctxErr := ctx.Err(); ctxErr != nil {
		_ = conn.Close()
		return nil, ctxErr
	}
	if err != nil {
		_ = conn.Close()
		return nil, handshakeError(cfg.Address, rejected, verified, err)
	}
	return ssh.NewClient(sshConn, channels, requests), nil
}

// refusedCredentials tells whether a failed handshake means the server did not accept the credentials. Besides the
// library's own message once every method failed, OpenSSH has two other ways of saying it: answering a keyboard-interactive
// attempt with a plain failure (message 51) where the library expects a question (message 60), and disconnecting after too
// many failed attempts.
func refusedCredentials(err error) bool {
	message := err.Error()
	return errors.Is(err, remotefs.ErrAuth) ||
		strings.Contains(message, "unable to authenticate") ||
		strings.Contains(message, "unexpected message type 51") ||
		strings.Contains(message, "Too many authentication failures")
}

// closedBy tells whether a handshake failed because the server closed the connection.
func closedBy(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed)
}

// handshakeError explains why the SSH handshake failed. A server that closes the connection once its key was verified, while the
// client signs in, refused the credentials: some gateways answer wrong passwords that way instead of with a failure, and taking it
// for a lost connection would try the same credentials again and again, which can lock the account.
func handshakeError(address string, rejected error, verified bool, err error) error {
	switch {
	case rejected != nil:
		return fmt.Errorf("verifying the host key of %s: %w", address, rejected)
	case refusedCredentials(err):
		return fmt.Errorf("%w: %s refused the credentials", remotefs.ErrAuth, address)
	case verified && closedBy(err):
		return fmt.Errorf("%w: %s closed the connection while signing in, as some servers do with wrong credentials", remotefs.ErrAuth, address)
	}
	return fmt.Errorf("handshake with %s: %w: %w", address, remotefs.ErrConnectionLost, err)
}

// guard fails when the file system is closed or the context ended.
func (f *FS) guard(ctx context.Context) error {
	if f.closed.Load() {
		return fmt.Errorf("%w: the file system is closed", remotefs.ErrConnectionLost)
	}
	return ctx.Err()
}

// wrap classifies err and names the operation and path.
func wrap(op, p string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s %s: %w", op, p, classify(err))
}

// pathState is what a path currently is on the server.
type pathState int

const (
	stateMissing pathState = iota
	stateFile
	stateDir
	stateBlocked
)

// state inspects a path, telling apart a missing one from one below a file, to explain generic failures.
func (f *FS) state(p string) pathState {
	if info, err := f.client.Stat(p); err == nil {
		if info.IsDir() {
			return stateDir
		}
		return stateFile
	}
	for dir := path.Dir(p); ; {
		if info, err := f.client.Stat(dir); err == nil {
			if info.IsDir() {
				return stateMissing
			}
			return stateBlocked
		}
		parent := path.Dir(dir)
		if parent == dir {
			return stateMissing
		}
		dir = parent
	}
}

// refinable reports whether err leaves room to explain it by looking at the path: the generic failure status,
// or "no such file", which OpenSSH also answers when a component of the path is a file instead of a directory.
func refinable(err error) bool {
	return isFailure(err) || errors.Is(err, fs.ErrNotExist)
}

// notADirectory reports whether p is a file, or lies below one.
func (f *FS) notADirectory(p string) bool {
	state := f.state(p)
	return state == stateFile || state == stateBlocked
}

// entryFrom converts file information to an entry of the directory dir.
func entryFrom(dir string, info fs.FileInfo) remotefs.Entry {
	return remotefs.Entry{
		Name: info.Name(), Path: path.Join(dir, info.Name()), Size: info.Size(), Mode: info.Mode(),
		ModTime: info.ModTime().UTC(), ModTimePrecision: remotefs.PrecisionSecond,
	}
}

// List implements [remotefs.RemoteFS].
func (f *FS) List(ctx context.Context, dir string) ([]remotefs.Entry, error) {
	if err := f.guard(ctx); err != nil {
		return nil, err
	}
	infos, err := f.client.ReadDirContext(ctx, dir)
	if err != nil && refinable(err) && f.notADirectory(dir) {
		return nil, fmt.Errorf("list %s: %w", dir, remotefs.ErrNotDirectory)
	}
	if err != nil {
		return nil, wrap("list", dir, err)
	}
	entries := make([]remotefs.Entry, 0, len(infos))
	for _, info := range infos {
		entries = append(entries, entryFrom(dir, info))
	}
	return entries, nil
}

// Stat implements [remotefs.RemoteFS].
func (f *FS) Stat(ctx context.Context, p string) (remotefs.Entry, error) {
	if err := f.guard(ctx); err != nil {
		return remotefs.Entry{}, err
	}
	info, err := f.client.Stat(p)
	if err != nil {
		return remotefs.Entry{}, wrap("stat", p, err)
	}
	return entryFrom(path.Dir(p), info), nil
}

// streamError classifies an error raised while transferring data. The end of a stream is not an error.
func streamError(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}
	return classify(err)
}

// sourceReader remembers the error of the reader it wraps, to tell it apart from errors of the connection.
type sourceReader struct {
	io.Reader
	err error
}

// Read reads from the wrapped reader and remembers a failure that is not the end of the stream.
func (s *sourceReader) Read(p []byte) (int, error) {
	n, err := s.Reader.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		s.err = err
	}
	return n, err
}

// sinkWriter remembers the error of the writer it wraps, to tell it apart from errors of the connection.
type sinkWriter struct {
	io.Writer
	err error
}

// Write writes to the wrapped writer and remembers a failure.
func (s *sinkWriter) Write(p []byte) (int, error) {
	n, err := s.Writer.Write(p)
	if err != nil {
		s.err = err
	}
	return n, err
}

// earlyDrop is how far into a file a pipelined read that loses the connection marks the server strict: strict servers drop the
// session at once, while a link that fails later is not the server's doing.
const earlyDrop = 1 << 20

// reader is the read side of a remote file. It keeps WriteTo, which reads concurrently, and reports
// connection failures as [remotefs.ErrConnectionLost].
type reader struct {
	file *sftp.File
	fs   *FS
}

// Read implements [io.Reader].
func (r reader) Read(p []byte) (int, error) {
	n, err := r.file.Read(p)
	return n, streamError(err)
}

// WriteTo implements [io.WriterTo]. A strict server is read one request at a time through [reader.Read], each as large as a reply
// can carry; others with several reads
// in flight, and losing the connection early in such a read marks the server strict. Errors of the destination are returned as
// they are.
func (r reader) WriteTo(w io.Writer) (int64, error) {
	if r.fs.strict.Load() {
		return io.CopyBuffer(w, struct{ io.Reader }{r}, make([]byte, bulkPacket))
	}
	sink := &sinkWriter{Writer: w}
	n, err := r.file.WriteTo(sink)
	if sink.err != nil && errors.Is(err, sink.err) {
		return n, err
	}
	err = streamError(err)
	if errors.Is(err, remotefs.ErrConnectionLost) && n < earlyDrop {
		r.fs.markStrict()
	}
	return n, err
}

// Close implements [io.Closer].
func (r reader) Close() error { return streamError(r.file.Close()) }

// writer is the write side of a remote file. It keeps ReadFrom, which writes concurrently, and reports
// connection failures as [remotefs.ErrConnectionLost].
type writer struct{ file *sftp.File }

// Write implements [io.Writer].
func (w writer) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)
	return n, streamError(err)
}

// ReadFrom implements [io.ReaderFrom]. Errors of the source are returned as they are.
func (w writer) ReadFrom(r io.Reader) (int64, error) {
	src := &sourceReader{Reader: r}
	n, err := w.file.ReadFrom(src)
	if src.err != nil && errors.Is(err, src.err) {
		return n, err
	}
	return n, streamError(err)
}

// Close implements [io.Closer].
func (w writer) Close() error { return streamError(w.file.Close()) }

// seekStart moves the offset of a remote file. It cannot fail: seeking from the start only stores the offset
// on the client, and the callers checked it against the size of the file.
func seekStart(file *sftp.File, offset int64) {
	_, _ = file.Seek(offset, io.SeekStart)
}

// OpenRead implements [remotefs.RemoteFS].
func (f *FS) OpenRead(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	entry, err := f.Stat(ctx, p)
	if err != nil {
		return nil, err
	}
	if entry.IsDir() || offset < 0 || offset > entry.Size {
		return nil, fmt.Errorf("read %s from %d: %w", p, offset, remotefs.ErrInvalid)
	}
	file, err := f.readClient().Open(p)
	if err != nil {
		return nil, wrap("read", p, err)
	}
	seekStart(file, offset)
	return reader{file: file, fs: f}, nil
}

// bulkReads turns the second channel of strict servers on; tests turn it off to measure what it saves.
var bulkReads = func() *atomic.Bool {
	on := &atomic.Bool{}
	on.Store(true)
	return on
}()

// readClient is the client files are read with: for a strict server, a second SFTP channel of the same connection whose reads ask
// for [bulkPacket] bytes each, opened on first use; otherwise, or when the server refuses that channel, or in sudo mode, the
// client of the session. Writes always go through the client of the session, whose packets every server takes.
func (f *FS) readClient() *sftp.Client {
	if !f.strict.Load() || f.elevated || !bulkReads.Load() {
		return f.client
	}
	f.bulkOnce.Do(func() {
		bulk, err := sftp.NewClient(f.ssh, sftp.UseConcurrentReads(false), sftp.MaxPacketUnchecked(bulkPacket))
		if err == nil {
			f.bulk = bulk
		}
	})
	if f.bulk == nil {
		return f.client
	}
	return f.bulk
}

// OpenWrite implements [remotefs.RemoteFS].
func (f *FS) OpenWrite(ctx context.Context, p string, offset int64, mode remotefs.WriteMode) (io.WriteCloser, error) {
	if err := f.guard(ctx); err != nil {
		return nil, err
	}
	var flags int
	switch mode {
	case remotefs.WriteCreate:
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	case remotefs.WriteOverwrite:
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case remotefs.WriteResume:
		return f.openResume(ctx, p, offset)
	default:
		return nil, fmt.Errorf("write %s: unknown write mode %d: %w", p, mode, remotefs.ErrInvalid)
	}
	file, err := f.client.OpenFile(p, flags)
	if err != nil && refinable(err) {
		switch f.state(p) {
		case stateFile:
			return nil, fmt.Errorf("write %s: %w", p, remotefs.ErrExists)
		case stateDir:
			if mode == remotefs.WriteCreate {
				return nil, fmt.Errorf("write %s: %w", p, remotefs.ErrExists)
			}
			return nil, fmt.Errorf("write %s: %w", p, remotefs.ErrInvalid)
		case stateBlocked:
			return nil, fmt.Errorf("write %s: %w", p, remotefs.ErrNotDirectory)
		}
	}
	if err != nil {
		return nil, wrap("write", p, err)
	}
	return writer{file}, nil
}

// openResume opens an existing file for writing from offset, without truncating it.
func (f *FS) openResume(ctx context.Context, p string, offset int64) (io.WriteCloser, error) {
	entry, err := f.Stat(ctx, p)
	if err != nil {
		return nil, err
	}
	if entry.IsDir() || offset < 0 || offset > entry.Size {
		return nil, fmt.Errorf("resume %s at %d: %w", p, offset, remotefs.ErrInvalid)
	}
	file, err := f.client.OpenFile(p, os.O_WRONLY)
	if err != nil {
		return nil, wrap("resume", p, err)
	}
	seekStart(file, offset)
	return writer{file}, nil
}

// Mkdir implements [remotefs.RemoteFS].
func (f *FS) Mkdir(ctx context.Context, p string) error {
	if err := f.guard(ctx); err != nil {
		return err
	}
	err := f.client.Mkdir(p)
	if err != nil && refinable(err) {
		switch f.state(p) {
		case stateFile, stateDir:
			return fmt.Errorf("mkdir %s: %w", p, remotefs.ErrExists)
		case stateBlocked:
			return fmt.Errorf("mkdir %s: %w", p, remotefs.ErrNotDirectory)
		}
	}
	return wrap("mkdir", p, err)
}

// isRoot reports whether p is the root directory.
func isRoot(p string) bool { return path.Clean(p) == "/" }

// Remove implements [remotefs.RemoteFS].
func (f *FS) Remove(ctx context.Context, p string) error {
	if err := f.guard(ctx); err != nil {
		return err
	}
	if isRoot(p) {
		return fmt.Errorf("remove %s: %w", p, remotefs.ErrPermission)
	}
	err := f.client.Remove(p)
	if isFailure(err) && f.state(p) == stateDir {
		return fmt.Errorf("remove %s: %w", p, remotefs.ErrNotEmpty)
	}
	return wrap("remove", p, err)
}

// RemoveAll implements [remotefs.RemoteFS]. Symbolic links are removed, never followed.
func (f *FS) RemoveAll(ctx context.Context, p string) error {
	if err := f.guard(ctx); err != nil {
		return err
	}
	if isRoot(p) {
		return fmt.Errorf("remove all %s: %w", p, remotefs.ErrPermission)
	}
	return f.removeTree(ctx, p)
}

// removeTree deletes p and everything below it, ignoring paths that do not exist.
func (f *FS) removeTree(ctx context.Context, p string) error {
	info, err := f.client.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return wrap("remove all", p, err)
	}
	if info.IsDir() {
		children, err := f.client.ReadDirContext(ctx, p)
		if err != nil {
			return wrap("remove all", p, err)
		}
		for _, child := range children {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := f.removeTree(ctx, path.Join(p, child.Name())); err != nil {
				return err
			}
		}
	}
	return f.Remove(ctx, p)
}

// Rename implements [remotefs.RemoteFS]. An existing destination is never replaced.
func (f *FS) Rename(ctx context.Context, from, to string) error {
	if err := f.guard(ctx); err != nil {
		return err
	}
	if strings.HasPrefix(path.Clean(to)+"/", path.Clean(from)+"/") {
		return fmt.Errorf("rename %s to %s: a path cannot move into itself: %w", from, to, remotefs.ErrInvalid)
	}
	err := f.client.Rename(from, to)
	if isFailure(err) && f.state(to) != stateMissing {
		return fmt.Errorf("rename %s to %s: %w", from, to, remotefs.ErrExists)
	}
	return wrap("rename", from, err)
}

// Chmod implements [remotefs.RemoteFS].
func (f *FS) Chmod(ctx context.Context, p string, mode fs.FileMode) error {
	if err := f.guard(ctx); err != nil {
		return err
	}
	return wrap("chmod", p, f.client.Chmod(p, mode))
}

// Chtimes implements [remotefs.RemoteFS].
func (f *FS) Chtimes(ctx context.Context, p string, mtime time.Time) error {
	if err := f.guard(ctx); err != nil {
		return err
	}
	return wrap("chtimes", p, f.client.Chtimes(p, mtime, mtime))
}

// Capabilities implements [remotefs.RemoteFS]: SFTP resumes writes and sets modes and times.
func (f *FS) Capabilities() remotefs.Capabilities {
	return remotefs.Capabilities{Resume: true, Rewrite: true, Chmod: true, Chtimes: true}
}

// Ping implements [remotefs.RemoteFS] with a round trip to the server.
func (f *FS) Ping(ctx context.Context) error {
	if err := f.guard(ctx); err != nil {
		return err
	}
	_, err := f.client.Getwd()
	return wrap("ping", "", err)
}

// Home implements [remotefs.Homer]: the folder the server opened the session in, as pwd tells it, which is the root only when the
// server answers something that is not an absolute path.
func (f *FS) Home(ctx context.Context) (string, error) {
	if err := f.guard(ctx); err != nil {
		return "", err
	}
	wd, err := f.client.Getwd()
	if err != nil {
		return "", wrap("home", "", err)
	}
	return remotefs.HomeOf(wd), nil
}

// Close implements [remotefs.RemoteFS]. It is safe to call more than once. Errors from tearing down a
// connection are not reported, since there is nothing a caller could do about them.
func (f *FS) Close() error {
	f.once.Do(func() {
		f.closed.Store(true)
		_ = f.client.Close()
		f.bulkOnce.Do(func() {})
		if f.bulk != nil {
			_ = f.bulk.Close()
		}
		_ = f.ssh.Close()
	})
	return nil
}

// SizesTrusted implements [remotefs.SizeChecker]: sizes are trusted until the server proves strict.
func (f *FS) SizesTrusted() bool { return !f.strict.Load() }

// NoteWholeRead implements [remotefs.SizeChecker]: a whole file whose bytes differ from the size the server reported marks it strict.
func (f *FS) NoteWholeRead(reported, read int64) {
	if read != reported {
		f.markStrict()
	}
}

// markStrict stops trusting the server's sizes and reading it with several requests in flight, on this connection and on the next.
func (f *FS) markStrict() {
	f.strict.Store(true)
	f.known.add(f.address)
}
