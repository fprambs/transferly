package app_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/app"
	"github.com/fprambs/transferly/internal/testutil"
)

// syncBuffer is a bytes.Buffer safe for concurrent use by a logger and a test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends p to the buffer.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns the accumulated output.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// envOf returns a getenv function backed by the given variables.
func envOf(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

// TestRun_RejectsInvalidInvocations verifies the errors of the command line.
//
// Scenarios:
//   - missing configuration
//   - unknown command
//   - unknown migrate subcommand
//   - an address that cannot be listened on
func TestRun_RejectsInvalidInvocations(t *testing.T) {
	dbURL := testutil.NewDatabase(t)
	valid := map[string]string{"TRANSFERLY_DATABASE_URL": dbURL, "TRANSFERLY_DATA_DIR": t.TempDir()}

	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantErr string
	}{
		{"missing configuration", []string{"serve"}, map[string]string{}, "TRANSFERLY_DATABASE_URL"},
		{"unknown command", []string{"explode"}, valid, "unknown command"},
		{"unknown migrate subcommand", []string{"migrate", "sideways"}, valid, "unknown migrate subcommand"},
		{"unusable listen address", []string{"serve"}, map[string]string{"TRANSFERLY_DATABASE_URL": dbURL, "TRANSFERLY_DATA_DIR": t.TempDir(), "TRANSFERLY_HTTP_ADDR": "no-such-host-name:99999"}, "listening on"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := app.Run(t.Context(), tt.args, envOf(tt.env), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Run() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestRun_Migrate verifies the migrate command.
//
// Scenarios:
//   - "migrate" without a subcommand applies migrations (up is the default)
//   - "migrate status" prints the schema version
//   - "migrate up" and "migrate status" fail when the database is unreachable
func TestRun_Migrate(t *testing.T) {
	env := envOf(map[string]string{"TRANSFERLY_DATABASE_URL": testutil.NewDatabase(t)})
	var out bytes.Buffer

	if err := app.Run(t.Context(), []string{"migrate"}, env, &out); err != nil {
		t.Fatalf("Run(migrate) = %v", err)
	}
	if err := app.Run(t.Context(), []string{"migrate", "status"}, env, &out); err != nil {
		t.Fatalf("Run(migrate status) = %v", err)
	}
	var version int
	if _, err := fmt.Sscanf(out.String(), "schema version: %d", &version); err != nil || version < 1 {
		t.Errorf("output = %q, want a schema version of at least 1", out.String())
	}

	unreachable := envOf(map[string]string{"TRANSFERLY_DATABASE_URL": "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1"})
	for _, sub := range []string{"up", "status"} {
		if err := app.Run(t.Context(), []string{"migrate", sub}, unreachable, &out); err == nil {
			t.Errorf("Run(migrate %s) with unreachable database = nil, want error", sub)
		}
	}
}

// TestRun_ServeStartsAndStopsOnCancel verifies the serve command end to end.
//
// Scenarios:
//   - serve is the default command and listens on the configured address
//   - the startup log announces the bound address at the configured level
//   - canceling the context stops the server cleanly
func TestRun_ServeStartsAndStopsOnCancel(t *testing.T) {
	env := envOf(map[string]string{
		"TRANSFERLY_DATABASE_URL": testutil.NewDatabase(t),
		"TRANSFERLY_HTTP_ADDR":    "127.0.0.1:0",
		"TRANSFERLY_DATA_DIR":     t.TempDir(),
		"TRANSFERLY_LOG_LEVEL":    "debug",
	})
	var logs syncBuffer
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, nil, env, &logs) }()

	addr := waitForAddress(t, &logs)
	waitForStatus(t, "http://"+addr+"/healthz", 200)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() after cancel = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return after the context was canceled")
	}
}

// TestRun_Healthcheck verifies the healthcheck command used by container health checks.
//
// Scenarios:
//   - a server answering 200 on the liveness path is healthy
//   - a server answering an error status is unhealthy
//   - nothing listening on the address is unhealthy
//   - a listen address without a port, or with an unusable port, is rejected
func TestRun_Healthcheck(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
	}))
	defer server.Close()
	envFor := func(addr string) func(string) string {
		return envOf(map[string]string{"TRANSFERLY_DATABASE_URL": "postgres://x", "TRANSFERLY_HTTP_ADDR": addr})
	}
	run := func(addr string) error {
		return app.Run(t.Context(), []string{"healthcheck"}, envFor(addr), &bytes.Buffer{})
	}
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())

	if err := run(":" + port); err != nil {
		t.Errorf("healthy server: Run(healthcheck) = %v, want nil", err)
	}
	status = http.StatusInternalServerError
	if err := run(":" + port); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("failing server: Run(healthcheck) = %v, want a 500 error", err)
	}
	for name, addr := range map[string]string{"nothing listening": ":1", "no port": "localhost", "unusable port": ":80 80"} {
		if err := run(addr); err == nil {
			t.Errorf("%s: Run(healthcheck) = nil, want error", name)
		}
	}
}

// waitForAddress polls the log output until the server announces the address it listens on.
func waitForAddress(t *testing.T, logs *syncBuffer) string {
	t.Helper()
	pattern := regexp.MustCompile(`"addr":"([^"]+)"`)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if m := pattern.FindStringSubmatch(logs.String()); m != nil {
			return m[1]
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never announced its address; logs: %s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRun_Version verifies the version command.
//
// Scenarios:
//   - it prints the version of the build, dev for builds that do not set it, without any configuration
func TestRun_Version(t *testing.T) {
	var out bytes.Buffer
	if err := app.Run(t.Context(), []string{"version"}, envOf(map[string]string{}), &out); err != nil || out.String() != "transferly dev\n" {
		t.Errorf("Run(version) = %v, %q; want transferly dev", err, out.String())
	}
}
