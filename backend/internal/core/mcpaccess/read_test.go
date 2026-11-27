package mcpaccess

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/workspace"
)

// TestScopedList verifies that a forbidden or canceled call cannot access or return remote metadata.
func TestScopedList(t *testing.T) {
	registry := NewUnlocks(&unlockClock{}, time.Hour, time.Minute)
	link := uuid.New()
	target := Target{WorkspaceID: uuid.New(), SiteID: uuid.New()}
	scope := NewScope(uuid.New(), []Target{target})
	window := registry.Open(t.Context(), link)
	t.Cleanup(func() { registry.Lock(link) })
	check := func(context.Context, uuid.UUID, Target, workspace.Action) error { return nil }
	reads := 0
	block := false
	read := func(ctx context.Context, selected Target) ([]remotefs.Entry, error) {
		reads++
		if selected != target {
			t.Fatal("read reached the wrong target")
		}
		if block {
			registry.Lock(link)
			<-ctx.Done()
		}
		return []remotefs.Entry{{Name: "private marker"}}, nil
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		ctx    context.Context
		target Target
		window *Window
	}{
		{t.Context(), target, nil},
		{t.Context(), Target{}, window},
		{canceled, target, window},
	} {
		if result, err := registry.List(test.ctx, link, test.window, scope, test.target, check, read); err != ErrUnavailable || len(result.Entries) != 0 || reads != 0 {
			t.Fatal("a denied request reached the remote reader")
		}
	}
	result, err := registry.List(t.Context(), link, window, scope, target, check, read)
	if err != nil || len(result.Entries) != 1 || reads != 1 {
		t.Fatalf("approved request: %v", err)
	}
	block = true
	result, err = registry.List(t.Context(), link, window, scope, target, check, read)
	if err != ErrUnavailable || len(result.Entries) != 0 || reads != 2 {
		t.Fatal("locking the grant did not discard the pending response")
	}
}
