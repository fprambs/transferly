package workspace_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/testutil"
)

// errBoom is the error injected by the fake store.
var errBoom = errors.New("boom")

// fakeStore is an in-memory workspace.Store with switchable failures.
type fakeStore struct {
	workspaces []workspace.Workspace
	roles      map[[2]uuid.UUID]workspace.Role
	failFind   bool
	failCreate error
	racing     *workspace.Workspace
}

// CreateWorkspace stores the workspace or returns the injected error.
func (f *fakeStore) CreateWorkspace(_ context.Context, w workspace.Workspace) error {
	if f.racing != nil {
		f.workspaces = append(f.workspaces, *f.racing)
		f.racing = nil
		return workspace.ErrExists
	}
	if f.failCreate != nil {
		return f.failCreate
	}
	f.workspaces = append(f.workspaces, w)
	f.roles[[2]uuid.UUID{w.ID, w.OwnerID}] = workspace.RoleOwner
	return nil
}

// PersonalWorkspace finds the personal workspace of a user.
func (f *fakeStore) PersonalWorkspace(_ context.Context, userID uuid.UUID) (workspace.Workspace, error) {
	if f.failFind {
		return workspace.Workspace{}, errBoom
	}
	for _, w := range f.workspaces {
		if w.OwnerID == userID && w.Kind == workspace.KindPersonal {
			return w, nil
		}
	}
	return workspace.Workspace{}, workspace.ErrNotFound
}

// WorkspacesForUser lists the workspaces where the user has a role.
func (f *fakeStore) WorkspacesForUser(_ context.Context, userID uuid.UUID) ([]workspace.Summary, error) {
	var out []workspace.Summary
	for _, w := range f.workspaces {
		if role, ok := f.roles[[2]uuid.UUID{w.ID, userID}]; ok {
			out = append(out, workspace.Summary{Workspace: w, Role: role})
		}
	}
	return out, nil
}

// RoleOf returns the role of a user in a workspace.
func (f *fakeStore) RoleOf(_ context.Context, workspaceID, userID uuid.UUID) (workspace.Role, error) {
	role, ok := f.roles[[2]uuid.UUID{workspaceID, userID}]
	if !ok {
		return "", workspace.ErrNotFound
	}
	return role, nil
}

// newService returns a service over a fresh fake store.
func newService() (*workspace.Service, *fakeStore) {
	store := &fakeStore{roles: map[[2]uuid.UUID]workspace.Role{}}
	clock := testutil.NewFakeClock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	return workspace.NewService(store, clock, nil), store
}

// TestCan verifies the permission matrix, cell by cell, including unknown roles and actions.
//
// Scenarios:
//   - every role and action pair matches the documented matrix
//   - unknown roles and unknown actions are denied
func TestCan(t *testing.T) {
	roles := []workspace.Role{workspace.RoleViewer, workspace.RoleEditor, workspace.RoleAdmin, workspace.RoleOwner}
	matrix := map[workspace.Action][4]bool{
		workspace.ActionBrowse:          {true, true, true, true},
		workspace.ActionDownload:        {true, true, true, true},
		workspace.ActionUpload:          {false, true, true, true},
		workspace.ActionModify:          {false, true, true, true},
		workspace.ActionManageSites:     {false, true, true, true},
		workspace.ActionManageMembers:   {false, false, true, true},
		workspace.ActionManageWorkspace: {false, false, false, true},
	}
	for action, row := range matrix {
		for i, role := range roles {
			if got := workspace.Can(role, action); got != row[i] {
				t.Errorf("Can(%s, %s) = %v; want %v", role, action, got, row[i])
			}
		}
	}
	if workspace.Can("stranger", workspace.ActionBrowse) || workspace.Can(workspace.RoleOwner, "fly") {
		t.Error("unknown role or action was allowed")
	}
}

// TestService_EnsurePersonal verifies personal workspace creation.
//
// Scenarios:
//   - a missing workspace is created with the clock time and an owner membership
//   - a second call returns the same workspace without creating another
//   - a lookup failure is returned
//   - an id generation failure is returned
//   - a creation failure is returned
//   - losing a creation race returns the workspace created by the winner
func TestService_EnsurePersonal(t *testing.T) {
	ctx := context.Background()
	user := uuid.New()

	t.Run("creates once", func(t *testing.T) {
		svc, store := newService()
		first, err := svc.EnsurePersonal(ctx, user)
		if err != nil || first.Kind != workspace.KindPersonal || first.OwnerID != user || first.CreatedAt.IsZero() {
			t.Fatalf("EnsurePersonal() = %+v, %v", first, err)
		}
		again, err := svc.EnsurePersonal(ctx, user)
		if err != nil || again.ID != first.ID || len(store.workspaces) != 1 {
			t.Fatalf("second EnsurePersonal() = %+v, %v with %d stored", again, err, len(store.workspaces))
		}
	})

	t.Run("lookup failure", func(t *testing.T) {
		svc, store := newService()
		store.failFind = true
		if _, err := svc.EnsurePersonal(ctx, user); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v; want boom", err)
		}
	})

	t.Run("id failure", func(t *testing.T) {
		store := &fakeStore{roles: map[[2]uuid.UUID]workspace.Role{}}
		svc := workspace.NewService(store, testutil.NewFakeClock(time.Now()), func() (uuid.UUID, error) { return uuid.Nil, errBoom })
		if _, err := svc.EnsurePersonal(ctx, user); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v; want boom", err)
		}
	})

	t.Run("creation failure", func(t *testing.T) {
		svc, store := newService()
		store.failCreate = errBoom
		if _, err := svc.EnsurePersonal(ctx, user); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v; want boom", err)
		}
	})

	t.Run("lost race", func(t *testing.T) {
		svc, store := newService()
		winner := workspace.Workspace{ID: uuid.New(), Kind: workspace.KindPersonal, OwnerID: user}
		store.racing = &winner
		got, err := svc.EnsurePersonal(ctx, user)
		if err != nil || got.ID != winner.ID {
			t.Fatalf("EnsurePersonal() = %+v, %v; want the winner", got, err)
		}
	})
}

// TestService_AuthorizeAndList verifies membership checks and listing.
//
// Scenarios:
//   - a member with permission gets their role
//   - a member without permission gets ErrForbidden
//   - a non member gets ErrNotFound, the same as an unknown workspace
//   - listing returns the workspaces of the user
func TestService_AuthorizeAndList(t *testing.T) {
	ctx := context.Background()
	svc, store := newService()
	owner, viewer, stranger := uuid.New(), uuid.New(), uuid.New()
	w, err := svc.EnsurePersonal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	store.roles[[2]uuid.UUID{w.ID, viewer}] = workspace.RoleViewer

	if role, err := svc.Authorize(ctx, owner, w.ID, workspace.ActionManageWorkspace); err != nil || role != workspace.RoleOwner {
		t.Errorf("owner = %q, %v", role, err)
	}
	if _, err := svc.Authorize(ctx, viewer, w.ID, workspace.ActionUpload); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer upload = %v; want ErrForbidden", err)
	}
	if _, err := svc.Authorize(ctx, stranger, w.ID, workspace.ActionBrowse); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger = %v; want ErrNotFound", err)
	}
	if _, err := svc.Authorize(ctx, owner, uuid.New(), workspace.ActionBrowse); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("unknown workspace = %v; want ErrNotFound", err)
	}
	list, err := svc.List(ctx, owner)
	if err != nil || len(list) != 1 || list[0].Role != workspace.RoleOwner {
		t.Errorf("List() = %+v, %v", list, err)
	}
}
