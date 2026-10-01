package storetest

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/invite"
	"github.com/fprambs/transferly/internal/core/workspace"
)

// InviteStore is the persistence an implementation offers to the invite package, plus what the suite needs to set up.
type InviteStore interface {
	invite.Store
	workspace.Store
	auth.UserStore
}

// InviteFactory returns a new, empty store for one test.
type InviteFactory func(t *testing.T) InviteStore

// RunInvite executes the invitation store suite against stores built by newStore.
//
// Scenarios:
//   - an invitation is stored with the workspace name and the inviter, found by the hash of its token and listed as pending
//   - a second invitation for the same workspace and email replaces the first, and its token stops working
//   - expired invitations are neither found, counted nor listed, and purging removes them
//   - revoking deletes an invitation of that workspace only
//   - accepting consumes the invitation and creates the membership in one step, works once and keeps the role of an existing member
//   - the inviter can be deleted and the invitation stays with an empty inviter
func RunInvite(t *testing.T, newStore InviteFactory) {
	t.Helper()
	ctx := t.Context()

	type world struct {
		store      InviteStore
		owner, bob auth.User
		team       workspace.Workspace
	}
	setup := func(t *testing.T) world {
		t.Helper()
		store := newStore(t)
		w := world{store: store, owner: newUser("owner@example.com"), bob: newUser("bob@example.com")}
		for _, u := range []auth.User{w.owner, w.bob} {
			if err := store.CreateUser(ctx, u); err != nil {
				t.Fatalf("CreateUser() = %v", err)
			}
		}
		w.team = newWorkspace(w.owner.ID, workspace.KindTeam, "Ops")
		if err := store.CreateWorkspace(ctx, w.team); err != nil {
			t.Fatalf("CreateWorkspace() = %v", err)
		}
		return w
	}
	invitation := func(w world, email string, role workspace.Role) invite.Invitation {
		return invite.Invitation{ID: uuid.New(), WorkspaceID: w.team.ID, Email: email, Role: role, InvitedBy: w.owner.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	}

	t.Run("create and read back", func(t *testing.T) {
		w := setup(t)
		i := invitation(w, "bob@example.com", workspace.RoleEditor)
		if err := w.store.Create(ctx, i, []byte("hash-1")); err != nil {
			t.Fatalf("Create() = %v", err)
		}
		got, err := w.store.ByTokenHash(ctx, []byte("hash-1"), now)
		want := i
		want.WorkspaceName, want.InvitedByEmail = "Ops", "owner@example.com"
		if err != nil || got != want {
			t.Fatalf("ByTokenHash() = %+v, %v; want %+v", got, err, want)
		}
		if _, err := w.store.ByTokenHash(ctx, []byte("other"), now); !errors.Is(err, invite.ErrNotFound) {
			t.Fatalf("ByTokenHash(unknown) = %v; want ErrNotFound", err)
		}
		list, err := w.store.Pending(ctx, w.team.ID, now)
		if err != nil || len(list) != 1 || list[0].ID != i.ID {
			t.Fatalf("Pending() = %+v, %v", list, err)
		}
		if n, err := w.store.CountPending(ctx, w.team.ID, now); err != nil || n != 1 {
			t.Fatalf("CountPending() = %d, %v; want 1", n, err)
		}
	})

	t.Run("one invitation per email", func(t *testing.T) {
		w := setup(t)
		first, second := invitation(w, "bob@example.com", workspace.RoleViewer), invitation(w, "bob@example.com", workspace.RoleAdmin)
		for i, inv := range []invite.Invitation{first, second} {
			if err := w.store.Create(ctx, inv, []byte{byte(i)}); err != nil {
				t.Fatalf("Create() = %v", err)
			}
		}
		if _, err := w.store.ByTokenHash(ctx, []byte{0}, now); !errors.Is(err, invite.ErrNotFound) {
			t.Fatalf("replaced token = %v; want ErrNotFound", err)
		}
		if n, _ := w.store.CountPending(ctx, w.team.ID, now); n != 1 {
			t.Fatalf("pending = %d; want 1", n)
		}
	})

	t.Run("expiry", func(t *testing.T) {
		w := setup(t)
		if err := w.store.Create(ctx, invitation(w, "bob@example.com", workspace.RoleViewer), []byte("h")); err != nil {
			t.Fatal(err)
		}
		late := now.Add(2 * time.Hour)
		if _, err := w.store.ByTokenHash(ctx, []byte("h"), late); !errors.Is(err, invite.ErrNotFound) {
			t.Fatalf("expired ByTokenHash() = %v; want ErrNotFound", err)
		}
		if list, _ := w.store.Pending(ctx, w.team.ID, late); len(list) != 0 {
			t.Fatalf("expired Pending() = %+v; want none", list)
		}
		if n, _ := w.store.CountPending(ctx, w.team.ID, late); n != 0 {
			t.Fatalf("expired CountPending() = %d; want 0", n)
		}
		if n, err := w.store.PurgeExpired(ctx, now); err != nil || n != 0 {
			t.Fatalf("early PurgeExpired() = %d, %v; want 0", n, err)
		}
		if n, err := w.store.PurgeExpired(ctx, late); err != nil || n != 1 {
			t.Fatalf("PurgeExpired() = %d, %v; want 1", n, err)
		}
	})

	t.Run("revoke", func(t *testing.T) {
		w := setup(t)
		i := invitation(w, "bob@example.com", workspace.RoleViewer)
		if err := w.store.Create(ctx, i, []byte("h")); err != nil {
			t.Fatal(err)
		}
		if err := w.store.Revoke(ctx, uuid.New(), i.ID); !errors.Is(err, invite.ErrNotFound) {
			t.Fatalf("Revoke(other workspace) = %v; want ErrNotFound", err)
		}
		if err := w.store.Revoke(ctx, w.team.ID, i.ID); err != nil {
			t.Fatalf("Revoke() = %v", err)
		}
		if err := w.store.Revoke(ctx, w.team.ID, i.ID); !errors.Is(err, invite.ErrNotFound) {
			t.Fatalf("second Revoke() = %v; want ErrNotFound", err)
		}
	})

	t.Run("accept", func(t *testing.T) {
		w := setup(t)
		i := invitation(w, "bob@example.com", workspace.RoleEditor)
		if err := w.store.Create(ctx, i, []byte("h")); err != nil {
			t.Fatal(err)
		}
		if _, err := w.store.Accept(ctx, i.ID, w.bob.ID, now.Add(2*time.Hour)); !errors.Is(err, invite.ErrNotFound) {
			t.Fatalf("expired Accept() = %v; want ErrNotFound", err)
		}
		got, err := w.store.Accept(ctx, i.ID, w.bob.ID, now)
		if err != nil || got.Workspace != w.team || got.Role != workspace.RoleEditor {
			t.Fatalf("Accept() = %+v, %v; want %+v as editor", got, err, w.team)
		}
		if role, _ := w.store.RoleOf(ctx, w.team.ID, w.bob.ID); role != workspace.RoleEditor {
			t.Fatalf("role after Accept() = %q; want editor", role)
		}
		if _, err := w.store.Accept(ctx, i.ID, w.bob.ID, now); !errors.Is(err, invite.ErrNotFound) {
			t.Fatalf("second Accept() = %v; want ErrNotFound", err)
		}
		again := invitation(w, "bob@example.com", workspace.RoleAdmin)
		if err := w.store.Create(ctx, again, []byte("h2")); err != nil {
			t.Fatal(err)
		}
		if _, err := w.store.Accept(ctx, again.ID, w.bob.ID, now); err != nil {
			t.Fatalf("Accept() of an existing member = %v", err)
		}
		if role, _ := w.store.RoleOf(ctx, w.team.ID, w.bob.ID); role != workspace.RoleEditor {
			t.Fatalf("role of an existing member = %q; want it kept as editor", role)
		}
	})

	t.Run("inviter without an account", func(t *testing.T) {
		w := setup(t)
		i := invitation(w, "bob@example.com", workspace.RoleViewer)
		i.InvitedBy = uuid.Nil
		if err := w.store.Create(ctx, i, []byte("h")); err != nil {
			t.Fatalf("Create() = %v", err)
		}
		got, err := w.store.ByTokenHash(ctx, []byte("h"), now)
		if err != nil || got.InvitedBy != uuid.Nil || got.InvitedByEmail != "" {
			t.Fatalf("ByTokenHash() = %+v, %v; want an empty inviter", got, err)
		}
	})
}
