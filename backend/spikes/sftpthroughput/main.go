// Command sftpthroughput measures SFTP transfer speed under injected latency and bandwidth limits.
//
// It answers whether pipelining keeps the link full at 100 ms round-trip time.
// It is a throwaway experiment: host keys are not verified.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// scenario is one combination of link conditions and client tuning.
type scenario struct {
	name       string
	rtt        time.Duration
	rateKBps   int
	concurrent bool
	window     int
	sizeMiB    int64
	parallel   int
	shared     bool
}

// zeros is an endless reader of zero bytes.
type zeros struct{}

// Read fills p with zeros.
func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// toxiproxy talks to the Toxiproxy HTTP API.
type toxiproxy struct {
	api   string
	proxy string
}

// reset removes every toxic and re-enables all proxies.
func (t toxiproxy) reset(ctx context.Context) error {
	return t.post(ctx, "/reset", nil)
}

// addToxic adds a toxic to the proxy in both directions.
func (t toxiproxy) addToxic(ctx context.Context, kind string, attributes map[string]any) error {
	for _, stream := range []string{"upstream", "downstream"} {
		body := map[string]any{
			"name":       kind + "_" + stream,
			"type":       kind,
			"stream":     stream,
			"toxicity":   1.0,
			"attributes": attributes,
		}
		if err := t.post(ctx, "/proxies/"+t.proxy+"/toxics", body); err != nil {
			return err
		}
	}
	return nil
}

// post sends a JSON request and fails on any non-2xx answer.
func (t toxiproxy) post(ctx context.Context, path string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.api+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("toxiproxy %s answered %d", path, response.StatusCode)
	}
	return nil
}

// apply configures the proxy for a scenario.
func (t toxiproxy) apply(ctx context.Context, s scenario) error {
	if err := t.reset(ctx); err != nil {
		return err
	}
	if s.rtt > 0 {
		if err := t.addToxic(ctx, "latency", map[string]any{"latency": s.rtt.Milliseconds() / 2}); err != nil {
			return err
		}
	}
	if s.rateKBps > 0 {
		return t.addToxic(ctx, "bandwidth", map[string]any{"rate": s.rateKBps})
	}
	return nil
}

// measure uploads and downloads a file through the proxy and returns the speeds in MB/s.
func measure(addr, user, password string, s scenario) (up, down float64, err error) {
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = conn.Close() }()
	client, err := sftp.NewClient(conn,
		sftp.UseConcurrentReads(s.concurrent),
		sftp.UseConcurrentWrites(s.concurrent),
		sftp.MaxConcurrentRequestsPerFile(s.window),
	)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = client.Close() }()

	const remote = "upload/spike.bin"
	size := s.sizeMiB << 20
	file, err := client.Create(remote)
	if err != nil {
		return 0, 0, err
	}
	start := time.Now()
	_, err = file.ReadFrom(io.LimitReader(zeros{}, size))
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return 0, 0, fmt.Errorf("upload: %w", firstError(err, closeErr))
	}
	up = float64(size) / (1 << 20) / time.Since(start).Seconds()

	file, err = client.Open(remote)
	if err != nil {
		return 0, 0, err
	}
	start = time.Now()
	_, err = file.WriteTo(io.Discard)
	_ = file.Close()
	if err != nil {
		return 0, 0, fmt.Errorf("download: %w", err)
	}
	down = float64(size) / (1 << 20) / time.Since(start).Seconds()
	return up, down, client.Remove(remote)
}

// measureMany transfers parallel files at once, over one shared SSH connection or one connection each,
// and returns the aggregate upload and download speeds in MB/s.
func measureMany(addr, user, password string, s scenario) (up, down float64, err error) {
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	var shared *ssh.Client
	if s.shared {
		if shared, err = ssh.Dial("tcp", addr, config); err != nil {
			return 0, 0, err
		}
		defer func() { _ = shared.Close() }()
	}
	clients := make([]*sftp.Client, s.parallel)
	for i := range clients {
		conn := shared
		if conn == nil {
			if conn, err = ssh.Dial("tcp", addr, config); err != nil {
				return 0, 0, err
			}
			defer func() { _ = conn.Close() }()
		}
		clients[i], err = sftp.NewClient(conn,
			sftp.UseConcurrentReads(true), sftp.UseConcurrentWrites(true), sftp.MaxConcurrentRequestsPerFile(s.window))
		if err != nil {
			return 0, 0, err
		}
		defer func() { _ = clients[i].Close() }()
	}

	size := s.sizeMiB << 20
	run := func(work func(i int, c *sftp.Client) error) (float64, error) {
		errs := make(chan error, len(clients))
		start := time.Now()
		for i, c := range clients {
			go func() { errs <- work(i, c) }()
		}
		for range clients {
			if err := <-errs; err != nil {
				return 0, err
			}
		}
		return float64(size) * float64(len(clients)) / (1 << 20) / time.Since(start).Seconds(), nil
	}
	name := func(i int) string { return fmt.Sprintf("upload/spike-%d.bin", i) }

	up, err = run(func(i int, c *sftp.Client) error {
		file, err := c.Create(name(i))
		if err != nil {
			return err
		}
		_, err = file.ReadFrom(io.LimitReader(zeros{}, size))
		return firstError(err, file.Close())
	})
	if err != nil {
		return 0, 0, err
	}
	down, err = run(func(i int, c *sftp.Client) error {
		file, err := c.Open(name(i))
		if err != nil {
			return err
		}
		_, err = file.WriteTo(io.Discard)
		return firstError(err, file.Close(), c.Remove(name(i)))
	})
	return up, down, err
}

// firstError returns the first non-nil error.
func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// main runs every scenario and prints a table.
func main() {
	proxyAddr := flag.String("proxy", "toxiproxy:2222", "SFTP address behind Toxiproxy")
	api := flag.String("api", "http://toxiproxy:8474", "Toxiproxy API URL")
	only := flag.String("only", "", "run only scenarios whose name contains this text")
	flag.Parse()

	link100Mbit, link1Gbit := 12800, 128000
	ms := 100 * time.Millisecond
	scenarios := []scenario{
		{"no limits, 0 ms, pipelined 64", 0, 0, true, 64, 256, 1, false},
		{"no limits, 100 ms, sequential", ms, 0, false, 1, 4, 1, false},
		{"no limits, 100 ms, pipelined 64", ms, 0, true, 64, 128, 1, false},
		{"no limits, 100 ms, pipelined 256", ms, 0, true, 256, 128, 1, false},
		{"100 Mbit, 100 ms, sequential", ms, link100Mbit, false, 1, 4, 1, false},
		{"100 Mbit, 100 ms, pipelined 64", ms, link100Mbit, true, 64, 64, 1, false},
		{"1 Gbit, 100 ms, pipelined 64", ms, link1Gbit, true, 64, 128, 1, false},
		{"1 Gbit, 100 ms, pipelined 256", ms, link1Gbit, true, 256, 128, 1, false},
		{"1 Gbit, 100 ms, 4 files, 1 SSH connection", ms, link1Gbit, true, 64, 64, 4, true},
		{"1 Gbit, 100 ms, 4 files, 4 SSH connections", ms, link1Gbit, true, 64, 64, 4, false},
		{"1 Gbit, 100 ms, 8 files, 1 SSH connection", ms, link1Gbit, true, 64, 64, 8, true},
	}

	proxy := toxiproxy{api: *api, proxy: "sftp"}
	ctx := context.Background()
	fmt.Printf("%-36s %10s %10s %10s\n", "scenario", "up MB/s", "down MB/s", "of link")
	for _, s := range scenarios {
		if *only != "" && !strings.Contains(s.name, *only) {
			continue
		}
		if err := proxy.apply(ctx, s); err != nil {
			fmt.Fprintln(os.Stderr, "toxiproxy:", err)
			os.Exit(1)
		}
		measureFunc := measure
		if s.parallel > 1 {
			measureFunc = measureMany
		}
		up, down, err := measureFunc(*proxyAddr, "transferly", "transferly", s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", s.name, err)
			os.Exit(1)
		}
		share := "-"
		if s.rateKBps > 0 {
			share = fmt.Sprintf("%.0f%%", 100*min(up, down)/(float64(s.rateKBps)/1024))
		}
		fmt.Printf("%-36s %10.1f %10.1f %10s\n", s.name, up, down, share)
	}
	_ = proxy.reset(ctx)
}
