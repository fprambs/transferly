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

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/config"
	"github.com/fprambs/sentinel/internal/core/activity"
	"github.com/fprambs/sentinel/internal/core/transfer"
	"github.com/fprambs/sentinel/internal/testutil"
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

// TestNewWiring_RegistersJanitorTasks verifies that the wiring cleans expired sessions and expired vault keys.
//
// Scenarios:
//   - the key sweep task is registered and reports nothing to remove on an empty key ring
func TestNewWiring_RegistersJanitorTasks(t *testing.T) {
	w := newWiring(config.Config{SessionIdle: time.Minute, SessionMax: time.Hour}, slog.New(slog.DiscardHandler), nil, []byte("0123456789abcdef0123456789abcdef"))

	names := map[string]int{}
	for _, task := range w.janitor {
		if task.name == "idle quick connections" {
			if removed, err := task.run(t.Context()); removed != 0 || err != nil {
				t.Errorf("%s = %d, %v; want nothing removed", task.name, removed, err)
			}
		}
		if task.name == "expired vault keys" {
			if removed, err := task.run(t.Context()); removed != 0 || err != nil {
				t.Errorf("%s = %d, %v; want nothing removed", task.name, removed, err)
			}
		}
		names[task.name]++
	}
	if names["old activity"] != 1 || names["expired invitations"] != 1 || names["expired vault keys"] != 1 || names["expired sessions"] != 1 || names["idle quick connections"] != 1 {
		t.Errorf("janitor tasks = %v; want sessions, quick connections, vault keys, old activity and invitations once each", names)
	}
}

// logStore is an activity store that keeps what it is given, or fails.
type logStore struct {
	entries []activity.Entry
	fail    error
}

// InsertActivity keeps the entry or fails.
func (l *logStore) InsertActivity(_ context.Context, e activity.Entry) error {
	l.entries = append(l.entries, e)
	return l.fail
}

// ActivityByUser is not used here.
func (l *logStore) ActivityByUser(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int) ([]activity.Entry, error) {
	return nil, nil
}

// PurgeActivity is not used here.
func (l *logStore) PurgeActivity(context.Context, time.Time) (int, error) { return 0, nil }

// TestTransferLogger verifies the hook that logs the transfers that end.
//
// Scenarios:
//   - an ended transfer becomes an activity entry with its site, path, direction, state and error code
//   - a log that cannot be written is reported as a warning and does not panic or fail the transfer
func TestTransferLogger(t *testing.T) {
	store := &logStore{}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	log := activity.NewService(store, testutil.NewFakeClock(time.Now()), time.Hour, nil)
	ended := transfer.Transfer{UserID: uuid.New(), WorkspaceID: uuid.New(), SiteName: "Files", Direction: transfer.Upload, Path: "/a", Status: transfer.Failed, ErrorCode: "remote_unavailable"}

	transferLogger(log, logger)(ended)
	if len(store.entries) != 1 || store.entries[0].Kind != activity.Transfer || store.entries[0].Detail != "upload:failed" || store.entries[0].ErrorCode != "remote_unavailable" || store.entries[0].Path != "/a" {
		t.Errorf("entries = %+v", store.entries)
	}

	store.fail = errors.New("disk on fire")
	transferLogger(log, logger)(ended)
	if !strings.Contains(logs.String(), "activity entry not written") || !strings.Contains(logs.String(), "disk on fire") {
		t.Errorf("log = %q; want a warning", logs.String())
	}
}
