package httpapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/core/wskey"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// teamEnv is a team workspace with one member of every role, each with a session.
type teamEnv struct {
	a     *api
	sites *site.Service
	ws    uuid.UUID
	// cookies, tokens and users by role name: owner, admin, editor, viewer and stranger.
	cookie map[string]*http.Cookie
	csrf   map[string]string
	user   map[string]uuid.UUID
}

// newTeamEnv creates the team through the API as its owner and adds the other members. Tweaks change the dependencies of the server.
func newTeamEnv(t *testing.T, tweaks ...func(*httpapi.Deps)) *teamEnv {
	t.Helper()
	e := &teamEnv{cookie: map[string]*http.Cookie{}, csrf: map[string]string{}, user: map[string]uuid.UUID{}}
	a := newAPI(t, append(tweaks, func(d *httpapi.Deps) { e.sites = d.Sites })...)
	e.a = a
	for _, role := range []string{"owner", "admin", "editor", "viewer", "stranger"} {
		cookie, user := a.signIn(role + "@example.com")
		e.cookie[role], e.user[role] = cookie, user.ID
		e.csrf[role] = a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}).session(t).CsrfToken
	}
	if r := a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, e.cookie["owner"], e.csrf["owner"]); r.Status != http.StatusCreated {
		t.Fatalf("vault setup = %d %s", r.Status, r.Body)
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

// session returns the identifier of the session of a role, which keys the vault of that session.
func (e *teamEnv) session(as string) string {
	e.a.t.Helper()
	p, err := e.a.auth.Authenticate(e.a.t.Context(), e.cookie[as].Value)
	if err != nil {
		e.a.t.Fatal(err)
	}
	return p.Session.ID.String()
}

// openSecret opens the password of a site as a role, the way a connection does.
func (e *teamEnv) openSecret(as string, siteID uuid.UUID) (string, error) {
	e.a.t.Helper()
	secret, err := e.sites.OpenSecret(e.a.t.Context(), e.user[as], e.ws, siteID, e.session(as), site.SecretPassword)
	return string(secret), err
}

// TestTeams_SharedKeys verifies that the members of a team share its secrets without sharing a password.
//
// Scenarios:
//   - creating a team needs an unlocked vault, and the owner opens the secret they saved in it
//   - a member who has not set up a vault is waiting, cannot open the secret and is told the key is pending
//   - once they set up their vault they still wait until a member with the key unlocks or lists the members, and then they open the secret
//   - the copy is made when the owner unlocks the vault, and also when a member lists the team
//   - a member who never got a copy cannot open the secret, and personal workspaces keep using the vault of their owner
func TestTeams_SharedKeys(t *testing.T) {
	e := newTeamEnv(t)
	expectProblem(t, e.a.send(http.MethodPost, "/api/v1/workspaces", map[string]string{"name": "No vault"}, e.cookie["stranger"], e.csrf["stranger"]), 409, "vault_locked")

	body := secret(siteBody("Production"), "password", "hunter2-hunter2")
	created := e.call("owner", http.MethodPost, "/sites", body)
	if created.Status != http.StatusCreated {
		t.Fatalf("create site = %d %s", created.Status, created.Body)
	}
	id := created.site(t).Id
	if got, err := e.openSecret("owner", id); err != nil || got != "hunter2-hunter2" {
		t.Fatalf("owner opens = %q, %v", got, err)
	}

	states := func() map[string]string {
		r := e.call("viewer", http.MethodGet, "/members", nil)
		var list []httpapi.Member
		mustDecode(t, r.Body, &list)
		out := map[string]string{}
		for _, m := range list {
			out[m.Email] = string(m.KeyState)
		}
		return out
	}
	if got := states(); got["owner@example.com"] != "sealed" || got["admin@example.com"] != "waiting" || got["editor@example.com"] != "waiting" {
		t.Fatalf("key states = %v; want the owner sealed and the others waiting", got)
	}
	if _, err := e.openSecret("admin", id); err == nil {
		t.Error("a member without a vault opened the secret")
	}

	for _, as := range []string{"admin", "editor"} {
		if r := e.a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, e.cookie[as], e.csrf[as]); r.Status != http.StatusCreated {
			t.Fatalf("vault setup as %s = %d %s", as, r.Status, r.Body)
		}
		if _, err := e.openSecret(as, id); !errors.Is(err, wskey.ErrPending) {
			t.Errorf("%s before receiving a copy = %v; want ErrPending", as, err)
		}
	}
	if got := states(); got["admin@example.com"] != "waiting" {
		t.Fatalf("key states = %v; want the admin still waiting: a member without the key cannot hand it over", got)
	}

	e.a.vaultCall("/lock", nil, e.cookie["owner"], e.csrf["owner"])
	if r := e.a.vaultCall("/unlock", map[string]string{"masterPassword": masterPassword}, e.cookie["owner"], e.csrf["owner"]); r.Status != http.StatusNoContent {
		t.Fatalf("unlock = %d %s", r.Status, r.Body)
	}
	for _, as := range []string{"admin", "editor"} {
		if got, err := e.openSecret(as, id); err != nil || got != "hunter2-hunter2" {
			t.Errorf("%s after the owner unlocked = %q, %v; want the secret", as, got, err)
		}
	}
	if got := states(); got["admin@example.com"] != "sealed" || got["editor@example.com"] != "sealed" || got["viewer@example.com"] != "waiting" {
		t.Errorf("key states = %v; want everyone with a vault sealed and the viewer waiting", got)
	}
	if _, err := e.openSecret("viewer", id); err == nil {
		t.Error("a member without a vault opened the secret")
	}

	late := e.a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, e.cookie["viewer"], e.csrf["viewer"])
	if late.Status != http.StatusCreated {
		t.Fatalf("vault setup as viewer = %d %s", late.Status, late.Body)
	}
	if r := e.call("owner", http.MethodGet, "/members", nil); r.Status != http.StatusOK {
		t.Fatalf("list = %d", r.Status)
	}
	if got, err := e.openSecret("viewer", id); err != nil || got != "hunter2-hunter2" {
		t.Errorf("viewer after the owner listed the members = %q, %v; want the secret", got, err)
	}

	personal := e.a.personal(e.cookie["owner"])
	own := e.a.send(http.MethodPost, sitesPath(personal), body, e.cookie["owner"], e.csrf["owner"])
	if own.Status != http.StatusCreated {
		t.Fatalf("personal site = %d %s", own.Status, own.Body)
	}
	if got, err := e.sites.OpenSecret(t.Context(), e.user["owner"], personal, own.site(t).Id, e.session("owner"), site.SecretPassword); err != nil || string(got) != "hunter2-hunter2" {
		t.Errorf("personal secret = %q, %v", got, err)
	}
}

// keyState reads the version of the key of the team and whether a rotation waits, straight from the database.
func (e *teamEnv) keyState() (int, bool) {
	e.a.t.Helper()
	var version int
	var pending bool
	err := e.a.store.DB().QueryRow(e.a.t.Context(), "SELECT key_version, rekey_pending FROM workspaces WHERE id = $1", e.ws).Scan(&version, &pending)
	if err != nil {
		e.a.t.Fatal(err)
	}
	return version, pending
}

// TestTeams_KeyRotation verifies that removing a member replaces the key of the team.
//
// Scenarios:
//   - removing a member as somebody with the key and an unlocked vault rotates the key at once: the version grows, the secrets are sealed again
//     and the members that stay still open them, while the removed member has no access
//   - removing a member as somebody whose vault is locked leaves the rotation waiting, and it happens when that member unlocks
//   - a member who leaves also makes the next member with the key rotate it
func TestTeams_KeyRotation(t *testing.T) {
	e := newTeamEnv(t)
	body := secret(siteBody("Production"), "password", "hunter2-hunter2")
	created := e.call("owner", http.MethodPost, "/sites", body)
	if created.Status != http.StatusCreated {
		t.Fatalf("create site = %d %s", created.Status, created.Body)
	}
	id := created.site(t).Id
	for _, as := range []string{"admin", "editor", "viewer"} {
		if r := e.a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, e.cookie[as], e.csrf[as]); r.Status != http.StatusCreated {
			t.Fatalf("vault setup as %s = %d %s", as, r.Status, r.Body)
		}
	}
	if r := e.call("owner", http.MethodGet, "/members", nil); r.Status != http.StatusOK {
		t.Fatalf("list = %d", r.Status)
	}
	if version, pending := e.keyState(); version != 1 || pending {
		t.Fatalf("key state = %d, %v; want version 1", version, pending)
	}

	if r := e.call("owner", http.MethodDelete, "/members/"+e.user["editor"].String(), nil); r.Status != http.StatusNoContent {
		t.Fatalf("remove = %d %s", r.Status, r.Body)
	}
	if version, pending := e.keyState(); version != 2 || pending {
		t.Fatalf("key state after the removal = %d, %v; want version 2 with nothing waiting", version, pending)
	}
	var stored, sealedWith int
	if err := e.a.store.DB().QueryRow(t.Context(), "SELECT count(*), count(*) FILTER (WHERE dek_version = 2) FROM site_secrets WHERE site_id = $1", id).Scan(&stored, &sealedWith); err != nil || stored != 1 || sealedWith != 1 {
		t.Fatalf("secrets = %d, %d at version 2, %v; want the one secret sealed again", stored, sealedWith, err)
	}
	for _, as := range []string{"owner", "admin", "viewer"} {
		if got, err := e.openSecret(as, id); err != nil || got != "hunter2-hunter2" {
			t.Errorf("%s after the rotation = %q, %v", as, got, err)
		}
	}
	if _, err := e.sites.OpenSecret(t.Context(), e.user["editor"], e.ws, id, e.session("editor"), site.SecretPassword); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("the removed member opens = %v; want ErrNotFound", err)
	}

	e.a.vaultCall("/lock", nil, e.cookie["owner"], e.csrf["owner"])
	if r := e.call("owner", http.MethodDelete, "/members/"+e.user["viewer"].String(), nil); r.Status != http.StatusNoContent {
		t.Fatalf("remove with a locked vault = %d %s", r.Status, r.Body)
	}
	if version, pending := e.keyState(); version != 2 || !pending {
		t.Fatalf("key state = %d, %v; want the rotation waiting", version, pending)
	}
	if r := e.a.vaultCall("/unlock", map[string]string{"masterPassword": masterPassword}, e.cookie["owner"], e.csrf["owner"]); r.Status != http.StatusNoContent {
		t.Fatalf("unlock = %d %s", r.Status, r.Body)
	}
	if version, pending := e.keyState(); version != 3 || pending {
		t.Fatalf("key state after unlocking = %d, %v; want version 3", version, pending)
	}

	if r := e.call("admin", http.MethodPost, "/leave", nil); r.Status != http.StatusNoContent {
		t.Fatalf("leave = %d %s", r.Status, r.Body)
	}
	if version, pending := e.keyState(); version != 3 || !pending {
		t.Fatalf("key state after leaving = %d, %v; want the rotation waiting", version, pending)
	}
	if r := e.call("owner", http.MethodGet, "/members", nil); r.Status != http.StatusOK {
		t.Fatalf("list = %d", r.Status)
	}
	if version, pending := e.keyState(); version != 4 || pending {
		t.Fatalf("key state after listing = %d, %v; want version 4", version, pending)
	}
	if got, err := e.openSecret("owner", id); err != nil || got != "hunter2-hunter2" {
		t.Errorf("owner at the end = %q, %v", got, err)
	}
}
