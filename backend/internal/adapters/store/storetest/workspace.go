package storetest

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// WorkspaceStore is the persistence an implementation offers to the workspace package, plus user creation.
type WorkspaceStore interface {
	workspace.Store
	auth.UserStore
}

// WorkspaceFactory returns a new, empty store for one test.
type WorkspaceFactory func(t *testing.T) WorkspaceStore

// newWorkspace returns a workspace of the given kind and owner.
func newWorkspace(owner uuid.UUID, kind workspace.Kind, name string) workspace.Workspace {
	return workspace.Workspace{ID: uuid.New(), Name: name, Kind: kind, OwnerID: owner, CreatedAt: now}
}

// RunWorkspace executes the workspace store suite against stores built by newStore.
//
// Scenarios:
//   - creating a workspace stores it with an owner membership and UTC times
//   - a second personal workspace of the same owner is refused with ErrExists, team workspaces are not limited
//   - personal workspace lookup finds the right one and reports ErrNotFound for users without one
//   - roles are reported per member and missing memberships, including foreign workspaces, are ErrNotFound
//   - listing returns only the workspaces of the user, with their role, personal first
func RunWorkspace(t *testing.T, newStore WorkspaceFactory) {
	t.Helper()
	ctx := t.Context()

	setup := func(t *testing.T) (WorkspaceStore, auth.User, auth.User) {
		t.Helper()
		store := newStore(t)
		alice, bob := newUser("alice@example.com"), newUser("bob@example.com")
		for _, u := range []auth.User{alice, bob} {
			if err := store.CreateUser(ctx, u); err != nil {
				t.Fatalf("CreateUser() = %v", err)
			}
		}
		return store, alice, bob
	}

	t.Run("create and read back", func(t *testing.T) {
		store, alice, _ := setup(t)
		want := newWorkspace(alice.ID, workspace.KindPersonal, "Personal")
		if err := store.CreateWorkspace(ctx, want); err != nil {
			t.Fatalf("CreateWorkspace() = %v", err)
		}
		got, err := store.PersonalWorkspace(ctx, alice.ID)
		if err != nil || got != want {
			t.Fatalf("PersonalWorkspace() = %+v, %v; want %+v", got, err, want)
		}
		role, err := store.RoleOf(ctx, want.ID, alice.ID)
		if err != nil || role != workspace.RoleOwner {
			t.Fatalf("RoleOf() = %q, %v; want owner", role, err)
		}
	})

	t.Run("one personal workspace per user", func(t *testing.T) {
		store, alice, _ := setup(t)
		if err := store.CreateWorkspace(ctx, newWorkspace(alice.ID, workspace.KindPersonal, "Personal")); err != nil {
			t.Fatalf("CreateWorkspace() = %v", err)
		}
		err := store.CreateWorkspace(ctx, newWorkspace(alice.ID, workspace.KindPersonal, "Again"))
		if !errors.Is(err, workspace.ErrExists) {
			t.Fatalf("second personal = %v; want ErrExists", err)
		}
		if err := store.CreateWorkspace(ctx, newWorkspace(alice.ID, workspace.KindTeam, "Team A")); err != nil {
			t.Fatalf("team workspace = %v", err)
		}
		if err := store.CreateWorkspace(ctx, newWorkspace(alice.ID, workspace.KindTeam, "Team B")); err != nil {
			t.Fatalf("second team workspace = %v", err)
		}
	})

	t.Run("missing personal workspace", func(t *testing.T) {
		store, alice, _ := setup(t)
		if _, err := store.PersonalWorkspace(ctx, alice.ID); !errors.Is(err, workspace.ErrNotFound) {
			t.Fatalf("PersonalWorkspace() = %v; want ErrNotFound", err)
		}
	})

	t.Run("roles of non members", func(t *testing.T) {
		store, alice, bob := setup(t)
		w := newWorkspace(alice.ID, workspace.KindPersonal, "Personal")
		if err := store.CreateWorkspace(ctx, w); err != nil {
			t.Fatalf("CreateWorkspace() = %v", err)
		}
		if _, err := store.RoleOf(ctx, w.ID, bob.ID); !errors.Is(err, workspace.ErrNotFound) {
			t.Fatalf("RoleOf(non member) = %v; want ErrNotFound", err)
		}
		if _, err := store.RoleOf(ctx, uuid.New(), alice.ID); !errors.Is(err, workspace.ErrNotFound) {
			t.Fatalf("RoleOf(unknown workspace) = %v; want ErrNotFound", err)
		}
	})

	t.Run("listing", func(t *testing.T) {
		store, alice, bob := setup(t)
		team := newWorkspace(alice.ID, workspace.KindTeam, "Ops")
		personal := newWorkspace(alice.ID, workspace.KindPersonal, "Personal")
		foreign := newWorkspace(bob.ID, workspace.KindPersonal, "Personal")
		for _, w := range []workspace.Workspace{team, personal, foreign} {
			if err := store.CreateWorkspace(ctx, w); err != nil {
				t.Fatalf("CreateWorkspace() = %v", err)
			}
		}
		got, err := store.WorkspacesForUser(ctx, alice.ID)
		if err != nil || len(got) != 2 {
			t.Fatalf("WorkspacesForUser() = %+v, %v; want 2 entries", got, err)
		}
		if got[0].ID != personal.ID || got[1].ID != team.ID || got[0].Role != workspace.RoleOwner {
			t.Fatalf("WorkspacesForUser() order or role wrong: %+v", got)
		}
		none, err := store.WorkspacesForUser(ctx, uuid.New())
		if err != nil || len(none) != 0 {
			t.Fatalf("WorkspacesForUser(stranger) = %+v, %v; want empty", none, err)
		}
	})
}
