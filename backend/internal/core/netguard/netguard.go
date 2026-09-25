// Package netguard decides which network addresses the server may connect to on behalf of users,
// so a saved site cannot be used to reach the server's own network, the cloud metadata service or
// other internal services (server-side request forgery).
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"time"
)

// ErrBlocked means the address is not one the server may connect to.
var ErrBlocked = errors.New("destination address is not allowed")

// ErrUnresolvable means the host name did not resolve to any address.
var ErrUnresolvable = errors.New("host did not resolve")

// blocked lists the special-purpose ranges that are refused unless explicitly allowed.
var blocked = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
	"192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
	"224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "64:ff9b::/96", "100::/64", "2001:db8::/32", "fc00::/7", "fe80::/10", "ff00::/8",
)

// mustPrefixes parses constant prefixes and panics on a typo, which the tests would catch.
func mustPrefixes(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		out[i] = netip.MustParsePrefix(c)
	}
	return out
}

// Resolver looks up host names, as [net.Resolver] does.
type Resolver interface {
	// LookupNetIP returns the addresses of host for the network ("ip", "ip4" or "ip6").
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Policy is the set of destinations the server may reach: every public address plus the
// networks the administrator allowed explicitly.
type Policy struct {
	allowed  []netip.Prefix
	resolver Resolver
}

// NewPolicy returns a policy that refuses private, loopback, link-local and other special-purpose
// addresses except those inside allowed. A nil resolver uses the system resolver.
func NewPolicy(allowed []netip.Prefix, resolver Resolver) *Policy {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &Policy{allowed: allowed, resolver: resolver}
}

// CheckIP returns [ErrBlocked] unless the address is public or inside an allowed network.
// IPv4-mapped IPv6 addresses are judged as the IPv4 address they carry.
func (p *Policy) CheckIP(ip netip.Addr) error {
	ip = ip.Unmap().WithZone("")
	for _, prefix := range p.allowed {
		if prefix.Contains(ip) {
			return nil
		}
	}
	for _, prefix := range blocked {
		if prefix.Contains(ip) {
			return fmt.Errorf("%w: %s", ErrBlocked, ip)
		}
	}
	return nil
}

// CheckHost verifies a host name or address before a site is saved. Every address the name resolves
// to must be allowed. It is a early answer for the user; [Policy.Control] is the check that counts,
// because the name can resolve differently later.
func (p *Policy) CheckHost(ctx context.Context, host string) error {
	if ip, err := netip.ParseAddr(host); err == nil {
		return p.CheckIP(ip)
	}
	ips, err := p.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("%w: %s", ErrUnresolvable, host)
	}
	for _, ip := range ips {
		if err := p.CheckIP(ip); err != nil {
			return err
		}
	}
	return nil
}

// Control is a [net.Dialer] Control function. It runs after the name was resolved and before the
// socket connects, so it judges the address actually used and defeats DNS rebinding.
func (p *Policy) Control(_, address string, _ syscall.RawConn) error {
	addrPort, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBlocked, address)
	}
	return p.CheckIP(addrPort.Addr())
}

// Dialer returns a dialer that refuses forbidden destinations, including those reached by redirects
// or by names that change their answer between checks.
func (p *Policy) Dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: p.Control}
}
