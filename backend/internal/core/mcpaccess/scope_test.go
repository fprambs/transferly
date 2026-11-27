package mcpaccess

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/workspace"
)

// TestScope checks snapshot isolation, cross-workspace requests and loss of current access.
func TestScope(t *testing.T) {
	user := uuid.New()
	allowed := Target{WorkspaceID: uuid.New(), SiteID: uuid.New()}
	outside := Target{WorkspaceID: uuid.New(), SiteID: allowed.SiteID}
	selected := []Target{allowed}
	scope := NewScope(user, selected)
	selected[0] = outside

	calls := 0
	denied := false
	check := func(_ context.Context, actor uuid.UUID, target Target, action workspace.Action) error {
		calls++
		if actor != user || target != allowed || action != workspace.ActionBrowse {
			t.Fatal("unexpected authorization request")
		}
		if denied {
			return errors.New("private connector detail")
		}
		return nil
	}

	if err := scope.Authorize(t.Context(), outside, check); err != ErrUnavailable || calls != 0 {
		t.Fatalf("outside scope: %v, permission checks: %d", err, calls)
	}
	if err := scope.Authorize(t.Context(), allowed, check); err != nil {
		t.Fatalf("approved site: %v", err)
	}
	denied = true
	if err := scope.Authorize(t.Context(), allowed, check); err != ErrUnavailable || calls != 2 {
		t.Fatalf("revoked access: %v, permission checks: %d", err, calls)
	}
}
