package invite_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/invite"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/testutil"
)

// errBoom is the failure injected by the fakes.
var errBoom = errors.New("boom")

// fakeStore keeps invitations in memory, keyed by token hash.
type fakeStore struct {
	byHash  map[string]invite.Invitation
	members map[[2]uuid.UUID]workspace.Role
	fail    map[string]error
}

// failure returns the error injected for an operation.
func (f *fakeStore) failure(op string) error { return f.fail[op] }

// Create replaces the invitation of the same workspace and email.
func (f *fakeStore) Create(_ context.Context, i invite.Invitation, hash []byte) error {
	if err := f.failure("create"); err != nil {
		return err
	}
	for key, other := range f.byHash {
		if other.WorkspaceID == i.WorkspaceID && other.Email == i.Email {
			delete(f.byHash, key)
		}
	}
	f.byHash[string(hash)] = i
	return nil
}

// ByTokenHash finds an unexpired invitation.
func (f *fakeStore) ByTokenHash(_ context.Context, hash []byte, now time.Time) (invite.Invitation, error) {
	if err := f.failure("byHash"); err != nil {
		return invite.Invitation{}, err
	}
	i, ok := f.byHash[string(hash)]
	if !ok || !i.ExpiresAt.After(now) {
		return invite.Invitation{}, invite.ErrNotFound
	}
	return i, nil
}

// Pending lists the invitations of a workspace.
func (f *fakeStore) Pending(_ context.Context, workspaceID uuid.UUID, _ time.Time) ([]invite.Invitation, error) {
	if err := f.failure("pending"); err != nil {
		return nil, err
	}
	var out []invite.Invitation
	for _, i := range f.byHash {
		if i.WorkspaceID == workspaceID {
			out = append(out, i)
		}
	}
	return out, nil
}

// CountPending counts the invitations of a workspace.
func (f *fakeStore) CountPending(ctx context.Context, workspaceID uuid.UUID, now time.Time) (int, error) {
	if err := f.failure("count"); err != nil {
		return 0, err
	}
	list, _ := f.Pending(ctx, workspaceID, now)
	return len(list), nil
}

// Revoke deletes an invitation by identifier.
func (f *fakeStore) Revoke(_ context.Context, workspaceID, id uuid.UUID) error {
	if err := f.failure("revoke"); err != nil {
		return err
	}
	for key, i := range f.byHash {
		if i.ID == id && i.WorkspaceID == workspaceID {
			delete(f.byHash, key)
			return nil
		}
	}
	return invite.ErrNotFound
}

// Accept consumes an invitation and records the membership.
func (f *fakeStore) Accept(_ context.Context, id, userID uuid.UUID, _ time.Time) (workspace.Summary, error) {
	if err := f.failure("accept"); err != nil {
		return workspace.Summary{}, err
	}
	for key, i := range f.byHash {
		if i.ID == id {
			delete(f.byHash, key)
			f.members[[2]uuid.UUID{i.WorkspaceID, userID}] = i.Role
			return workspace.Summary{Workspace: workspace.Workspace{ID: i.WorkspaceID, Name: i.WorkspaceName, Kind: workspace.KindTeam}, Role: i.Role}, nil
		}
	}
	return workspace.Summary{}, invite.ErrNotFound
}

// PurgeExpired counts the expired invitations and removes them.
func (f *fakeStore) PurgeExpired(_ context.Context, now time.Time) (int, error) {
	if err := f.failure("purge"); err != nil {
		return 0, err
	}
	n := 0
	for key, i := range f.byHash {
		if !i.ExpiresAt.After(now) {
			delete(f.byHash, key)
			n++
		}
	}
	return n, nil
}

// fakeAuthz allows what a role map allows and knows the members of one workspace.
type fakeAuthz struct {
	ws      workspace.Workspace
	role    map[uuid.UUID]workspace.Role
	members []workspace.Member
	fail    error
}

// Invitable reproduces the rules of the workspace service for the tests.
func (f *fakeAuthz) Invitable(_ context.Context, actor, workspaceID uuid.UUID, role workspace.Role) (workspace.Workspace, error) {
	mine, ok := f.role[actor]
	switch {
	case !ok || workspaceID != f.ws.ID:
		return workspace.Workspace{}, workspace.ErrNotFound
	case !workspace.Can(mine, workspace.ActionManageMembers):
		return workspace.Workspace{}, workspace.ErrForbidden
	case role == workspace.RoleOwner:
		return workspace.Workspace{}, workspace.ErrInvalidRole
	}
	return f.ws, nil
}

// Authorize checks the role map.
func (f *fakeAuthz) Authorize(_ context.Context, userID, workspaceID uuid.UUID, action workspace.Action) (workspace.Role, error) {
	mine, ok := f.role[userID]
	switch {
	case !ok || workspaceID != f.ws.ID:
		return "", workspace.ErrNotFound
	case !workspace.Can(mine, action):
		return "", workspace.ErrForbidden
	}
	return mine, nil
}

// Members returns the configured members.
func (f *fakeAuthz) Members(context.Context, uuid.UUID, uuid.UUID) ([]workspace.Member, error) {
	return f.members, f.fail
}

// fakeAccounts knows the emails that have an account.
type fakeAccounts struct {
	known map[string]bool
	fail  error
}

// UserByEmail finds a configured account.
func (f *fakeAccounts) UserByEmail(_ context.Context, email string) (auth.User, error) {
	if f.fail != nil {
		return auth.User{}, f.fail
	}
	if !f.known[email] {
		return auth.User{}, auth.ErrNotFound
	}
	return auth.User{ID: uuid.New(), Email: email}, nil
}

// fakeRegistrar creates accounts in the fake account list.
type fakeRegistrar struct {
	accounts *fakeAccounts
	fail     error
}

// Register creates an account unless the email is taken or a failure is injected.
func (f *fakeRegistrar) Register(_ context.Context, email, _ string) (auth.User, error) {
	if f.fail != nil {
		return auth.User{}, f.fail
	}
	if f.accounts.known[email] {
		return auth.User{}, auth.ErrEmailTaken
	}
	f.accounts.known[email] = true
	return auth.User{ID: uuid.New(), Email: email}, nil
}

// env is a service over fakes with an owner, an admin and an editor.
type env struct {
	svc                  *invite.Service
	store                *fakeStore
	authz                *fakeAuthz
	accounts             *fakeAccounts
	registrar            *fakeRegistrar
	clock                *testutil.FakeClock
	ws                   uuid.UUID
	owner, admin, editor uuid.UUID
}

// newEnv builds the environment.
func newEnv() *env {
	e := &env{ws: uuid.New(), owner: uuid.New(), admin: uuid.New(), editor: uuid.New()}
	e.store = &fakeStore{byHash: map[string]invite.Invitation{}, members: map[[2]uuid.UUID]workspace.Role{}, fail: map[string]error{}}
	e.authz = &fakeAuthz{
		ws:      workspace.Workspace{ID: e.ws, Name: "Acme", Kind: workspace.KindTeam},
		role:    map[uuid.UUID]workspace.Role{e.owner: workspace.RoleOwner, e.admin: workspace.RoleAdmin, e.editor: workspace.RoleEditor},
		members: []workspace.Member{{UserID: e.owner, Email: "owner@example.com", Role: workspace.RoleOwner}},
	}
	e.accounts = &fakeAccounts{known: map[string]bool{"known@example.com": true}}
	e.registrar = &fakeRegistrar{accounts: e.accounts}
	e.clock = testutil.NewFakeClock(time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
	e.svc = invite.NewService(e.store, e.authz, e.accounts, e.registrar, e.clock, rand.Reader, 0, nil)
	return e
}

// TestService_Create verifies creating invitations.
//
// Scenarios:
//   - an admin invites an email with a role: the address is normalized, the token is long and random, only its hash is stored, and the
//     invitation expires after the default seven days
//   - inviting the same address again replaces the first invitation and its token stops working
//   - owners, editors and strangers cannot invite as they should, unknown roles are refused, and invalid emails are refused
//   - an address that is already a member cannot be invited, and a workspace with too many invitations refuses more
//   - failures of the store, of the member list and of the random source are returned
func TestService_Create(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	created, token, err := e.svc.Create(ctx, e.admin, e.ws, "  New@Example.COM ", workspace.RoleEditor)
	if err != nil || created.Email != "new@example.com" || created.Role != workspace.RoleEditor || created.WorkspaceName != "Acme" || created.InvitedBy != e.admin {
		t.Fatalf("Create() = %+v, %v", created, err)
	}
	if len(token) < 43 || created.ExpiresAt.Sub(created.CreatedAt) != invite.DefaultTTL {
		t.Errorf("token %q and lifetime %v; want 43 characters and seven days", token, created.ExpiresAt.Sub(created.CreatedAt))
	}
	for key := range e.store.byHash {
		if strings.Contains(key, token) {
			t.Error("the token is stored in clear text")
		}
	}

	_, second, err := e.svc.Create(ctx, e.owner, e.ws, "new@example.com", workspace.RoleViewer)
	if err != nil || second == token || len(e.store.byHash) != 1 {
		t.Fatalf("second invitation = %v with %d stored; want it to replace the first", err, len(e.store.byHash))
	}
	if _, err := e.svc.Preview(ctx, token); !errors.Is(err, invite.ErrNotFound) {
		t.Errorf("replaced token = %v; want ErrNotFound", err)
	}

	for name, c := range map[string]struct {
		actor uuid.UUID
		email string
		role  workspace.Role
		want  error
	}{
		"editor invites":     {e.editor, "a@example.com", workspace.RoleViewer, workspace.ErrForbidden},
		"stranger":           {uuid.New(), "a@example.com", workspace.RoleViewer, workspace.ErrNotFound},
		"invite an owner":    {e.owner, "a@example.com", workspace.RoleOwner, workspace.ErrInvalidRole},
		"invalid email":      {e.owner, "nobody", workspace.RoleViewer, auth.ErrInvalidEmail},
		"existing member":    {e.owner, "OWNER@example.com", workspace.RoleViewer, workspace.ErrAlreadyMember},
		"a name and address": {e.owner, "Bob <b@example.com>", workspace.RoleViewer, auth.ErrInvalidEmail},
	} {
		if _, _, err := e.svc.Create(ctx, c.actor, e.ws, c.email, c.role); !errors.Is(err, c.want) {
			t.Errorf("%s = %v; want %v", name, err, c.want)
		}
	}

	many := newEnv()
	for i := range invite.MaxPending {
		if _, _, err := many.svc.Create(ctx, many.owner, many.ws, uuid.NewString()+"@example.com", workspace.RoleViewer); err != nil {
			t.Fatalf("invitation %d = %v", i, err)
		}
	}
	if _, _, err := many.svc.Create(ctx, many.owner, many.ws, "late@example.com", workspace.RoleViewer); !errors.Is(err, invite.ErrTooMany) {
		t.Errorf("one too many = %v; want ErrTooMany", err)
	}

	for _, op := range []string{"count", "create"} {
		failing := newEnv()
		failing.store.fail[op] = errBoom
		if _, _, err := failing.svc.Create(ctx, failing.owner, failing.ws, "a@example.com", workspace.RoleViewer); !errors.Is(err, errBoom) {
			t.Errorf("%s failure = %v", op, err)
		}
	}
	failing := newEnv()
	failing.authz.fail = errBoom
	if _, _, err := failing.svc.Create(ctx, failing.owner, failing.ws, "a@example.com", workspace.RoleViewer); !errors.Is(err, errBoom) {
		t.Errorf("members failure = %v", err)
	}
	noRandom := invite.NewService(e.store, e.authz, e.accounts, e.registrar, e.clock, bytes.NewReader(nil), 0, nil)
	if _, _, err := noRandom.Create(ctx, e.owner, e.ws, "r@example.com", workspace.RoleViewer); err == nil {
		t.Error("a failing random source created an invitation")
	}
	noID := invite.NewService(e.store, e.authz, e.accounts, e.registrar, e.clock, rand.Reader, time.Hour, func() (uuid.UUID, error) { return uuid.Nil, errBoom })
	if _, _, err := noID.Create(ctx, e.owner, e.ws, "r@example.com", workspace.RoleViewer); !errors.Is(err, errBoom) {
		t.Errorf("id failure = %v", err)
	}
}

// TestService_ListAndRevoke verifies listing and revoking.
//
// Scenarios:
//   - admins list and revoke, and a revoked token stops working
//   - editors and strangers cannot list or revoke, and revoking an unknown invitation is ErrNotFound
//   - store failures are returned
func TestService_ListAndRevoke(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	created, token, err := e.svc.Create(ctx, e.admin, e.ws, "a@example.com", workspace.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	list, err := e.svc.List(ctx, e.admin, e.ws)
	if err != nil || len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("List() = %+v, %v", list, err)
	}
	if _, err := e.svc.List(ctx, e.editor, e.ws); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("editor list = %v", err)
	}
	if _, err := e.svc.List(ctx, uuid.New(), e.ws); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger list = %v", err)
	}
	if err := e.svc.Revoke(ctx, e.editor, e.ws, created.ID); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("editor revoke = %v", err)
	}
	if err := e.svc.Revoke(ctx, e.admin, e.ws, uuid.New()); !errors.Is(err, invite.ErrNotFound) {
		t.Errorf("unknown revoke = %v", err)
	}
	if err := e.svc.Revoke(ctx, e.admin, e.ws, created.ID); err != nil {
		t.Fatalf("Revoke() = %v", err)
	}
	if _, err := e.svc.Preview(ctx, token); !errors.Is(err, invite.ErrNotFound) {
		t.Errorf("revoked token = %v; want ErrNotFound", err)
	}
	e.store.fail["pending"] = errBoom
	if _, err := e.svc.List(ctx, e.admin, e.ws); !errors.Is(err, errBoom) {
		t.Errorf("list failure = %v", err)
	}
}

// TestService_Preview verifies what a token holder learns.
//
// Scenarios:
//   - the preview names the workspace, role and email and tells whether the address has an account
//   - unknown and expired tokens are ErrNotFound, and lookup failures are returned
func TestService_Preview(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	_, token, _ := e.svc.Create(ctx, e.admin, e.ws, "new@example.com", workspace.RoleEditor)
	_, knownToken, _ := e.svc.Create(ctx, e.admin, e.ws, "known@example.com", workspace.RoleViewer)
	got, err := e.svc.Preview(ctx, token)
	if err != nil || got.WorkspaceName != "Acme" || got.Role != workspace.RoleEditor || got.Email != "new@example.com" || got.AccountExists {
		t.Fatalf("Preview() = %+v, %v", got, err)
	}
	if got, err := e.svc.Preview(ctx, knownToken); err != nil || !got.AccountExists {
		t.Fatalf("Preview(known) = %+v, %v; want an existing account", got, err)
	}
	if _, err := e.svc.Preview(ctx, "nope"); !errors.Is(err, invite.ErrNotFound) {
		t.Errorf("unknown = %v", err)
	}
	e.accounts.fail = errBoom
	if _, err := e.svc.Preview(ctx, token); !errors.Is(err, errBoom) {
		t.Errorf("account failure = %v", err)
	}
	e.accounts.fail = nil
	e.clock.Advance(invite.DefaultTTL)
	if _, err := e.svc.Preview(ctx, token); !errors.Is(err, invite.ErrNotFound) {
		t.Errorf("expired = %v; want ErrNotFound", err)
	}
}

// TestService_Accept verifies accepting with a signed-in account.
//
// Scenarios:
//   - the account with the invited email joins with the invited role and the token works only once
//   - an account with another email gets ErrNotFound and the invitation stays usable
//   - unknown tokens and store failures are returned as they are
func TestService_Accept(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	_, token, _ := e.svc.Create(ctx, e.admin, e.ws, "known@example.com", workspace.RoleEditor)
	me := auth.User{ID: uuid.New(), Email: "known@example.com"}
	if _, err := e.svc.Accept(ctx, auth.User{ID: uuid.New(), Email: "other@example.com"}, token); !errors.Is(err, invite.ErrNotFound) {
		t.Fatalf("foreign account = %v; want ErrNotFound", err)
	}
	w, err := e.svc.Accept(ctx, me, token)
	if err != nil || w.ID != e.ws || w.Role != workspace.RoleEditor || e.store.members[[2]uuid.UUID{e.ws, me.ID}] != workspace.RoleEditor {
		t.Fatalf("Accept() = %+v, %v; members %v", w, err, e.store.members)
	}
	if _, err := e.svc.Accept(ctx, me, token); !errors.Is(err, invite.ErrNotFound) {
		t.Errorf("second use = %v; want ErrNotFound", err)
	}
	if _, err := e.svc.Accept(ctx, me, "nope"); !errors.Is(err, invite.ErrNotFound) {
		t.Errorf("unknown = %v", err)
	}
	_, again, _ := e.svc.Create(ctx, e.admin, e.ws, "known@example.com", workspace.RoleViewer)
	e.store.fail["accept"] = errBoom
	if _, err := e.svc.Accept(ctx, me, again); !errors.Is(err, errBoom) {
		t.Errorf("store failure = %v", err)
	}
}

// TestService_Join verifies creating the invited account.
//
// Scenarios:
//   - the account is created with the invited email and joins with the invited role
//   - an address that already has an account gets ErrEmailTaken and nothing changes
//   - unknown tokens, registration failures and acceptance failures are returned
func TestService_Join(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	_, token, _ := e.svc.Create(ctx, e.admin, e.ws, "fresh@example.com", workspace.RoleViewer)
	user, w, err := e.svc.Join(ctx, token, "long enough password")
	if err != nil || user.Email != "fresh@example.com" || w.ID != e.ws || e.store.members[[2]uuid.UUID{e.ws, user.ID}] != workspace.RoleViewer {
		t.Fatalf("Join() = %+v, %+v, %v", user, w, err)
	}
	_, taken, _ := e.svc.Create(ctx, e.admin, e.ws, "known@example.com", workspace.RoleViewer)
	if _, _, err := e.svc.Join(ctx, taken, "long enough password"); !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("taken = %v; want ErrEmailTaken", err)
	}
	if _, _, err := e.svc.Join(ctx, "nope", "long enough password"); !errors.Is(err, invite.ErrNotFound) {
		t.Errorf("unknown = %v", err)
	}
	_, weak, _ := e.svc.Create(ctx, e.admin, e.ws, "weak@example.com", workspace.RoleViewer)
	e.registrar.fail = auth.ErrWeakPassword
	if _, _, err := e.svc.Join(ctx, weak, "short"); !errors.Is(err, auth.ErrWeakPassword) {
		t.Errorf("registration failure = %v", err)
	}
	e.registrar.fail = nil
	e.store.fail["accept"] = errBoom
	if _, _, err := e.svc.Join(ctx, weak, "long enough password"); !errors.Is(err, errBoom) {
		t.Errorf("accept failure = %v", err)
	}
}

// TestService_PurgeExpired verifies cleaning up.
//
// Scenarios:
//   - only the invitations past their expiry are deleted and counted, and failures are returned
func TestService_PurgeExpired(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	for _, email := range []string{"a@example.com", "b@example.com"} {
		if _, _, err := e.svc.Create(ctx, e.admin, e.ws, email, workspace.RoleViewer); err != nil {
			t.Fatal(err)
		}
		e.clock.Advance(invite.DefaultTTL - time.Hour)
	}
	if n, err := e.svc.PurgeExpired(ctx); err != nil || n != 1 || len(e.store.byHash) != 1 {
		t.Fatalf("PurgeExpired() = %d, %v with %d left; want 1 removed", n, err, len(e.store.byHash))
	}
	e.store.fail["purge"] = errBoom
	if _, err := e.svc.PurgeExpired(ctx); !errors.Is(err, errBoom) {
		t.Errorf("failure = %v", err)
	}
}

// fakeQuota is a member quota that answers with a fixed error and counts the questions.
type fakeQuota struct {
	err   error
	asked int
}

// CheckMember counts the question and returns the error.
func (q *fakeQuota) CheckMember(context.Context, uuid.UUID) error {
	q.asked++
	return q.err
}

// TestService_Quota verifies that a quota decides whether a team may take one more person.
//
// Scenarios:
//   - without a quota, and with one that allows, an invitation is created
//   - a quota that refuses stops it before anything is stored, and its error is returned
//   - people who may not invite are refused before the quota is asked
func TestService_Quota(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	if _, _, err := e.svc.Create(ctx, e.admin, e.ws, "a@example.com", workspace.RoleViewer); err != nil {
		t.Fatalf("Create() without a quota = %v", err)
	}
	allowing := &fakeQuota{}
	e.svc.UseQuota(allowing)
	if _, _, err := e.svc.Create(ctx, e.admin, e.ws, "b@example.com", workspace.RoleViewer); err != nil || allowing.asked != 1 {
		t.Fatalf("Create() with a quota that allows = %v, asked %d", err, allowing.asked)
	}
	errFull := errors.New("full")
	refusing := &fakeQuota{err: errFull}
	e.svc.UseQuota(refusing)
	stored := len(e.store.byHash)
	if _, _, err := e.svc.Create(ctx, e.admin, e.ws, "c@example.com", workspace.RoleViewer); !errors.Is(err, errFull) || len(e.store.byHash) != stored {
		t.Errorf("Create() with a quota that refuses = %v, %d stored; want the refusal and nothing stored", err, len(e.store.byHash)-stored)
	}
	refusing.asked = 0
	if _, _, err := e.svc.Create(ctx, e.editor, e.ws, "d@example.com", workspace.RoleViewer); !errors.Is(err, workspace.ErrForbidden) || refusing.asked != 0 {
		t.Errorf("an editor inviting = %v, quota asked %d times; want ErrForbidden and no question", err, refusing.asked)
	}
}
