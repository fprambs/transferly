package netguard_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/core/netguard"
)

// fakeResolver answers lookups from a fixed table.
type fakeResolver struct{ answers map[string][]netip.Addr }

// LookupNetIP returns the configured answer, or an error for unknown hosts.
func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	answer, ok := f.answers[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	return answer, nil
}

// TestPolicy_CheckIP verifies the address classification.
//
// Scenarios:
//   - public IPv4 and IPv6 addresses are allowed
//   - loopback, private, CGNAT, link-local (including the cloud metadata address), unspecified, multicast,
//     reserved and unique-local addresses are refused, in IPv4 and IPv6
//   - IPv4-mapped IPv6 addresses and zoned addresses are judged as the address they carry
//   - an explicitly allowed network overrides the refusal, and only for its own addresses
func TestPolicy_CheckIP(t *testing.T) {
	p := netguard.NewPolicy(nil, nil)
	for _, ok := range []string{"8.8.8.8", "93.184.216.34", "2606:4700:4700::1111", "172.32.0.1", "100.128.0.1"} {
		if err := p.CheckIP(netip.MustParseAddr(ok)); err != nil {
			t.Errorf("CheckIP(%s) = %v; want allowed", ok, err)
		}
	}
	for _, bad := range []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1", "100.64.0.1", "169.254.169.254", "0.0.0.0",
		"224.0.0.1", "255.255.255.255", "::", "::1", "fe80::1", "fc00::1", "fd12::1", "ff02::1", "::ffff:127.0.0.1",
		"::ffff:10.0.0.1", "64:ff9b::1", "fe80::1%eth0",
	} {
		if err := p.CheckIP(netip.MustParseAddr(bad)); !errors.Is(err, netguard.ErrBlocked) {
			t.Errorf("CheckIP(%s) = %v; want ErrBlocked", bad, err)
		}
	}

	lan := netguard.NewPolicy([]netip.Prefix{netip.MustParsePrefix("192.168.10.0/24")}, nil)
	if err := lan.CheckIP(netip.MustParseAddr("192.168.10.5")); err != nil {
		t.Errorf("allowed network = %v; want allowed", err)
	}
	if err := lan.CheckIP(netip.MustParseAddr("192.168.11.5")); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("neighbor network = %v; want ErrBlocked", err)
	}
	if err := lan.CheckIP(netip.MustParseAddr("169.254.169.254")); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("metadata address = %v; want ErrBlocked", err)
	}
}

// TestPolicy_CheckHost verifies host validation before a site is saved.
//
// Scenarios:
//   - literal addresses are classified without resolving
//   - a name is allowed when every address it resolves to is allowed
//   - a name with one forbidden address among public ones is refused
//   - a name that does not resolve, or resolves to nothing, is ErrUnresolvable
func TestPolicy_CheckHost(t *testing.T) {
	resolver := fakeResolver{answers: map[string][]netip.Addr{
		"public.example":  {netip.MustParseAddr("93.184.216.34")},
		"mixed.example":   {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.5")},
		"internal.corp":   {netip.MustParseAddr("192.168.1.10")},
		"empty.example":   {},
		"rebinding.local": {netip.MustParseAddr("127.0.0.1")},
	}}
	p := netguard.NewPolicy(nil, resolver)
	ctx := context.Background()

	if err := p.CheckHost(ctx, "8.8.8.8"); err != nil {
		t.Errorf("public literal = %v", err)
	}
	if err := p.CheckHost(ctx, "127.0.0.1"); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("loopback literal = %v; want ErrBlocked", err)
	}
	if err := p.CheckHost(ctx, "public.example"); err != nil {
		t.Errorf("public name = %v", err)
	}
	for _, name := range []string{"mixed.example", "internal.corp", "rebinding.local"} {
		if err := p.CheckHost(ctx, name); !errors.Is(err, netguard.ErrBlocked) {
			t.Errorf("CheckHost(%s) = %v; want ErrBlocked", name, err)
		}
	}
	for _, name := range []string{"missing.example", "empty.example"} {
		if err := p.CheckHost(ctx, name); !errors.Is(err, netguard.ErrUnresolvable) {
			t.Errorf("CheckHost(%s) = %v; want ErrUnresolvable", name, err)
		}
	}
	if err := netguard.NewPolicy([]netip.Prefix{netip.MustParsePrefix("192.168.0.0/16")}, resolver).CheckHost(ctx, "internal.corp"); err != nil {
		t.Errorf("allowed internal name = %v", err)
	}
}

// TestPolicy_Dialer verifies that the dialer enforces the policy at connection time.
//
// Scenarios:
//   - dialing a loopback listener is refused before connecting
//   - after allowing the loopback network the same dial succeeds
//   - the name resolved at dial time is what is checked, so a name pointing to loopback is refused
//   - an address that is not an IP and port is refused
func TestPolicy_Dialer(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	strict := netguard.NewPolicy(nil, nil).Dialer(time.Second)
	if c, err := strict.DialContext(t.Context(), "tcp", ln.Addr().String()); !errors.Is(err, netguard.ErrBlocked) {
		if c != nil {
			_ = c.Close()
		}
		t.Fatalf("dial loopback = %v; want ErrBlocked", err)
	}
	if c, err := strict.DialContext(t.Context(), "tcp", "localhost:"+portOf(ln)); !errors.Is(err, netguard.ErrBlocked) {
		if c != nil {
			_ = c.Close()
		}
		t.Fatalf("dial localhost = %v; want ErrBlocked", err)
	}

	open := netguard.NewPolicy([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, nil).Dialer(time.Second)
	c, err := open.DialContext(t.Context(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial allowed loopback = %v", err)
	}
	_ = c.Close()

	if err := netguard.NewPolicy(nil, nil).Control("tcp", "not-an-address", nil); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("Control(garbage) = %v; want ErrBlocked", err)
	}
}

// portOf returns the port of a listener as text.
func portOf(ln net.Listener) string {
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

// TestPolicy_Extra verifies the networks an administrator allows while the server runs.
//
// Scenarios:
//   - an extra network opens its addresses at once and only its own, next to the fixed ones
//   - the fixed and the extra networks are reported apart, as copies the caller cannot use to change the policy
//   - replacing the extra networks closes what they opened
func TestPolicy_Extra(t *testing.T) {
	p := netguard.NewPolicy([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, nil)
	inside := netip.MustParseAddr("192.168.1.5")
	if err := p.CheckIP(inside); !errors.Is(err, netguard.ErrBlocked) {
		t.Fatalf("CheckIP(%s) = %v; want ErrBlocked before the extra network", inside, err)
	}

	extra := []netip.Prefix{netip.MustParsePrefix("192.168.0.0/16")}
	p.SetExtra(extra)
	if err := p.CheckIP(inside); err != nil {
		t.Errorf("CheckIP(%s) = %v; want allowed by the extra network", inside, err)
	}
	if err := p.CheckIP(netip.MustParseAddr("10.9.9.9")); err != nil {
		t.Errorf("CheckIP(fixed network) = %v; want still allowed", err)
	}
	if err := p.CheckIP(netip.MustParseAddr("172.16.0.1")); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("CheckIP(172.16.0.1) = %v; want ErrBlocked", err)
	}

	got := p.Extra()
	got[0] = netip.MustParsePrefix("172.16.0.0/12")
	extra[0] = netip.MustParsePrefix("172.16.0.0/12")
	if p.Extra()[0] != netip.MustParsePrefix("192.168.0.0/16") || p.Fixed()[0] != netip.MustParsePrefix("10.0.0.0/8") {
		t.Errorf("Extra() = %v, Fixed() = %v; want independent copies", p.Extra(), p.Fixed())
	}

	p.SetExtra(nil)
	if err := p.CheckIP(inside); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("CheckIP(%s) = %v; want ErrBlocked after clearing", inside, err)
	}
}
