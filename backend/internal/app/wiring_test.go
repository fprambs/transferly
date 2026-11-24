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

	"github.com/fprambs/transferly/internal/adapters/clock"
	"github.com/fprambs/transferly/internal/config"
	"github.com/fprambs/transferly/internal/core/activity"
	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/limits"
	"github.com/fprambs/transferly/internal/core/transfer"
	"github.com/fprambs/transferly/internal/testutil"
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
//   - the key sweep and asked password sweep tasks are registered and report nothing to remove when empty
func TestNewWiring_RegistersJanitorTasks(t *testing.T) {
	w, err := newWiring(config.Config{SessionIdle: time.Minute, SessionMax: time.Hour}, slog.New(slog.DiscardHandler), nil, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}

	names := map[string]int{}
	for _, task := range w.janitor {
		if task.name == "idle quick connections" {
			if removed, err := task.run(t.Context()); removed != 0 || err != nil {
				t.Errorf("%s = %d, %v; want nothing removed", task.name, removed, err)
			}
		}
		if task.name == "expired vault keys" || task.name == "asked passwords" {
			if removed, err := task.run(t.Context()); removed != 0 || err != nil {
				t.Errorf("%s = %d, %v; want nothing removed", task.name, removed, err)
			}
		}
		names[task.name]++
	}
	if names["old activity"] != 1 || names["expired invitations"] != 1 || names["expired vault keys"] != 1 || names["expired sessions"] != 1 || names["idle quick connections"] != 1 || names["asked passwords"] != 1 || names["expired terminal approvals"] != 1 {
		t.Errorf("janitor tasks = %v; want sessions, terminal approvals, quick connections, vault keys, asked passwords, old activity and invitations once each", names)
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

// auditLog is an audit store that keeps what it is given, or fails.
type auditLog struct {
	events []audit.Event
	fail   error
}

// InsertAudit keeps the event or fails.
func (a *auditLog) InsertAudit(_ context.Context, e audit.Event) error {
	a.events = append(a.events, e)
	return a.fail
}

// AuditByWorkspace is not used here.
func (a *auditLog) AuditByWorkspace(context.Context, uuid.UUID, audit.Filter, int) ([]audit.Event, error) {
	return nil, nil
}

// AuditInstance is not used here.
func (a *auditLog) AuditInstance(context.Context, audit.Filter, int) ([]audit.Event, error) {
	return nil, nil
}

// TestRotationLogger verifies the hook that logs the rotations of workspace keys.
//
// Scenarios:
//   - a rotation is recorded with its workspace, actor and version
//   - a failure to record is logged and not returned
func TestRotationLogger(t *testing.T) {
	var out strings.Builder
	logger := slog.New(slog.NewTextHandler(&out, nil))
	store := &auditLog{}
	hook := rotationLogger(audit.NewService(store, nil, clock.System{}), logger)
	ws, actor := uuid.New(), uuid.New()

	hook(t.Context(), ws, actor, 3)
	if len(store.events) != 1 || store.events[0].WorkspaceID != ws || store.events[0].ActorID != actor || store.events[0].Action != audit.KeyRotated || store.events[0].Meta["version"] != "3" {
		t.Fatalf("events = %+v", store.events)
	}
	store.fail = errors.New("disk on fire")
	hook(t.Context(), ws, actor, 4)
	if !strings.Contains(out.String(), "audit event not recorded") || !strings.Contains(out.String(), "disk on fire") {
		t.Errorf("logs = %q; want the failure", out.String())
	}
}

// TestRelyingParty verifies deriving the WebAuthn relying party from the allowed origins.
//
// Scenarios:
//   - the host of the first origin is the relying party and every origin is kept
//   - without origins it is localhost, and an origin that cannot be parsed is used as it is
//   - a relying party the WebAuthn library refuses stops the composition of the services
func TestRelyingParty(t *testing.T) {
	host, origins := relyingParty([]string{"https://transferly.example:8443", "https://alias.example"})
	if host != "transferly.example" || len(origins) != 2 {
		t.Errorf("relyingParty() = %q, %v", host, origins)
	}
	if host, origins := relyingParty(nil); host != "localhost" || len(origins) != 1 {
		t.Errorf("relyingParty(nil) = %q, %v", host, origins)
	}
	if host, _ := relyingParty([]string{"://bad"}); host != "://bad" {
		t.Errorf("relyingParty(unparsable) = %q", host)
	}
	if _, err := newWiring(config.Config{AllowedOrigins: []string{"://bad"}}, slog.New(slog.DiscardHandler), nil, []byte("0123456789abcdef0123456789abcdef")); err == nil {
		t.Error("newWiring() accepted a relying party WebAuthn refuses")
	}
}

// TestStartingLimits verifies the limits the server starts with.
//
// Scenarios:
//   - every value of the configuration lands in its own field of the limits, concurrent transfers included
func TestStartingLimits(t *testing.T) {
	cfg := config.Config{
		TransferLimits: config.TransferLimits{Global: 1, PerWorkspace: 2, PerUser: 3, PerSite: 4},
		Quotas:         config.Quotas{BandwidthGlobal: 5, BandwidthPerUser: 6, MaxSitesPerWorkspace: 7, MaxTeamsPerUser: 8, MaxMembersPerTeam: 9, DailyBytesPerUser: 10},
	}
	want := limits.Limits{BandwidthGlobal: 5, BandwidthPerUser: 6, TransfersGlobal: 1, TransfersPerWorkspace: 2, TransfersPerUser: 3, TransfersPerSite: 4,
		MaxSitesPerWorkspace: 7, MaxTeamsPerUser: 8, MaxMembersPerTeam: 9, DailyBytesPerUser: 10}
	if got := startingLimits(cfg); got != want {
		t.Errorf("startingLimits() = %+v; want %+v", got, want)
	}
}

// TestOpenConnections covers [openConnections].
//
// Use cases:
//   - a site with no pool reports no open connection, so the sweep may drop a password not to be remembered
func TestOpenConnections(t *testing.T) {
	pool := connections.New(nil, testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), connections.Config{})
	t.Cleanup(pool.Close)
	if openConnections(pool)(uuid.New(), uuid.New(), uuid.New()) {
		t.Error("a site nobody connected to reports an open connection")
	}
}

// TestNewWiring_VaultRememberSessions verifies that the wiring builds the vault, also when it keeps the keys of sessions across
// restarts, as development asks (ADR-0028).
func TestNewWiring_VaultRememberSessions(t *testing.T) {
	for _, remember := range []bool{false, true} {
		w, err := newWiring(config.Config{SessionIdle: time.Minute, SessionMax: time.Hour, VaultRememberSessions: remember}, slog.New(slog.DiscardHandler), nil, []byte("0123456789abcdef0123456789abcdef"))
		if err != nil || w.deps.Vault == nil {
			t.Errorf("remember %v: newWiring() = %v with vault %v", remember, err, w.deps.Vault)
		}
	}
}

// TestPersistenceLogger verifies that a store failure is visible to instance operators.
func TestPersistenceLogger(t *testing.T) {
	var logs bytes.Buffer
	persistenceLogger(slog.New(slog.NewTextHandler(&logs, nil)))(errors.New("database unavailable"))
	if !strings.Contains(logs.String(), "transfer persistence failed") {
		t.Fatalf("missing diagnostic: %s", logs.String())
	}
}
