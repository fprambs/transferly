package proxydial

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
)

// socks5 reply codes (RFC 1928 §6), the ones worth naming; anything else is reported by its number.
const (
	socks5Succeeded         = 0x00
	socks5NoAcceptableAuth  = 0xFF
	socks5AuthOK            = 0x00
	socks5AddrIPv4          = 0x01
	socks5AddrDomain        = 0x03
	socks5AddrIPv6          = 0x04
	socks5CmdConnect        = 0x01
	socks5AuthNone          = 0x00
	socks5AuthUserPass      = 0x02
	socks5UserPassVersion   = 0x01
	socks5UserPassSucceeded = 0x00
)

// socks5Connect runs the SOCKS5 handshake (RFC 1928) over conn and asks it to connect to address. Authentication
// (RFC 1929) runs when cfg.Username is set.
func socks5Connect(conn net.Conn, cfg Config, address string) error {
	methods := []byte{socks5AuthNone}
	if cfg.Username != "" {
		methods = []byte{socks5AuthUserPass}
	}
	if _, err := conn.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		return err
	}
	chosen := make([]byte, 2)
	if _, err := io.ReadFull(conn, chosen); err != nil {
		return err
	}
	if chosen[0] != 0x05 {
		return fmt.Errorf("%w: not a SOCKS5 proxy", ErrRefused)
	}
	switch chosen[1] {
	case socks5AuthNone:
	case socks5AuthUserPass:
		if err := socks5Authenticate(conn, cfg); err != nil {
			return err
		}
	case socks5NoAcceptableAuth:
		return fmt.Errorf("%w: no acceptable authentication method", ErrRefused)
	default:
		return fmt.Errorf("%w: unknown authentication method %#x", ErrRefused, chosen[1])
	}
	return socks5RequestConnect(conn, address)
}

// socks5Authenticate runs the username/password subnegotiation (RFC 1929).
func socks5Authenticate(conn net.Conn, cfg Config) error {
	if len(cfg.Username) > 255 || len(cfg.Password) > 255 {
		return fmt.Errorf("%w: the username or password is too long for SOCKS5", ErrRefused)
	}
	req := []byte{socks5UserPassVersion, byte(len(cfg.Username))}
	req = append(req, cfg.Username...)
	req = append(req, byte(len(cfg.Password)))
	req = append(req, cfg.Password...)
	if _, err := conn.Write(req); err != nil {
		return err
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[1] != socks5UserPassSucceeded {
		return fmt.Errorf("%w: authentication refused", ErrRefused)
	}
	return nil
}

// socks5RequestConnect sends the CONNECT request and reads the reply, whose bound address is read and discarded:
// nothing here uses it.
func socks5RequestConnect(conn net.Conn, address string) error {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRefused, err)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return fmt.Errorf("%w: invalid port in %s", ErrRefused, address)
	}
	req := []byte{0x05, socks5CmdConnect, 0x00}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Is4() {
			req = append(req, socks5AddrIPv4)
			b := ip.As4()
			req = append(req, b[:]...)
		} else {
			req = append(req, socks5AddrIPv6)
			b := ip.As16()
			req = append(req, b[:]...)
		}
	} else {
		if len(host) > 255 {
			return fmt.Errorf("%w: host name too long for SOCKS5", ErrRefused)
		}
		req = append(req, socks5AddrDomain, byte(len(host)))
		req = append(req, host...)
	}
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := conn.Write(req); err != nil {
		return err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[0] != 0x05 {
		return fmt.Errorf("%w: not a SOCKS5 proxy", ErrRefused)
	}
	if head[1] != socks5Succeeded {
		return fmt.Errorf("%w: %s", ErrRefused, socks5ReplyText(head[1]))
	}
	return socks5DiscardBoundAddress(conn, head[3])
}

// socks5DiscardBoundAddress reads and discards the address the reply carries, whose length depends on its type, and
// the 2 bytes of the bound port that follow it.
func socks5DiscardBoundAddress(conn net.Conn, addrType byte) error {
	var n int
	switch addrType {
	case socks5AddrIPv4:
		n = 4
	case socks5AddrIPv6:
		n = 16
	case socks5AddrDomain:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return err
		}
		n = int(length[0])
	default:
		return fmt.Errorf("%w: unknown address type %#x in reply", ErrRefused, addrType)
	}
	_, err := io.CopyN(io.Discard, conn, int64(n)+2)
	return err
}

// socks5ReplyText names a SOCKS5 reply code (RFC 1928 §6), for the error it becomes.
func socks5ReplyText(code byte) string {
	switch code {
	case 0x01:
		return "general SOCKS server failure"
	case 0x02:
		return "connection not allowed by ruleset"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "TTL expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	}
	return fmt.Sprintf("unknown reply code %#x", code)
}
