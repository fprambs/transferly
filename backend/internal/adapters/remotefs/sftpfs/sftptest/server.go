// Package sftptest is an SFTP server for tests. It serves a temporary directory over SSH in the test process,
// answers errors with the status codes OpenSSH uses, and can inject faults, so adapters are tested without Docker.
package sftptest

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Options configure a test server.
type Options struct {
	// Users maps user names to passwords accepted by password and keyboard-interactive authentication.
	Users map[string]string
	// Keys maps user names to the public keys accepted for public key authentication.
	Keys map[string][]ssh.PublicKey
	// HostKey is the server identity; a new ed25519 key is generated when nil.
	HostKey ssh.Signer
	// KeyboardOnly disables password authentication, leaving keyboard-interactive.
	KeyboardOnly bool
	// NoSFTP makes the server refuse the sftp subsystem.
	NoSFTP bool
	// Fault returns an error to answer an operation with instead of performing it; nil performs it.
	// The method is the SFTP request method, such as Get, Put, List, Mkdir or Rename.
	Fault func(method, path string) error
}

// Server is a running test server.
type Server struct {
	// Addr is the host:port the server listens on.
	Addr string
	// HostKey is the public key the server presents.
	HostKey ssh.PublicKey
	// Root is the directory served as "/".
	Root string

	listener net.Listener
	mu       sync.Mutex
	conns    []net.Conn
	closed   bool
}

// NewSigner returns a new ed25519 signer.
func NewSigner(t testing.TB) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// Start runs a server that stops when the test ends.
func Start(t testing.TB, opt Options) *Server {
	t.Helper()
	hostKey := opt.HostKey
	if hostKey == nil {
		hostKey = NewSigner(t)
	}
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if want, ok := opt.Users[c.User()]; ok && want == string(pw) {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		},
		KeyboardInteractiveCallback: func(c ssh.ConnMetadata, ask ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := ask(c.User(), "", []string{"Password: "}, []bool{false})
			if want, ok := opt.Users[c.User()]; err == nil && ok && len(answers) == 1 && answers[0] == want {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			for _, allowed := range opt.Keys[c.User()] {
				if string(allowed.Marshal()) == string(key.Marshal()) {
					return nil, nil
				}
			}
			return nil, errors.New("unknown key")
		},
	}
	if opt.KeyboardOnly {
		config.PasswordCallback = nil
	}
	config.AddHostKey(hostKey)

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Addr: listener.Addr().String(), HostKey: hostKey.PublicKey(), Root: t.TempDir(), listener: listener}
	t.Cleanup(s.close)
	go s.accept(config, opt)
	return s
}

// close stops the server and every connection.
func (s *Server) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	_ = s.listener.Close()
	s.DropConnections()
}

// DropConnections closes every open connection at once, like a network failure. New connections are still accepted.
func (s *Server) DropConnections() {
	s.mu.Lock()
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// accept serves connections until the listener closes.
func (s *Server) accept(config *ssh.ServerConfig, opt Options) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.conns = append(s.conns, conn)
		s.mu.Unlock()
		go s.serve(conn, config, opt)
	}
}

// serve performs the SSH handshake and serves the sftp subsystem of every session channel.
func (s *Server) serve(conn net.Conn, config *ssh.ServerConfig, opt Options) {
	defer func() { _ = conn.Close() }()
	_, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		channel, reqs, err := newChannel.Accept()
		if err != nil {
			return
		}
		go s.session(channel, reqs, opt)
	}
}

// session answers subsystem requests of a channel and runs the SFTP server on sftp ones.
func (s *Server) session(channel ssh.Channel, requests <-chan *ssh.Request, opt Options) {
	defer func() { _ = channel.Close() }()
	for req := range requests {
		if opt.NoSFTP || req.Type != "subsystem" || len(req.Payload) < 4 || string(req.Payload[4:]) != "sftp" {
			_ = req.Reply(false, nil)
			continue
		}
		_ = req.Reply(true, nil)
		h := &handlers{root: s.Root, fault: opt.Fault}
		server := sftp.NewRequestServer(channel, sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h})
		_ = server.Serve()
		return
	}
}

// handlers serves the SFTP requests from a directory.
type handlers struct {
	root  string
	fault func(method, path string) error
}

// real maps a client path to a path below the root, never outside it.
func (h *handlers) real(p string) string {
	return filepath.Join(h.root, filepath.FromSlash(path.Clean("/"+p)))
}

// injected returns the fault configured for the request, if any.
func (h *handlers) injected(r *sftp.Request) error {
	if h.fault == nil {
		return nil
	}
	return h.fault(r.Method, r.Filepath)
}

// wire converts an operating system error to what OpenSSH answers: a path component that is a file instead of a
// directory reads as "no such file".
func wire(err error) error {
	if errors.Is(err, syscall.ENOTDIR) {
		return &os.PathError{Op: "open", Path: "/", Err: os.ErrNotExist}
	}
	return err
}

// Fileread opens a file for reading.
func (h *handlers) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	if err := h.injected(r); err != nil {
		return nil, err
	}
	file, err := os.Open(h.real(r.Filepath))
	return file, wire(err)
}

// Filewrite opens a file for writing with the flags of the request.
func (h *handlers) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	if err := h.injected(r); err != nil {
		return nil, err
	}
	flags := os.O_WRONLY
	pflags := r.Pflags()
	if pflags.Creat {
		flags |= os.O_CREATE
	}
	if pflags.Excl {
		flags |= os.O_EXCL
	}
	if pflags.Trunc {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(h.real(r.Filepath), flags, 0o644)
	return file, wire(err)
}

// Filecmd runs commands that change the file system. Renames never overwrite, as in SFTP version 3.
func (h *handlers) Filecmd(r *sftp.Request) error {
	if err := h.injected(r); err != nil {
		return err
	}
	target := h.real(r.Filepath)
	switch r.Method {
	case "Setstat":
		attrs, flags := r.Attributes(), r.AttrFlags()
		if flags.Permissions {
			if err := os.Chmod(target, attrs.FileMode().Perm()); err != nil {
				return err
			}
		}
		if flags.Acmodtime {
			return os.Chtimes(target, attrs.AccessTime(), attrs.ModTime())
		}
		return nil
	case "Rename":
		if _, err := os.Lstat(h.real(r.Target)); err == nil {
			return os.ErrExist
		}
		return wire(os.Rename(target, h.real(r.Target)))
	case "Rmdir":
		return h.removeIf(target, true)
	case "Remove":
		return h.removeIf(target, false)
	case "Mkdir":
		return wire(os.Mkdir(target, 0o755))
	}
	return sftp.ErrSSHFxOpUnsupported
}

// PosixRename renames and replaces an existing destination, like the posix-rename extension of OpenSSH.
func (h *handlers) PosixRename(r *sftp.Request) error {
	if err := h.injected(r); err != nil {
		return err
	}
	return os.Rename(h.real(r.Filepath), h.real(r.Target))
}

// removeIf removes a directory (wantDir) or a file, failing like a real server when the kind does not match.
func (h *handlers) removeIf(target string, wantDir bool) error {
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if info.IsDir() != wantDir {
		if wantDir {
			return &os.PathError{Op: "rmdir", Path: target, Err: syscall.ENOTDIR}
		}
		return &os.PathError{Op: "unlink", Path: target, Err: syscall.EISDIR}
	}
	return os.Remove(target)
}

// lister serves a fixed list of entries.
type lister []os.FileInfo

// ListAt copies entries from offset.
func (l lister) ListAt(out []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(out, l[offset:])
	if n < len(out) {
		return n, io.EOF
	}
	return n, nil
}

// Filelist lists a directory or describes one path.
func (h *handlers) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	if err := h.injected(r); err != nil {
		return nil, err
	}
	target := h.real(r.Filepath)
	switch r.Method {
	case "List":
		entries, err := os.ReadDir(target)
		if err != nil {
			return nil, wire(err)
		}
		infos := make(lister, 0, len(entries))
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				return nil, err
			}
			infos = append(infos, info)
		}
		return infos, nil
	case "Stat":
		info, err := os.Stat(target)
		if err != nil {
			return nil, wire(err)
		}
		return lister{info}, nil
	}
	return nil, sftp.ErrSSHFxOpUnsupported
}

// Lstat describes one path without following a final symbolic link.
func (h *handlers) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	if err := h.injected(r); err != nil {
		return nil, err
	}
	info, err := os.Lstat(h.real(r.Filepath))
	if err != nil {
		return nil, wire(err)
	}
	return lister{info}, nil
}
