package app_test

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/app"
	"github.com/fprambs/sentinel/internal/testutil"
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
	valid := map[string]string{"SENTINEL_DATABASE_URL": dbURL}

	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantErr string
	}{
		{"missing configuration", []string{"serve"}, map[string]string{}, "SENTINEL_DATABASE_URL"},
		{"unknown command", []string{"explode"}, valid, "unknown command"},
		{"unknown migrate subcommand", []string{"migrate", "sideways"}, valid, "unknown migrate subcommand"},
		{"unusable listen address", []string{"serve"}, map[string]string{"SENTINEL_DATABASE_URL": dbURL, "SENTINEL_HTTP_ADDR": "no-such-host-name:99999"}, "listening on"},
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
	env := envOf(map[string]string{"SENTINEL_DATABASE_URL": testutil.NewDatabase(t)})
	var out bytes.Buffer

	if err := app.Run(t.Context(), []string{"migrate"}, env, &out); err != nil {
		t.Fatalf("Run(migrate) = %v", err)
	}
	if err := app.Run(t.Context(), []string{"migrate", "status"}, env, &out); err != nil {
		t.Fatalf("Run(migrate status) = %v", err)
	}
	if !strings.Contains(out.String(), "schema version: 1") {
		t.Errorf("output = %q, want the schema version", out.String())
	}

	unreachable := envOf(map[string]string{"SENTINEL_DATABASE_URL": "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1"})
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
		"SENTINEL_DATABASE_URL": testutil.NewDatabase(t),
		"SENTINEL_HTTP_ADDR":    "127.0.0.1:0",
		"SENTINEL_LOG_LEVEL":    "debug",
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
