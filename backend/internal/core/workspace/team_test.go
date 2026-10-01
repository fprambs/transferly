package workspace_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/testutil"
)

// team is a team workspace with one member of every role.
type team struct {
	svc                                 *workspace.Service
	store                               *fakeStore
	id                                  uuid.UUID
	owner, admin, editor, viewer, other uuid.UUID
}

// newTeam creates a team owned by a user and adds an admin, an editor and a viewer.
func newTeam(t *testing.T) team {
	t.Helper()
	svc, store := newService()
	tm := team{svc: svc, store: store, owner: uuid.New(), admin: uuid.New(), editor: uuid.New(), viewer: uuid.New(), other: uuid.New()}
	created, err := svc.NewTeam(tm.owner, "  Acme Web  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWorkspace(context.Background(), created); err != nil {
		t.Fatal(err)
	}
	tm.id = created.ID
	for user, role := range map[uuid.UUID]workspace.Role{tm.admin: workspace.RoleAdmin, tm.editor: workspace.RoleEditor, tm.viewer: workspace.RoleViewer} {
		store.roles[[2]uuid.UUID{tm.id, user}] = role
	}
	return tm
}

// role reads the role of a user in the team.
func (tm team) role(user uuid.UUID) workspace.Role { return tm.store.roles[[2]uuid.UUID{tm.id, user}] }

// TestService_NewTeam verifies preparing team workspaces.
//
// Scenarios:
//   - a team is prepared with a trimmed name, the team kind, the actor as owner and the clock time
//   - empty, blank, too long and control-character names are refused, and a name of 80 multibyte characters is accepted
//   - an identifier failure is returned
func TestService_NewTeam(t *testing.T) {
	tm := newTeam(t)
	created := tm.store.workspaces[0]
	if created.Name != "Acme Web" || created.Kind != workspace.KindTeam || created.OwnerID != tm.owner || created.CreatedAt.IsZero() {
		t.Errorf("team = %+v", created)
	}
	for _, name := range []string{"", "   ", strings.Repeat("x", 81), "bad\x00name", "line\nbreak"} {
		if _, err := tm.svc.NewTeam(tm.owner, name); !errors.Is(err, workspace.ErrInvalidName) {
			t.Errorf("NewTeam(%q) = %v; want ErrInvalidName", name, err)
		}
	}
	if _, err := tm.svc.NewTeam(tm.owner, strings.Repeat("é", 80)); err != nil {
		t.Errorf("an 80 character name was refused: %v", err)
	}
	failing := workspace.NewService(tm.store, testutil.NewFakeClock(time.Now()), func() (uuid.UUID, error) { return uuid.Nil, errBoom })
	if _, err := failing.NewTeam(tm.owner, "x"); !errors.Is(err, errBoom) {
		t.Errorf("id failure = %v", err)
	}
}

// TestService_Get verifies reading one workspace.
//
// Scenarios:
//   - a member gets the workspace with their role, and a stranger gets ErrNotFound
//   - a store failure is returned
func TestService_Get(t *testing.T) {
	ctx := context.Background()
	tm := newTeam(t)
	got, err := tm.svc.Get(ctx, tm.viewer, tm.id)
	if err != nil || got.ID != tm.id || got.Role != workspace.RoleViewer || got.Kind != workspace.KindTeam {
		t.Fatalf("Get() = %+v, %v", got, err)
	}
	if _, err := tm.svc.Get(ctx, tm.other, tm.id); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger = %v; want ErrNotFound", err)
	}
	tm.store.fail = map[string]error{"workspace": errBoom}
	if _, err := tm.svc.Get(ctx, tm.owner, tm.id); !errors.Is(err, errBoom) {
		t.Errorf("failure = %v", err)
	}
}

// TestService_Members verifies who may read the member list.
//
// Scenarios:
//   - every role may list a team, and a stranger gets ErrNotFound
//   - a personal workspace has no member list
//   - failures of the workspace lookup and of the list are returned
func TestService_Members(t *testing.T) {
	ctx := context.Background()
	tm := newTeam(t)
	for _, user := range []uuid.UUID{tm.owner, tm.admin, tm.editor, tm.viewer} {
		if _, err := tm.svc.Members(ctx, user, tm.id); err != nil {
			t.Errorf("Members() = %v", err)
		}
	}
	if _, err := tm.svc.Members(ctx, tm.other, tm.id); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger = %v; want ErrNotFound", err)
	}
	personal, err := tm.svc.EnsurePersonal(ctx, tm.other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.Members(ctx, tm.other, personal.ID); !errors.Is(err, workspace.ErrPersonal) {
		t.Errorf("personal = %v; want ErrPersonal", err)
	}
	tm.store.fail = map[string]error{"workspace": errBoom}
	if _, err := tm.svc.Members(ctx, tm.owner, tm.id); !errors.Is(err, errBoom) {
		t.Errorf("workspace failure = %v", err)
	}
	tm.store.fail = map[string]error{"members": errBoom}
	if _, err := tm.svc.Members(ctx, tm.owner, tm.id); !errors.Is(err, errBoom) {
		t.Errorf("members failure = %v", err)
	}
}

// TestService_SetRole verifies role changes.
//
// Scenarios:
//   - the owner may make an editor an admin, and an admin may make a viewer an editor
//   - an admin cannot grant admin or owner, nor touch another admin or the owner
//   - editors and viewers cannot change roles, and strangers get ErrNotFound
//   - unknown roles, oneself and non members are refused
//   - repeating the current role writes nothing, and store failures are returned
func TestService_SetRole(t *testing.T) {
	ctx := context.Background()
	tm := newTeam(t)
	if err := tm.svc.SetRole(ctx, tm.owner, tm.id, tm.editor, workspace.RoleAdmin); err != nil || tm.role(tm.editor) != workspace.RoleAdmin {
		t.Fatalf("owner promotes = %v, role %s", err, tm.role(tm.editor))
	}
	tm.store.roles[[2]uuid.UUID{tm.id, tm.editor}] = workspace.RoleEditor
	if err := tm.svc.SetRole(ctx, tm.admin, tm.id, tm.viewer, workspace.RoleEditor); err != nil || tm.role(tm.viewer) != workspace.RoleEditor {
		t.Fatalf("admin promotes = %v, role %s", err, tm.role(tm.viewer))
	}
	cases := []struct {
		name          string
		actor, target uuid.UUID
		role          workspace.Role
		want          error
	}{
		{"admin grants admin", tm.admin, tm.viewer, workspace.RoleAdmin, workspace.ErrInvalidRole},
		{"anyone grants owner", tm.owner, tm.viewer, workspace.RoleOwner, workspace.ErrInvalidRole},
		{"unknown role", tm.owner, tm.viewer, "boss", workspace.ErrInvalidRole},
		{"admin changes the owner", tm.admin, tm.owner, workspace.RoleViewer, workspace.ErrOwnerBound},
		{"owner changes oneself", tm.owner, tm.owner, workspace.RoleAdmin, workspace.ErrOwnerBound},
		{"admin changes oneself", tm.admin, tm.admin, workspace.RoleViewer, workspace.ErrInvalidRole},
		{"editor changes roles", tm.editor, tm.viewer, workspace.RoleViewer, workspace.ErrForbidden},
		{"viewer changes roles", tm.viewer, tm.editor, workspace.RoleViewer, workspace.ErrForbidden},
		{"stranger", tm.other, tm.viewer, workspace.RoleViewer, workspace.ErrNotFound},
		{"non member target", tm.owner, tm.other, workspace.RoleViewer, workspace.ErrMemberNotFound},
	}
	for _, c := range cases {
		if err := tm.svc.SetRole(ctx, c.actor, tm.id, c.target, c.role); !errors.Is(err, c.want) {
			t.Errorf("%s = %v; want %v", c.name, err, c.want)
		}
	}
	other := newTeam(t)
	other.store.roles[[2]uuid.UUID{other.id, other.admin}] = workspace.RoleAdmin
	if err := other.svc.SetRole(ctx, other.owner, other.id, other.admin, workspace.RoleAdmin); err != nil {
		t.Errorf("repeating a role = %v", err)
	}
	other.store.fail = map[string]error{"roleOf:" + other.viewer.String(): errBoom}
	if err := other.svc.SetRole(ctx, other.owner, other.id, other.viewer, workspace.RoleEditor); !errors.Is(err, errBoom) {
		t.Errorf("lookup failure = %v", err)
	}
	other.store.fail = map[string]error{"setRole": errBoom}
	if err := other.svc.SetRole(ctx, other.owner, other.id, other.viewer, workspace.RoleEditor); !errors.Is(err, errBoom) {
		t.Errorf("write failure = %v", err)
	}
}

// TestService_RemoveMember verifies removals and leaving.
//
// Scenarios:
//   - the owner removes an admin and an admin removes an editor, while an admin cannot remove another admin or the owner
//   - editors and viewers cannot remove anyone, and removing a non member or oneself is refused
//   - a member leaves, and the owner cannot leave
//   - store failures are returned
func TestService_RemoveMember(t *testing.T) {
	ctx := context.Background()
	tm := newTeam(t)
	if err := tm.svc.RemoveMember(ctx, tm.admin, tm.id, tm.editor); err != nil || tm.role(tm.editor) != "" {
		t.Fatalf("admin removes editor = %v", err)
	}
	if err := tm.svc.RemoveMember(ctx, tm.owner, tm.id, tm.admin); err != nil || tm.role(tm.admin) != "" {
		t.Fatalf("owner removes admin = %v", err)
	}
	tm = newTeam(t)
	tm.store.roles[[2]uuid.UUID{tm.id, tm.editor}] = workspace.RoleAdmin
	for name, c := range map[string]struct {
		actor, target uuid.UUID
		want          error
	}{
		"admin removes admin":  {tm.admin, tm.editor, workspace.ErrForbidden},
		"admin removes owner":  {tm.admin, tm.owner, workspace.ErrOwnerBound},
		"viewer removes":       {tm.viewer, tm.admin, workspace.ErrForbidden},
		"stranger":             {tm.other, tm.admin, workspace.ErrNotFound},
		"non member":           {tm.owner, tm.other, workspace.ErrMemberNotFound},
		"admin removes itself": {tm.admin, tm.admin, workspace.ErrInvalidRole},
	} {
		if err := tm.svc.RemoveMember(ctx, c.actor, tm.id, c.target); !errors.Is(err, c.want) {
			t.Errorf("%s = %v; want %v", name, err, c.want)
		}
	}
	if err := tm.svc.Leave(ctx, tm.viewer, tm.id); err != nil || tm.role(tm.viewer) != "" {
		t.Errorf("leave = %v", err)
	}
	if err := tm.svc.Leave(ctx, tm.owner, tm.id); !errors.Is(err, workspace.ErrOwnerBound) {
		t.Errorf("owner leaves = %v; want ErrOwnerBound", err)
	}
	if err := tm.svc.Leave(ctx, tm.other, tm.id); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger leaves = %v; want ErrNotFound", err)
	}
	tm.store.fail = map[string]error{"remove": errBoom}
	if err := tm.svc.Leave(ctx, tm.admin, tm.id); !errors.Is(err, errBoom) {
		t.Errorf("leave failure = %v", err)
	}
	if err := tm.svc.RemoveMember(ctx, tm.owner, tm.id, tm.admin); !errors.Is(err, errBoom) {
		t.Errorf("remove failure = %v", err)
	}
}

// TestService_TransferOwnership verifies handing a team over.
//
// Scenarios:
//   - the owner makes an admin the owner and becomes an admin
//   - admins cannot transfer, and the owner cannot transfer to oneself or to a non member
//   - a personal workspace cannot be transferred
//   - store failures are returned
func TestService_TransferOwnership(t *testing.T) {
	ctx := context.Background()
	tm := newTeam(t)
	if err := tm.svc.TransferOwnership(ctx, tm.owner, tm.id, tm.admin); err != nil || tm.role(tm.admin) != workspace.RoleOwner || tm.role(tm.owner) != workspace.RoleAdmin {
		t.Fatalf("transfer = %v, roles %s %s", err, tm.role(tm.admin), tm.role(tm.owner))
	}
	tm = newTeam(t)
	for name, c := range map[string]struct {
		actor, target uuid.UUID
		want          error
	}{
		"admin transfers": {tm.admin, tm.editor, workspace.ErrForbidden},
		"to oneself":      {tm.owner, tm.owner, workspace.ErrOwnerBound},
		"to a non member": {tm.owner, tm.other, workspace.ErrMemberNotFound},
		"stranger":        {tm.other, tm.admin, workspace.ErrNotFound},
	} {
		if err := tm.svc.TransferOwnership(ctx, c.actor, tm.id, c.target); !errors.Is(err, c.want) {
			t.Errorf("%s = %v; want %v", name, err, c.want)
		}
	}
	personal, _ := tm.svc.EnsurePersonal(ctx, tm.other)
	if err := tm.svc.TransferOwnership(ctx, tm.other, personal.ID, tm.owner); !errors.Is(err, workspace.ErrPersonal) {
		t.Errorf("personal = %v; want ErrPersonal", err)
	}
	tm.store.fail = map[string]error{"roleOf:" + tm.admin.String(): errBoom}
	if err := tm.svc.TransferOwnership(ctx, tm.owner, tm.id, tm.admin); !errors.Is(err, errBoom) {
		t.Errorf("lookup failure = %v", err)
	}
	tm.store.fail = map[string]error{"transfer": errBoom}
	if err := tm.svc.TransferOwnership(ctx, tm.owner, tm.id, tm.admin); !errors.Is(err, errBoom) {
		t.Errorf("write failure = %v", err)
	}
}

// TestService_Invitable verifies who may invite with which role.
//
// Scenarios:
//   - the owner may invite admins, editors and viewers, and an admin may invite editors and viewers
//   - nobody may invite an owner, an unknown role or a role that does not rank below their own
//   - editors, viewers and strangers cannot invite, and a personal workspace cannot be invited to
func TestService_Invitable(t *testing.T) {
	ctx := context.Background()
	tm := newTeam(t)
	for _, c := range []struct {
		actor uuid.UUID
		role  workspace.Role
		want  error
	}{
		{tm.owner, workspace.RoleAdmin, nil},
		{tm.owner, workspace.RoleViewer, nil},
		{tm.admin, workspace.RoleEditor, nil},
		{tm.admin, workspace.RoleAdmin, workspace.ErrInvalidRole},
		{tm.owner, workspace.RoleOwner, workspace.ErrInvalidRole},
		{tm.owner, "boss", workspace.ErrInvalidRole},
		{tm.editor, workspace.RoleViewer, workspace.ErrForbidden},
		{tm.viewer, workspace.RoleViewer, workspace.ErrForbidden},
		{tm.other, workspace.RoleViewer, workspace.ErrNotFound},
	} {
		w, err := tm.svc.Invitable(ctx, c.actor, tm.id, c.role)
		if !errors.Is(err, c.want) || (err == nil && w.ID != tm.id) {
			t.Errorf("Invitable(%s) = %+v, %v; want %v", c.role, w, err, c.want)
		}
	}
	personal, _ := tm.svc.EnsurePersonal(ctx, tm.other)
	if _, err := tm.svc.Invitable(ctx, tm.other, personal.ID, workspace.RoleViewer); !errors.Is(err, workspace.ErrPersonal) {
		t.Errorf("personal = %v; want ErrPersonal", err)
	}
}
