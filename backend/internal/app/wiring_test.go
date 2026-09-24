package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunJanitor verifies the periodic cleanup loop.
//
// Scenarios:
//   - every task runs on each tick
//   - failures are logged and do not stop the other tasks or the loop
//   - non-empty cleanups are logged
//   - canceling the context stops the loop
func TestRunJanitor(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	var failing, removing, idle atomic.Int32
	tasks := []janitorTask{
		{"failing", func(context.Context) (int, error) { failing.Add(1); return 0, errors.New("disk on fire") }},
		{"removing", func(context.Context) (int, error) { removing.Add(1); return 3, nil }},
		{"idle", func(context.Context) (int, error) { idle.Add(1); return 0, nil }},
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		runJanitor(ctx, 5*time.Millisecond, logger, tasks)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for failing.Load() < 2 || removing.Load() < 2 || idle.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("tasks did not run repeatedly")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the janitor did not stop when the context was canceled")
	}

	out := logs.String()
	if !strings.Contains(out, "janitor task failed") || !strings.Contains(out, "disk on fire") || !strings.Contains(out, "janitor removed items") {
		t.Errorf("logs = %q, want the failure and the cleanup", out)
	}
}
