package audit_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/audit"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/testutil"
)

// errBoom is the failure injected by the fakes.
var errBoom = errors.New("boom")

// fakeStore keeps events in memory.
type fakeStore struct {
	events []audit.Event
	fail   error
	seen   audit.Filter
	limit  int
}

// InsertAudit stores an event.
func (f *fakeStore) InsertAudit(_ context.Context, e audit.Event) error {
	if f.fail != nil {
		return f.fail
	}
	f.events = append(f.events, e)
	return nil
}

// AuditByWorkspace records the arguments and returns the stored events.
func (f *fakeStore) AuditByWorkspace(_ context.Context, _ uuid.UUID, filter audit.Filter, limit int) ([]audit.Event, error) {
	f.seen, f.limit = filter, limit
	return f.events, f.fail
}

// authz allows the admin only.
type authz struct{ admin uuid.UUID }

// Authorize follows the rules of the workspace service for the two roles of the test.
func (a authz) Authorize(_ context.Context, user, _ uuid.UUID, _ workspace.Action) (workspace.Role, error) {
	if user == a.admin {
		return workspace.RoleAdmin, nil
	}
	return "", workspace.ErrForbidden
}

// TestService verifies recording and listing.
//
// Scenarios:
//   - an event is stamped with the clock and stored, and a failure to store is returned
//   - admins list with the default limit, a given limit and the maximum, and the filter reaches the store
//   - members who cannot manage the workspace are refused, and filters that are not words are refused
//   - a failing store is returned
func TestService(t *testing.T) {
	ctx := context.Background()
	admin, ws := uuid.New(), uuid.New()
	store := &fakeStore{}
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	svc := audit.NewService(store, authz{admin}, testutil.NewFakeClock(now))

	if err := svc.Record(ctx, audit.Event{WorkspaceID: ws, ActorID: admin, Action: audit.MemberRemoved, Target: "a@example.com"}); err != nil || !store.events[0].At.Equal(now) {
		t.Fatalf("Record() = %v, %+v", err, store.events)
	}
	for limit, want := range map[int]int{0: 100, 7: 7, 100000: 500} {
		if _, err := svc.List(ctx, admin, ws, audit.Filter{Action: "member.", Before: 9}, limit); err != nil || store.limit != want || store.seen != (audit.Filter{Action: "member.", Before: 9}) {
			t.Errorf("List(limit %d) = %v, limit %d, filter %+v", limit, err, store.limit, store.seen)
		}
	}
	if _, err := svc.List(ctx, uuid.New(), ws, audit.Filter{}, 0); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("a member who cannot manage = %v; want ErrForbidden", err)
	}
	for _, filter := range []audit.Filter{{Action: "Member"}, {Action: "a b"}, {Action: strings.Repeat("a", 41)}, {Before: -1}} {
		if _, err := svc.List(ctx, admin, ws, filter, 0); !errors.Is(err, audit.ErrInvalidFilter) {
			t.Errorf("List(%+v) = %v; want ErrInvalidFilter", filter, err)
		}
	}
	store.fail = errBoom
	if err := svc.Record(ctx, audit.Event{}); !errors.Is(err, errBoom) {
		t.Errorf("Record() failure = %v", err)
	}
	if _, err := svc.List(ctx, admin, ws, audit.Filter{}, 0); !errors.Is(err, errBoom) {
		t.Errorf("List() failure = %v", err)
	}
}
