package proxydial

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// TestSOCKS5_EdgeCases covers the SOCKS5 handshake edge cases that a full round trip through a real server rarely
// exercises: the reply codes named in socks5ReplyText, a bound address the server describes as IPv6 or with an
// unknown type, an unexpected version byte, no acceptable authentication method, an unknown authentication method,
// and credentials too long for the protocol to carry.
//
// Scenarios:
//   - every named SOCKS5 reply code, and an unknown one, are turned into their own text
//   - discarding the bound address of the reply supports IPv4, IPv6 and domain names, and refuses an unknown type
//   - a server answering a version other than 5 is refused, so is one offering no acceptable method or an unknown one
//   - a username or password longer than SOCKS5 can carry is refused before anything is sent
func TestSOCKS5_EdgeCases(t *testing.T) {
	for code, want := range map[byte]string{
		0x01: "general SOCKS server failure",
		0x02: "connection not allowed by ruleset",
		0x03: "network unreachable",
		0x04: "host unreachable",
		0x05: "connection refused",
		0x06: "TTL expired",
		0x07: "command not supported",
		0x08: "address type not supported",
		0x09: "unknown reply code 0x9",
	} {
		if got := socks5ReplyText(code); got != want {
			t.Errorf("socks5ReplyText(%#x) = %q; want %q", code, got, want)
		}
	}

	t.Run("discards an IPv4 bound address", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		go func() { _, _ = server.Write([]byte{1, 2, 3, 4, 0, 80}); _ = server.Close() }()
		if err := socks5DiscardBoundAddress(client, socks5AddrIPv4); err != nil {
			t.Errorf("socks5DiscardBoundAddress(IPv4) = %v", err)
		}
	})

	t.Run("discards an IPv6 bound address", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		go func() { _, _ = server.Write(make([]byte, 18)); _ = server.Close() }()
		if err := socks5DiscardBoundAddress(client, socks5AddrIPv6); err != nil {
			t.Errorf("socks5DiscardBoundAddress(IPv6) = %v", err)
		}
	})

	t.Run("refuses an unknown bound address type", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close(); _ = server.Close() }()
		if err := socks5DiscardBoundAddress(client, 0x7f); !errors.Is(err, ErrRefused) {
			t.Errorf("socks5DiscardBoundAddress(unknown) = %v; want ErrRefused", err)
		}
	})

	t.Run("a closed connection fails the greeting write", func(t *testing.T) {
		client, _ := net.Pipe()
		_ = client.Close()
		if err := socks5Connect(client, Config{}, "x:1"); err == nil {
			t.Error("socks5Connect() over a closed connection = <nil>; want an error")
		}
	})

	t.Run("no reply to the greeting fails the read", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		go func() { _, _ = server.Read(make([]byte, 8)); _ = server.Close() }()
		if err := socks5Connect(client, Config{}, "x:1"); err == nil {
			t.Error("socks5Connect() with no reply to the greeting = <nil>; want an error")
		}
	})

	t.Run("a server that answers the wrong version is refused", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		go func() {
			_, _ = server.Read(make([]byte, 8))
			_, _ = server.Write([]byte{0x04, 0x00})
			_ = server.Close()
		}()
		if err := socks5Connect(client, Config{}, "x:1"); !errors.Is(err, ErrRefused) {
			t.Errorf("socks5Connect() with the wrong version = %v; want ErrRefused", err)
		}
	})

	t.Run("a server offering no acceptable method is refused", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		go func() {
			_, _ = server.Read(make([]byte, 8))
			_, _ = server.Write([]byte{0x05, socks5NoAcceptableAuth})
			_ = server.Close()
		}()
		if err := socks5Connect(client, Config{}, "x:1"); !errors.Is(err, ErrRefused) {
			t.Errorf("socks5Connect() with no acceptable method = %v; want ErrRefused", err)
		}
	})

	t.Run("a server offering an unknown method is refused", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		go func() {
			_, _ = server.Read(make([]byte, 8))
			_, _ = server.Write([]byte{0x05, 0x03})
			_ = server.Close()
		}()
		if err := socks5Connect(client, Config{}, "x:1"); !errors.Is(err, ErrRefused) {
			t.Errorf("socks5Connect() with an unknown method = %v; want ErrRefused", err)
		}
	})

	t.Run("a username too long for SOCKS5 is refused before anything is sent", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close(); _ = server.Close() }()
		cfg := Config{Username: strings.Repeat("u", 256), Password: "p"}
		if err := socks5Authenticate(client, cfg); !errors.Is(err, ErrRefused) {
			t.Errorf("socks5Authenticate() with a long username = %v; want ErrRefused", err)
		}
	})

	t.Run("a host too long for SOCKS5 is refused before anything is sent", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close(); _ = server.Close() }()
		if err := socks5RequestConnect(client, strings.Repeat("h", 256)+":1"); !errors.Is(err, ErrRefused) {
			t.Errorf("socks5RequestConnect() with a long host = %v; want ErrRefused", err)
		}
	})

	t.Run("an IPv6 target is sent with the IPv6 address type", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		done := make(chan []byte, 1)
		go func() {
			buf := make([]byte, 32)
			n, _ := server.Read(buf)
			done <- buf[:n]
			_, _ = server.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		}()
		if err := socks5RequestConnect(client, "[::1]:22"); err != nil {
			t.Fatalf("socks5RequestConnect() = %v", err)
		}
		if got := <-done; got[3] != socks5AddrIPv6 {
			t.Errorf("address type sent for an IPv6 host = %#x; want %#x", got[3], socks5AddrIPv6)
		}
	})

	t.Run("an invalid target address is refused", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close(); _ = server.Close() }()
		if err := socks5RequestConnect(client, "not-a-host-port"); !errors.Is(err, ErrRefused) {
			t.Errorf("socks5RequestConnect() with an invalid address = %v; want ErrRefused", err)
		}
	})

	t.Run("an invalid port is refused", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close(); _ = server.Close() }()
		if err := socks5RequestConnect(client, "host:notaport"); !errors.Is(err, ErrRefused) {
			t.Errorf("socks5RequestConnect() with an invalid port = %v; want ErrRefused", err)
		}
	})

	t.Run("a closed connection fails the CONNECT write", func(t *testing.T) {
		client, _ := net.Pipe()
		_ = client.Close()
		if err := socks5RequestConnect(client, "host:1"); err == nil {
			t.Error("socks5RequestConnect() over a closed connection = <nil>; want an error")
		}
	})

	t.Run("no reply to the CONNECT request fails the read", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		go func() { _, _ = server.Read(make([]byte, 64)); _ = server.Close() }()
		if err := socks5RequestConnect(client, "host:1"); err == nil {
			t.Error("socks5RequestConnect() with no reply = <nil>; want an error")
		}
	})

	t.Run("a CONNECT reply of the wrong version is refused", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		go func() {
			_, _ = server.Read(make([]byte, 64))
			_, _ = server.Write([]byte{0x04, 0x00, 0x00, 0x01})
			_ = server.Close()
		}()
		if err := socks5RequestConnect(client, "host:1"); !errors.Is(err, ErrRefused) {
			t.Errorf("socks5RequestConnect() with a reply of the wrong version = %v; want ErrRefused", err)
		}
	})

	t.Run("a domain name bound address is discarded", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		go func() { _, _ = server.Write(append([]byte{4}, []byte("host\x00\x50")...)); _ = server.Close() }()
		if err := socks5DiscardBoundAddress(client, socks5AddrDomain); err != nil {
			t.Errorf("socks5DiscardBoundAddress(domain) = %v", err)
		}
	})

	t.Run("no length byte for a domain bound address fails the read", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		_ = server.Close()
		if err := socks5DiscardBoundAddress(client, socks5AddrDomain); err == nil {
			t.Error("socks5DiscardBoundAddress(domain) with nothing to read = <nil>; want an error")
		}
	})

	t.Run("a closed connection fails the authentication write", func(t *testing.T) {
		client, _ := net.Pipe()
		_ = client.Close()
		if err := socks5Authenticate(client, Config{Username: "a", Password: "b"}); err == nil {
			t.Error("socks5Authenticate() over a closed connection = <nil>; want an error")
		}
	})

	t.Run("no reply to the authentication request fails the read", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		go func() { _, _ = server.Read(make([]byte, 64)); _ = server.Close() }()
		if err := socks5Authenticate(client, Config{Username: "a", Password: "b"}); err == nil {
			t.Error("socks5Authenticate() with no reply = <nil>; want an error")
		}
	})
}

// TestHTTPConnect_EdgeCases verifies the failures httpConnect reports before or instead of a clean 200.
//
// Scenarios:
//   - a proxy that answers with something other than a valid HTTP response is ErrRefused
//   - writing the request over an already closed connection fails
func TestHTTPConnect_EdgeCases(t *testing.T) {
	t.Run("garbage instead of an HTTP response", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		go func() {
			_, _ = server.Read(make([]byte, 4096))
			_, _ = server.Write([]byte("not http\r\n\r\n"))
			_ = server.Close()
		}()
		if err := httpConnect(context.Background(), client, Config{}, "x:1"); !errors.Is(err, ErrRefused) {
			t.Errorf("httpConnect() with garbage = %v; want ErrRefused", err)
		}
	})

	t.Run("a closed connection fails the write", func(t *testing.T) {
		client, _ := net.Pipe()
		_ = client.Close()
		if err := httpConnect(context.Background(), client, Config{}, "x:1"); err == nil {
			t.Error("httpConnect() over a closed connection = <nil>; want an error")
		}
	})

	t.Run("an address that cannot become a request is refused before anything is sent", func(t *testing.T) {
		if err := httpConnect(context.Background(), nil, Config{}, "%zz"); !errors.Is(err, ErrRefused) {
			t.Errorf("httpConnect() with an unparsable address = %v; want ErrRefused", err)
		}
	})
}

// TestDialer_ContextCanceledDuringHandshake verifies that canceling the context while the handshake with the proxy
// is in flight stops the dial with the context's own error, not a raw connection failure.
func TestDialer_ContextCanceledDuringHandshake(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = server.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	dial := func(context.Context, string, string) (net.Conn, error) { return client, nil }
	go func() {
		buf := make([]byte, 8)
		_, _ = server.Read(buf)
		cancel()
	}()
	_, err := New(Config{Kind: SOCKS5, Address: "proxy:1080"}, dial).DialContext(ctx, "tcp", "x:1")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("DialContext() canceled mid handshake = %v; want context.Canceled", err)
	}
}
