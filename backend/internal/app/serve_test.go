package app_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/app"
	"github.com/fprambs/sentinel/internal/config"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/transfer"
	"github.com/fprambs/sentinel/internal/core/workspace"
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
	cfg.DataDir = t.TempDir()
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

// TestServe_FailsWhenTheInstanceKeyCannotBeResolved verifies that Serve refuses to start
// when the instance key file is corrupt.
func TestServe_FailsWhenTheInstanceKeyCannotBeResolved(t *testing.T) {
	cfg := testConfig(t, testutil.NewDatabase(t))
	if err := os.WriteFile(filepath.Join(cfg.DataDir, "instance.key"), []byte("junk\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := app.Serve(t.Context(), cfg, discardLogger(), listen(t))

	if err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("Serve() with a corrupt key = %v, want a corrupt key error", err)
	}
}

// TestServe_FailsWhenTheServicesCannotBeComposed verifies that Serve refuses to start with a base URL the passkeys cannot be bound to.
func TestServe_FailsWhenTheServicesCannotBeComposed(t *testing.T) {
	cfg := testConfig(t, testutil.NewDatabase(t))
	cfg.AllowedOrigins = []string{"://bad"}

	err := app.Serve(t.Context(), cfg, discardLogger(), listen(t))

	if err == nil || !strings.Contains(err.Error(), "composing the services") {
		t.Errorf("Serve() with a bad base URL = %v, want a composition error", err)
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

// TestServe_FailsWhenTransfersCannotBeRecovered verifies that Serve refuses to start when it cannot mark the
// transfers left over by a previous run.
//
// Scenarios:
//   - a database without the schema, with migrations off, makes recovery fail and Serve return that error
func TestServe_FailsWhenTransfersCannotBeRecovered(t *testing.T) {
	cfg := testConfig(t, testutil.NewDatabase(t))
	cfg.AutoMigrate = false
	ln := listen(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	err := app.Serve(ctx, cfg, discardLogger(), ln)
	if err == nil || !strings.Contains(err.Error(), "recovering transfers") {
		t.Errorf("Serve() = %v; want a recovery failure", err)
	}
}

// TestServe_FailsWhenTheAllowedNetworksCannotBeLoaded verifies that Serve refuses to start when it cannot read the networks the
// administrators allowed, instead of running with a stricter policy than they chose.
//
// Scenarios:
//   - a database whose allowed networks table is gone makes Serve return that error
func TestServe_FailsWhenTheAllowedNetworksCannotBeLoaded(t *testing.T) {
	url := testutil.NewDatabase(t)
	if err := postgres.Migrate(t.Context(), url); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.DB().Exec(t.Context(), "DROP TABLE network_allowances"); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t, url)
	cfg.AutoMigrate = false
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	err = app.Serve(ctx, cfg, discardLogger(), listen(t))
	if err == nil || !strings.Contains(err.Error(), "loading the allowed networks") {
		t.Errorf("Serve() = %v; want a failure to load the networks", err)
	}
}

// TestServe_InterruptsTransfersLeftRunning verifies the recovery of a crashed run.
//
// Scenarios:
//   - a transfer that was running when the previous process stopped is marked interrupted at startup and the
//     server logs how many there were
func TestServe_InterruptsTransfersLeftRunning(t *testing.T) {
	url := testutil.NewDatabase(t)
	if err := postgres.Migrate(t.Context(), url); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	at := time.Now().UTC()
	user := auth.User{ID: uuid.New(), Email: "u@example.com", PasswordHash: "h", Role: auth.RoleUser, Locale: "en", Timezone: "UTC", CreatedAt: at}
	if err := store.CreateUser(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	ws := workspace.Workspace{ID: uuid.New(), Name: "Personal", Kind: workspace.KindPersonal, OwnerID: user.ID, CreatedAt: at}
	if err := store.CreateWorkspace(t.Context(), ws); err != nil {
		t.Fatal(err)
	}
	running := transfer.Transfer{ID: uuid.New(), WorkspaceID: ws.ID, UserID: user.ID, SiteName: "S", Direction: transfer.Upload, Path: "/f", Name: "f",
		Status: transfer.Running, Attempts: 1, CreatedAt: at, UpdatedAt: at}
	if err := store.CreateTransfer(t.Context(), running); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	cfg := testConfig(t, url)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ln := listen(t)
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, cfg, slog.New(slog.NewTextHandler(&logs, nil)), ln) }()
	waitForStatus(t, "http://"+ln.Addr().String()+"/readyz", http.StatusOK)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Serve() = %v", err)
	}

	got, err := store.TransferByID(t.Context(), user.ID, running.ID)
	if err != nil || got.Status != transfer.Interrupted || got.ErrorCode != "server_restarted" {
		t.Errorf("transfer after startup = %+v, %v", got, err)
	}
	if !strings.Contains(logs.String(), "transfers interrupted by the previous shutdown") {
		t.Errorf("the recovery was not logged: %s", logs.String())
	}
}
