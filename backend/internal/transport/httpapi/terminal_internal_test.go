package httpapi

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// TestCanceledTerminalValidation rejects a revoked context before looking up any session or permission.
func TestCanceledTerminalValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if (&server{}).terminalValid(ctx, authenticated{}, files.Target{}) {
		t.Fatal("accepted a canceled terminal")
	}
}

// TestIdleTerminalMonitor periodically verifies idle access and revokes it without waiting for another command.
func TestIdleTerminalMonitor(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var checks atomic.Int32
	done := make(chan struct{})
	go monitorTerminal(ctx, func() { cancel(); close(done) }, time.Millisecond, func() bool { return checks.Add(1) < 2 })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("idle monitor did not revoke expired access")
	}
	if checks.Load() != 2 {
		t.Fatal("monitor did not preserve then revoke authorization")
	}
	stopped, stop := context.WithCancel(t.Context())
	stop()
	monitorTerminal(stopped, func() { t.Fatal("revoked twice") }, time.Hour, func() bool { t.Fatal("verified a stopped terminal"); return true })
}

// TestCopyAdmissionProblem preserves a specific actionable code before the generic unsupported classification.
func TestCopyAdmissionProblem(t *testing.T) {
	problem, ok := asProblem(errors.Join(files.ErrCopyConnectionLimit, remotefs.ErrUnsupported))
	if !ok || problem.code != "copy_connection_limit" || problem.status != 422 {
		t.Fatalf("copy admission: %+v", problem)
	}
}
