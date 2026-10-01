// Command ftpstls checks whether jlaffaye/ftp can talk to FTPS servers that require TLS session reuse
// on the data channel, in explicit and implicit modes, with and without a client session cache.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jlaffaye/ftp"
)

// attempt describes one connection variant.
type attempt struct {
	name     string
	addr     string
	implicit bool
	cache    bool
	tls12    bool
	noName   bool
}

// stage is the step the running attempt has reached; it is shown when an attempt hangs.
var stage atomic.Value

// run performs login, upload, listing and download, returning the first failure.
func run(a attempt) error {
	stage.Store("dial")
	config := &tls.Config{InsecureSkipVerify: true, ServerName: "transferly-test"}
	if a.tls12 {
		config.MaxVersion = tls.VersionTLS12
	}
	if a.noName {
		config.ServerName = ""
	}
	if a.cache {
		config.ClientSessionCache = tls.NewLRUClientSessionCache(8)
	}
	option := ftp.DialWithExplicitTLS(config)
	if a.implicit {
		option = ftp.DialWithTLS(config)
	}
	conn, err := ftp.Dial(a.addr, option, ftp.DialWithTimeout(10*time.Second))
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.Quit() }()
	stage.Store("login")
	if err := conn.Login("transferly", "transferly"); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	stage.Store("upload")
	if err := conn.Stor("upload/tls-probe.txt", strings.NewReader("hello over tls")); err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	stage.Store("list")
	entries, err := conn.List("upload")
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("list: no entries")
	}
	stage.Store("download")
	response, err := conn.Retr("upload/tls-probe.txt")
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	stage.Store("download: reading")
	body, err := io.ReadAll(response)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if string(body) != "hello over tls" {
		return fmt.Errorf("download content %q", body)
	}
	stage.Store("download: closing the data connection")
	if err := response.Close(); err != nil {
		return fmt.Errorf("closing download: %w", err)
	}
	stage.Store("delete")
	return conn.Delete("upload/tls-probe.txt")
}

// attemptTimeout bounds one variant; a hang is itself a result.
const attemptTimeout = 20 * time.Second

// outcome runs an attempt with a deadline and reports success or the failure, including hangs.
func outcome(a attempt) error {
	done := make(chan error, 1)
	go func() { done <- run(a) }()
	select {
	case err := <-done:
		return err
	case <-time.After(attemptTimeout):
		return fmt.Errorf("no answer after %s (hang during %v)", attemptTimeout, stage.Load())
	}
}

// main runs every variant and prints the outcome.
func main() {
	address, _ := (&net.Resolver{}).LookupHost(context.Background(), "ftps")
	attempts := []attempt{
		{"explicit, TLS 1.3, session cache", "ftps:21", false, true, false, false},
		{"explicit, TLS 1.3, no session cache", "ftps:21", false, false, false, false},
		{"explicit, TLS 1.2 only, session cache", "ftps:21", false, true, true, false},
		{"explicit, TLS 1.2 only, no session cache", "ftps:21", false, false, true, false},
		{"implicit, TLS 1.3, session cache", "ftps-implicit:990", true, true, false, false},
		{"implicit, TLS 1.3, no session cache", "ftps-implicit:990", true, false, false, false},
		{"implicit, TLS 1.2 only, session cache", "ftps-implicit:990", true, true, true, false},
		{"explicit, session cache but empty ServerName (dialing an IP)", "IPADDR:21", false, true, false, true},
	}
	for _, a := range attempts {
		a.addr = strings.Replace(a.addr, "IPADDR", address[0], 1)
		if err := outcome(a); err != nil {
			fmt.Printf("FAIL  %-34s %v\n", a.name, err)
			continue
		}
		fmt.Printf("ok    %s\n", a.name)
	}
}
