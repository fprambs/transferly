// Package proxydial tunnels a connection through a SOCKS5 or HTTP CONNECT proxy, in place of a direct TCP
// connection. It sits below the protocol adapters (sftpfs, ftpfs): once the tunnel is open, everything above it —
// TLS, the SSH handshake, authentication — runs over it exactly as it would over a raw socket.
package proxydial

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// Kind selects the proxy protocol.
type Kind string

const (
	// SOCKS5 tunnels through a SOCKS5 proxy (RFC 1928), with username/password authentication (RFC 1929) when a
	// username is given.
	SOCKS5 Kind = "socks5"
	// HTTP tunnels through an HTTP proxy with the CONNECT method (RFC 9110 §9.3.6).
	HTTP Kind = "http"
)

// Config describes a proxy to tunnel a connection through.
type Config struct {
	// Kind selects the proxy protocol.
	Kind Kind
	// Address is the host:port of the proxy.
	Address string
	// Username authenticates to the proxy; empty means no authentication.
	Username string
	// Password authenticates to the proxy, with Username.
	Password string
}

// ErrRefused means the proxy refused the tunnel, or answered in a way this client does not understand.
var ErrRefused = errors.New("proxy refused the connection")

// Dialer builds a [Dial] function tunneled through a proxy.
type Dialer struct {
	cfg  Config
	dial func(ctx context.Context, network, address string) (net.Conn, error)
}

// New returns a [Dialer] that reaches address through cfg's proxy, dialing the proxy itself with dial (nil uses a
// plain [net.Dialer]). Give the network guard's own dialer here, so the proxy's address is checked exactly like a
// site's host is.
func New(cfg Config, dial func(ctx context.Context, network, address string) (net.Conn, error)) *Dialer {
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	return &Dialer{cfg: cfg, dial: dial}
}

// DialContext connects to the proxy and asks it to tunnel to address, returning the tunneled connection. network is
// always "tcp": a proxy has no notion of UDP or Unix sockets here.
func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := d.dial(ctx, network, d.cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("connecting to the proxy at %s: %w", d.cfg.Address, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	switch d.cfg.Kind {
	case SOCKS5:
		err = socks5Connect(conn, d.cfg, address)
	case HTTP:
		err = httpConnect(ctx, conn, d.cfg, address)
	default:
		err = fmt.Errorf("%w: unsupported proxy kind %q", ErrRefused, d.cfg.Kind)
	}
	if !stop() {
		_ = conn.Close()
		return nil, ctx.Err()
	}
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("tunneling to %s through %s: %w", address, d.cfg.Address, err)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}
