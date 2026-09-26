package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

// teamEnv is a team workspace with one member of every role, each with a session.
type teamEnv struct {
	a  *api
	ws uuid.UUID
	// cookies, tokens and users by role name: owner, admin, editor, viewer and stranger.
	cookie map[string]*http.Cookie
	csrf   map[string]string
	user   map[string]uuid.UUID
}

// newTeamEnv creates the team through the API as its owner and adds the other members.
func newTeamEnv(t *testing.T) *teamEnv {
	t.Helper()
	a := newAPI(t)
	e := &teamEnv{a: a, cookie: map[string]*http.Cookie{}, csrf: map[string]string{}, user: map[string]uuid.UUID{}}
	for _, role := range []string{"owner", "admin", "editor", "viewer", "stranger"} {
		cookie, user := a.signIn(role + "@example.com")
		e.cookie[role], e.user[role] = cookie, user.ID
		e.csrf[role] = a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}).session(t).CsrfToken
	}
	created := a.send(http.MethodPost, "/api/v1/workspaces", map[string]string{"name": "  Acme Web  "}, e.cookie["owner"], e.csrf["owner"])
	if created.Status != http.StatusCreated {
		t.Fatalf("create team = %d %s", created.Status, created.Body)
	}
	var w httpapi.Workspace
	mustDecode(t, created.Body, &w)
	if w.Name != "Acme Web" || w.Kind != "team" || w.Role != "owner" {
		t.Fatalf("team = %+v", w)
	}
	e.ws = w.Id
	for role, r := range map[string]workspace.Role{"admin": workspace.RoleAdmin, "editor": workspace.RoleEditor, "viewer": workspace.RoleViewer} {
		a.addMember(e.ws, e.user[role], r)
	}
	return e
}

// call sends a request as a role, with its CSRF token.
func (e *teamEnv) call(as, method, path string, body any) reply {
	e.a.t.Helper()
	return e.a.send(method, "/api/v1/workspaces/"+e.ws.String()+path, body, e.cookie[as], e.csrf[as])
}

// members lists the team as a role and returns email to role.
func (e *teamEnv) members(as string) map[string]string {
	e.a.t.Helper()
	r := e.call(as, http.MethodGet, "/members", nil)
	if r.Status != http.StatusOK {
		e.a.t.Fatalf("list members = %d %s", r.Status, r.Body)
	}
	var list []httpapi.Member
	if err := json.Unmarshal(r.Body, &list); err != nil {
		e.a.t.Fatal(err)
	}
	out := map[string]string{}
	for _, m := range list {
		out[m.Email] = string(m.Role)
	}
	if len(list) > 0 && list[0].Role != "owner" {
		e.a.t.Errorf("first member is %s; want the owner first", list[0].Role)
	}
	return out
}

// TestTeams_Lifecycle verifies the membership API of a team workspace.
//
// Scenarios:
//   - a team is created with a trimmed name, listed for every member with their role, and its members are listed with the owner first
//   - the owner promotes an editor to admin, an admin promotes a viewer to editor, and role changes show in the list
//   - a member removed by an admin no longer sees the workspace, and a member who leaves is gone too
//   - ownership is transferred and the old owner becomes an admin
//   - bad names, unknown roles and the like are refused with stable codes
func TestTeams_Lifecycle(t *testing.T) {
	e := newTeamEnv(t)
	want := map[string]string{"owner@example.com": "owner", "admin@example.com": "admin", "editor@example.com": "editor", "viewer@example.com": "viewer"}
	got := e.members("viewer")
	for email, role := range want {
		if got[email] != role {
			t.Errorf("member %s = %q; want %q", email, got[email], role)
		}
	}
	listed := e.a.do(request{method: http.MethodGet, path: "/api/v1/workspaces", cookie: e.cookie["viewer"]}).workspaces(t)
	found := false
	for _, w := range listed {
		found = found || w.Id == e.ws && w.Role == "viewer" && w.Kind == "team"
	}
	if !found {
		t.Errorf("the viewer's workspaces = %+v; want the team as viewer", listed)
	}

	if r := e.call("owner", http.MethodPatch, "/members/"+e.user["editor"].String(), map[string]string{"role": "admin"}); r.Status != http.StatusNoContent {
		t.Fatalf("promote = %d %s", r.Status, r.Body)
	}
	if e.members("owner")["editor@example.com"] != "admin" {
		t.Error("the promotion is not in the list")
	}
	if r := e.call("admin", http.MethodPatch, "/members/"+e.user["viewer"].String(), map[string]string{"role": "editor"}); r.Status != http.StatusNoContent {
		t.Fatalf("admin promotes = %d %s", r.Status, r.Body)
	}

	if r := e.call("admin", http.MethodDelete, "/members/"+e.user["viewer"].String(), nil); r.Status != http.StatusNoContent {
		t.Fatalf("remove = %d %s", r.Status, r.Body)
	}
	expectProblem(t, e.call("viewer", http.MethodGet, "/members", nil), 404, "workspace_not_found")
	if r := e.call("editor", http.MethodPost, "/leave", nil); r.Status != http.StatusNoContent {
		t.Fatalf("leave = %d %s", r.Status, r.Body)
	}
	expectProblem(t, e.call("editor", http.MethodGet, "/members", nil), 404, "workspace_not_found")

	if r := e.call("owner", http.MethodPost, "/transfer", map[string]string{"userId": e.user["admin"].String()}); r.Status != http.StatusNoContent {
		t.Fatalf("transfer = %d %s", r.Status, r.Body)
	}
	after := e.members("admin")
	if after["admin@example.com"] != "owner" || after["owner@example.com"] != "admin" {
		t.Errorf("after the transfer = %v", after)
	}

	expectProblem(t, e.a.send(http.MethodPost, "/api/v1/workspaces", map[string]string{"name": "  "}, e.cookie["owner"], e.csrf["owner"]), 422, "invalid_name")
	expectProblem(t, e.call("owner", http.MethodPatch, "/members/"+e.user["admin"].String(), map[string]string{"role": "viewer"}), 409, "owner_bound")
	expectProblem(t, e.call("admin", http.MethodPatch, "/members/"+e.user["owner"].String(), map[string]string{"role": "owner"}), 422, "invalid_role")
	expectProblem(t, e.call("admin", http.MethodPatch, "/members/"+uuid.NewString(), map[string]string{"role": "viewer"}), 404, "member_not_found")
	expectProblem(t, e.call("admin", http.MethodPatch, "/members/"+e.user["owner"].String(), map[string]string{"role": "boss"}), 422, "invalid_role")
	expectProblem(t, e.call("admin", http.MethodPost, "/leave", nil), 409, "owner_bound")
}

// TestTeams_AuthorizationMatrix verifies who may reach each membership endpoint.
//
// Scenarios:
//   - anonymous callers get 401 and writers without the anti-CSRF token get 403 csrf_invalid
//   - a stranger gets 404 workspace_not_found everywhere
//   - viewers and editors may list members but get 403 forbidden when they change or remove one, and only the owner may transfer
//   - an admin cannot change another admin or grant admin, and cannot transfer
//   - personal workspaces have no members, cannot be left and cannot be transferred: 409 personal_workspace
func TestTeams_AuthorizationMatrix(t *testing.T) {
	e := newTeamEnv(t)
	editor, viewer := e.user["editor"].String(), e.user["viewer"].String()
	type call struct {
		name, method, path string
		body               any
	}
	calls := []call{
		{"list", http.MethodGet, "/members", nil},
		{"role", http.MethodPatch, "/members/" + viewer, map[string]string{"role": "editor"}},
		{"remove", http.MethodDelete, "/members/" + viewer, nil},
		{"leave", http.MethodPost, "/leave", nil},
		{"transfer", http.MethodPost, "/transfer", map[string]string{"userId": e.user["admin"].String()}},
	}
	base := "/api/v1/workspaces/" + e.ws.String()
	for _, c := range calls {
		expectProblem(t, e.a.do(request{method: c.method, path: base + c.path, body: c.body}), 401, "unauthenticated")
		if c.method != http.MethodGet {
			expectProblem(t, e.a.do(request{method: c.method, path: base + c.path, body: c.body, cookie: e.cookie["admin"]}), 403, "csrf_invalid")
		}
		expectProblem(t, e.call("stranger", c.method, c.path, c.body), 404, "workspace_not_found")
	}
	for _, as := range []string{"viewer", "editor"} {
		if r := e.call(as, http.MethodGet, "/members", nil); r.Status != http.StatusOK {
			t.Errorf("%s list = %d", as, r.Status)
		}
		expectProblem(t, e.call(as, http.MethodPatch, "/members/"+viewer, map[string]string{"role": "editor"}), 403, "forbidden")
		expectProblem(t, e.call(as, http.MethodDelete, "/members/"+editor, nil), 403, "forbidden")
		expectProblem(t, e.call(as, http.MethodPost, "/transfer", map[string]string{"userId": e.user["admin"].String()}), 403, "forbidden")
	}
	expectProblem(t, e.call("admin", http.MethodPatch, "/members/"+viewer, map[string]string{"role": "admin"}), 422, "invalid_role")
	expectProblem(t, e.call("admin", http.MethodPost, "/transfer", map[string]string{"userId": editor}), 403, "forbidden")
	e.a.addMember(e.ws, e.user["stranger"], workspace.RoleAdmin)
	expectProblem(t, e.call("admin", http.MethodDelete, "/members/"+e.user["stranger"].String(), nil), 403, "forbidden")

	personal := e.a.personal(e.cookie["owner"])
	for _, c := range calls {
		r := e.a.send(c.method, "/api/v1/workspaces/"+personal.String()+c.path, c.body, e.cookie["owner"], e.csrf["owner"])
		if c.name == "leave" || c.name == "transfer" || c.name == "list" || c.name == "role" || c.name == "remove" {
			expectProblem(t, r, 409, "personal_workspace")
		}
	}
}
