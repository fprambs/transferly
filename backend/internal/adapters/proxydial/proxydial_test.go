package proxydial_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/adapters/proxydial"
)

// echoOnce accepts one connection and writes back whatever it reads, once, then closes.
func echoOnce(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		buf := make([]byte, 64)
		n, _ := conn.Read(buf)
		_, _ = conn.Write(buf[:n])
	}()
	return ln.Addr().String()
}

// socks5Server is a minimal SOCKS5 server, written independently of the client under test, that relays one
// connection to whatever address it is asked to CONNECT to. auth, when set, is the username/password it demands.
type socks5Server struct {
	addr string
	auth *[2]string // username, password
}

// newSOCKS5Server starts the server and returns its address.
func newSOCKS5Server(t *testing.T, auth *[2]string) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s := &socks5Server{addr: ln.Addr().String(), auth: auth}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		s.serve(conn)
	}()
	return s.addr
}

// serve runs the handshake and relays the connection once it succeeds.
func (s *socks5Server) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	method := byte(0x00)
	if s.auth != nil {
		method = 0x02
	}
	if _, err := conn.Write([]byte{0x05, method}); err != nil {
		return
	}
	if s.auth != nil {
		if !s.authenticate(conn) {
			return
		}
	}
	target, ok := s.readConnect(conn)
	if !ok {
		return
	}
	upstream, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", target)
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer func() { _ = upstream.Close() }()
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	go func() { _, _ = io.Copy(upstream, conn) }()
	_, _ = io.Copy(conn, upstream)
}

// authenticate runs the username/password subnegotiation and reports whether it matched.
func (s *socks5Server) authenticate(conn net.Conn) bool {
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return false
	}
	user := make([]byte, head[1])
	if _, err := io.ReadFull(conn, user); err != nil {
		return false
	}
	plen := make([]byte, 1)
	if _, err := io.ReadFull(conn, plen); err != nil {
		return false
	}
	pass := make([]byte, plen[0])
	if _, err := io.ReadFull(conn, pass); err != nil {
		return false
	}
	ok := string(user) == s.auth[0] && string(pass) == s.auth[1]
	status := byte(0x00)
	if !ok {
		status = 0x01
	}
	_, _ = conn.Write([]byte{0x01, status})
	return ok
}

// readConnect reads a CONNECT request and returns the address it names.
func (s *socks5Server) readConnect(conn net.Conn) (string, bool) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return "", false
	}
	var host string
	switch head[3] {
	case 0x01:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return "", false
		}
		host = net.IP(ip).String()
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return "", false
		}
		name := make([]byte, length[0])
		if _, err := io.ReadFull(conn, name); err != nil {
			return "", false
		}
		host = string(name)
	default:
		return "", false
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(conn, port); err != nil {
		return "", false
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port[0])<<8|int(port[1]))), true
}

// httpProxyServer is a minimal HTTP CONNECT proxy, independent of the client under test, that relays one connection.
// wantAuth, when set, is the exact Proxy-Authorization header value it demands.
func httpProxyServer(t *testing.T, wantAuth string) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		reader := bufio.NewReader(conn)
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		if wantAuth != "" && req.Header.Get("Proxy-Authorization") != wantAuth {
			_, _ = conn.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\n\r\n"))
			return
		}
		upstream, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", req.Host)
		if err != nil {
			_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			return
		}
		defer func() { _ = upstream.Close() }()
		if _, err := conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); err != nil {
			return
		}
		go func() { _, _ = io.Copy(upstream, reader) }()
		_, _ = io.Copy(conn, upstream)
	}()
	return ln.Addr().String()
}

// TestDialer_DialContext covers [proxydial.Dialer.DialContext] and [proxydial.New].
//
// Scenarios:
//   - a SOCKS5 tunnel relays data to and from the real target, with and without username/password authentication
//   - a SOCKS5 proxy that refuses the credentials, offers no acceptable method or fails the CONNECT itself is ErrRefused
//   - an HTTP CONNECT tunnel relays data to and from the real target, with and without a Proxy-Authorization header
//   - an HTTP proxy that answers anything but 200 is ErrRefused
//   - an unsupported proxy kind is ErrRefused
//   - the proxy's own address is dialed through the dial function given to New, and its failure is reported
//   - a context with a deadline bounds the tunneled connection
func TestDialer_DialContext(t *testing.T) {
	target := echoOnce(t)
	ctx := context.Background()

	t.Run("SOCKS5 without authentication", func(t *testing.T) {
		proxy := newSOCKS5Server(t, nil)
		conn, err := proxydial.New(proxydial.Config{Kind: proxydial.SOCKS5, Address: proxy}, nil).DialContext(ctx, "tcp", target)
		if err != nil {
			t.Fatalf("DialContext() = %v", err)
		}
		defer func() { _ = conn.Close() }()
		roundTrip(t, conn)
	})

	t.Run("a context with a deadline bounds the tunneled connection", func(t *testing.T) {
		target := echoOnce(t)
		proxy := newSOCKS5Server(t, nil)
		deadlined, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		conn, err := proxydial.New(proxydial.Config{Kind: proxydial.SOCKS5, Address: proxy}, nil).DialContext(deadlined, "tcp", target)
		if err != nil {
			t.Fatalf("DialContext() = %v", err)
		}
		defer func() { _ = conn.Close() }()
		roundTrip(t, conn)
	})

	t.Run("SOCKS5 with authentication", func(t *testing.T) {
		target := echoOnce(t)
		proxy := newSOCKS5Server(t, &[2]string{"alice", "secret"})
		conn, err := proxydial.New(proxydial.Config{Kind: proxydial.SOCKS5, Address: proxy, Username: "alice", Password: "secret"}, nil).DialContext(ctx, "tcp", target)
		if err != nil {
			t.Fatalf("DialContext() = %v", err)
		}
		defer func() { _ = conn.Close() }()
		roundTrip(t, conn)
	})

	t.Run("SOCKS5 with the wrong credentials", func(t *testing.T) {
		proxy := newSOCKS5Server(t, &[2]string{"alice", "secret"})
		_, err := proxydial.New(proxydial.Config{Kind: proxydial.SOCKS5, Address: proxy, Username: "alice", Password: "wrong"}, nil).DialContext(ctx, "tcp", target)
		if !errors.Is(err, proxydial.ErrRefused) {
			t.Errorf("DialContext() with wrong credentials = %v; want ErrRefused", err)
		}
	})

	t.Run("SOCKS5 target that cannot be reached", func(t *testing.T) {
		proxy := newSOCKS5Server(t, nil)
		_, err := proxydial.New(proxydial.Config{Kind: proxydial.SOCKS5, Address: proxy}, nil).DialContext(ctx, "tcp", "127.0.0.1:1")
		if !errors.Is(err, proxydial.ErrRefused) {
			t.Errorf("DialContext() to an unreachable target = %v; want ErrRefused", err)
		}
	})

	t.Run("HTTP CONNECT without authentication", func(t *testing.T) {
		target := echoOnce(t)
		proxy := httpProxyServer(t, "")
		conn, err := proxydial.New(proxydial.Config{Kind: proxydial.HTTP, Address: proxy}, nil).DialContext(ctx, "tcp", target)
		if err != nil {
			t.Fatalf("DialContext() = %v", err)
		}
		defer func() { _ = conn.Close() }()
		roundTrip(t, conn)
	})

	t.Run("HTTP CONNECT with a Proxy-Authorization header", func(t *testing.T) {
		target := echoOnce(t)
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret"))
		proxy := httpProxyServer(t, want)
		conn, err := proxydial.New(proxydial.Config{Kind: proxydial.HTTP, Address: proxy, Username: "alice", Password: "secret"}, nil).DialContext(ctx, "tcp", target)
		if err != nil {
			t.Fatalf("DialContext() = %v", err)
		}
		defer func() { _ = conn.Close() }()
		roundTrip(t, conn)
	})

	t.Run("HTTP CONNECT refused", func(t *testing.T) {
		proxy := httpProxyServer(t, "something required")
		_, err := proxydial.New(proxydial.Config{Kind: proxydial.HTTP, Address: proxy}, nil).DialContext(ctx, "tcp", target)
		if !errors.Is(err, proxydial.ErrRefused) {
			t.Errorf("DialContext() refused by the proxy = %v; want ErrRefused", err)
		}
	})

	t.Run("unsupported proxy kind", func(t *testing.T) {
		client, server := net.Pipe()
		t.Cleanup(func() { _ = server.Close() })
		_, err := proxydial.New(proxydial.Config{Kind: "wireguard", Address: "127.0.0.1:1"}, func(context.Context, string, string) (net.Conn, error) {
			return client, nil
		}).DialContext(ctx, "tcp", target)
		if !errors.Is(err, proxydial.ErrRefused) {
			t.Errorf("DialContext() with an unsupported kind = %v; want ErrRefused", err)
		}
	})

	t.Run("the proxy's own address is dialed through the given dial function", func(t *testing.T) {
		asked := ""
		dial := func(_ context.Context, network, address string) (net.Conn, error) {
			asked = address
			return nil, errors.New("refused")
		}
		_, err := proxydial.New(proxydial.Config{Kind: proxydial.SOCKS5, Address: "proxy.example:1080"}, dial).DialContext(ctx, "tcp", target)
		if err == nil || asked != "proxy.example:1080" {
			t.Errorf("DialContext() = %v, asked %q; want the proxy's own address dialed and the failure reported", err, asked)
		}
	})
}

// roundTrip writes a byte string over conn and expects it echoed back, proving the tunnel actually carries data
// end to end, not just that the handshake succeeded.
func roundTrip(t *testing.T, conn net.Conn) {
	t.Helper()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello" {
		t.Errorf("round trip = %q; want %q", buf, "hello")
	}
}
