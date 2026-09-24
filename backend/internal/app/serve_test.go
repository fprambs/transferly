package app_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/app"
	"github.com/fprambs/sentinel/internal/config"
	"github.com/fprambs/sentinel/internal/testutil"
)

// testConfig returns a valid configuration pointing at databaseURL with a short shutdown timeout.
func testConfig(t *testing.T, databaseURL string) config.Config {
	t.Helper()
	cfg, err := config.Load(func(k string) string {
		if k == "SENTINEL_DATABASE_URL" {
			return databaseURL
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.ShutdownTimeout = 5 * time.Second
	return cfg
}

// discardLogger returns a logger that drops all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestServe_StartsServesAndShutsDownGracefully verifies the server lifecycle.
//
// Scenarios:
//   - migrations apply and the server starts on the given listener
//   - /readyz and /healthz answer 200
//   - canceling the context shuts the server down and Serve returns nil
func TestServe_StartsServesAndShutsDownGracefully(t *testing.T) {
	cfg := testConfig(t, testutil.NewDatabase(t))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ln := listen(t)

	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, cfg, discardLogger(), ln) }()

	base := "http://" + ln.Addr().String()
	waitForStatus(t, base+"/readyz", http.StatusOK)
	waitForStatus(t, base+"/healthz", http.StatusOK)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve() after cancel = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve() did not return after the context was canceled")
	}
}

// TestServe_FailsWhenDatabaseIsUnreachable verifies that Serve returns an error
// instead of starting when the database cannot be reached.
func TestServe_FailsWhenDatabaseIsUnreachable(t *testing.T) {
	testutil.DatabaseURL(t)
	cfg := testConfig(t, "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	cfg.AutoMigrate = false
	ln := listen(t)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := app.Serve(ctx, cfg, discardLogger(), ln); err == nil {
		t.Error("Serve() with unreachable database = nil, want error")
	}
}

// TestServe_FailsWhenMigrationsCannotRun verifies that Serve reports a migration failure
// instead of starting when auto-migration is on and the database is unreachable.
func TestServe_FailsWhenMigrationsCannotRun(t *testing.T) {
	testutil.DatabaseURL(t)
	cfg := testConfig(t, "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	cfg.AutoMigrate = true
	ln := listen(t)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := app.Serve(ctx, cfg, discardLogger(), ln); err == nil {
		t.Error("Serve() with failing migrations = nil, want error")
	}
}

// TestServe_FailsWhenListenerIsClosed verifies that Serve returns the listener error
// when the HTTP server cannot accept connections.
func TestServe_FailsWhenListenerIsClosed(t *testing.T) {
	cfg := testConfig(t, testutil.NewDatabase(t))
	ln := listen(t)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	err := app.Serve(t.Context(), cfg, discardLogger(), ln)
	if err == nil || !strings.Contains(err.Error(), "http server") {
		t.Errorf("Serve() with closed listener = %v, want an http server error", err)
	}
}

// TestServe_FailsWhenGracefulShutdownTimesOut verifies that Serve reports an error
// when a connection stays open past the shutdown timeout.
//
// A connection that never sends a request is not idle for net/http, so shutdown waits on it.
func TestServe_FailsWhenGracefulShutdownTimesOut(t *testing.T) {
	cfg := testConfig(t, testutil.NewDatabase(t))
	cfg.ShutdownTimeout = 50 * time.Millisecond
	ln := listen(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, cfg, discardLogger(), ln) }()

	base := "http://" + ln.Addr().String()
	waitForStatus(t, base+"/readyz", http.StatusOK)
	stuck, err := (&net.Dialer{}).DialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stuck.Close() }()
	waitForStatus(t, base+"/healthz", http.StatusOK)

	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "graceful shutdown") {
			t.Errorf("Serve() = %v, want a graceful shutdown error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve() did not return after the shutdown timeout")
	}
}

// listen opens a loopback listener on a free port and closes it when the test ends.
func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// waitForStatus polls url until it answers with the wanted status, failing after a deadline.
func waitForStatus(t *testing.T, url string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := testutil.Fetch(t.Context(), http.MethodGet, url)
		if err == nil && resp.Status == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s did not return %d in time (err=%v)", url, want, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
