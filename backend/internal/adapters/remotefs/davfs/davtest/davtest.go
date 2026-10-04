// Package davtest runs a WebDAV server for tests (ADR-0026): golang.org/x/net/webdav on a file system in memory, under a root
// path, with HTTP Basic authentication and failures that can be injected.
package davtest

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"

	"golang.org/x/net/webdav"
)

// The credentials and the root path of every server.
const (
	Username = "transferly"
	Password = "transferly-dav"
	// Root is the path the files are served under, as /remote.php/dav/files/{user}/ is on Nextcloud.
	Root = "/dav/files/transferly/"
)

// Options configure a server.
type Options struct {
	// Fault returns the HTTP status to answer a request with, or zero to serve it.
	Fault func(r *http.Request) int
	// Anonymous serves without authentication, as a public share does.
	Anonymous bool
	// IgnoreRanges answers reads whole, as servers that do not support ranges do.
	IgnoreRanges bool
}

// Server is a running WebDAV server.
type Server struct {
	// URL is the origin it listens at; the root of the files is URL followed by [Root].
	URL string

	opt     Options
	fs      webdav.FileSystem
	handler http.Handler
}

// New returns a server that [Server.Handler] serves, not listening yet.
func New(opt Options) *Server {
	s := &Server{opt: opt, fs: webdav.NewMemFS()}
	dav := &webdav.Handler{Prefix: strings.TrimSuffix(Root, "/"), FileSystem: s.fs, LockSystem: webdav.NewMemLS()}
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opt.Fault != nil {
			if status := s.opt.Fault(r); status != 0 {
				w.WriteHeader(status)
				return
			}
		}
		if !s.opt.Anonymous {
			user, password, ok := r.BasicAuth()
			if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(Username)) != 1 || subtle.ConstantTimeCompare([]byte(password), []byte(Password)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="transferly"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		if s.opt.IgnoreRanges {
			r.Header.Del("Range")
		}
		dav.ServeHTTP(w, r)
	})
	return s
}

// Start runs a server until the test ends.
func Start(t testing.TB, opt Options) *Server {
	t.Helper()
	s := New(opt)
	server := httptest.NewServer(s.handler)
	t.Cleanup(server.Close)
	s.URL = server.URL
	return s
}

// Handler serves the server.
func (s *Server) Handler() http.Handler { return s.handler }

// Endpoint returns the URL of the root of the files.
func (s *Server) Endpoint() string { return s.URL + Root }

// Put stores a file directly, as another program would, at a path below the root; its folders must exist.
func (s *Server) Put(name string, data []byte) error {
	f, err := s.fs.OpenFile(context.Background(), path.Clean("/"+name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Mkdir makes a folder directly, at a path below the root, with the folders above it.
func (s *Server) Mkdir(name string) error {
	p := "/"
	for _, part := range strings.Split(strings.Trim(path.Clean("/"+name), "/"), "/") {
		p = path.Join(p, part)
		if err := s.fs.Mkdir(context.Background(), p, 0o755); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}
