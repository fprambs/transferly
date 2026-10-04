package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/quick"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// quickPath returns the quick connections path of a workspace, or one connection when an id and a tail are given.
func quickPath(ws uuid.UUID, rest ...string) string {
	return "/api/v1/workspaces/" + ws.String() + "/quick-connections" + strings.Join(rest, "")
}

// quickBody returns the settings of a server to try.
func quickBody(host string) map[string]any {
	return map[string]any{"connector": "sftp", "config": map[string]any{"host": host, "username": "deploy"}, "secrets": map[string]any{"password": "hunter2-hunter2"}}
}

// quickConnection decodes the reply as a quick connection.
func (r reply) quickConnection(t *testing.T) httpapi.QuickConnection {
	t.Helper()
	var q httpapi.QuickConnection
	mustDecode(t, r.Body, &q)
	return q
}

// TestQuickConnections_Lifecycle verifies trying a server, using it and keeping it.
//
// Scenarios:
//   - a quick connection is created without contacting anything, named after its host, and never exposes credentials
//   - its identifier works in the file endpoints: browsing, creating folders, uploading and downloading, and the
//     transfers are recorded under the host name, with no site since nothing was saved
//   - saving needs the vault: locked it is 409 vault_locked and the quick connection stays; unlocked it becomes a
//     site in the chosen collection with its credentials sealed, the quick connection ends and stops working
//   - a taken site name keeps the quick connection
//   - closing ends it, twice is 404, and its identifier no longer reaches files
func TestQuickConnections_Lifecycle(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	created := a.send(http.MethodPost, quickPath(ws), quickBody("8.8.8.8"), cookie, csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Status, created.Body)
	}
	q := created.quickConnection(t)
	if q.Name != "8.8.8.8" || q.Connector != "sftp" || q.Config["port"] != "22" || q.Config["auth"] != "password" || q.Config["username"] != "deploy" {
		t.Errorf("quick connection = %+v", q)
	}
	if strings.Contains(string(created.Body), "hunter2") || strings.Contains(string(a.do(request{method: http.MethodGet, path: quickPath(ws), cookie: cookie}).Body), "hunter2") {
		t.Fatal("a response exposes the password")
	}
	var list []httpapi.QuickConnection
	mustDecode(t, a.do(request{method: http.MethodGet, path: quickPath(ws), cookie: cookie}).Body, &list)
	if len(list) != 1 || list[0].Id != q.Id {
		t.Errorf("list = %+v", list)
	}

	files := func(endpoint string, query map[string]string) string {
		return filesPath(ws, q.Id, endpoint, q2(query))
	}
	if r := a.send(http.MethodPost, files("/directories", nil), map[string]string{"path": "/docs"}, cookie, csrf); r.Status != http.StatusCreated {
		t.Fatalf("mkdir through a quick connection = %d %s", r.Status, r.Body)
	}
	up := a.do(request{method: http.MethodPut, path: files("/content", map[string]string{"path": "/docs/a.txt"}), body: "hello", cookie: cookie,
		headers: map[string]string{"X-CSRF-Token": csrf, "Content-Type": "application/octet-stream"}})
	if up.Status != http.StatusCreated {
		t.Fatalf("upload through a quick connection = %d %s", up.Status, up.Body)
	}
	if got := a.do(request{method: http.MethodGet, path: files("/content", map[string]string{"path": "/docs/a.txt"}), cookie: cookie}); string(got.Body) != "hello" {
		t.Errorf("download = %q", got.Body)
	}
	waitFor(t, "the transfers to be recorded", func() bool { return len(a.transfersOf(cookie, nil)) == 2 })
	for _, tr := range a.transfersOf(cookie, nil) {
		if tr.SiteName != "8.8.8.8" || tr.SiteId != nil {
			t.Errorf("transfer = %+v; want it recorded under the host, with no saved site", tr)
		}
	}

	collection := a.send(http.MethodPost, collectionsPath(ws), map[string]any{"name": "Trials"}, cookie, csrf).collection(t).Id
	expectProblem(t, a.send(http.MethodPost, quickPath(ws, "/", q.Id.String(), "/save"), map[string]any{"name": "Try 1", "collectionId": collection}, cookie, csrf), 409, "vault_locked")
	if list := a.do(request{method: http.MethodGet, path: quickPath(ws), cookie: cookie}); !strings.Contains(string(list.Body), q.Id.String()) {
		t.Fatal("a refused save ended the quick connection")
	}
	if r := a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, cookie, csrf); r.Status != http.StatusCreated {
		t.Fatalf("vault setup = %d", r.Status)
	}
	taken := a.send(http.MethodPost, sitesPath(ws), siteBody("Taken"), cookie, csrf)
	if taken.Status != http.StatusCreated {
		t.Fatal("could not create the clashing site")
	}
	expectProblem(t, a.send(http.MethodPost, quickPath(ws, "/", q.Id.String(), "/save"), map[string]any{"name": "taken"}, cookie, csrf), 409, "site_name_taken")

	saved := a.send(http.MethodPost, quickPath(ws, "/", q.Id.String(), "/save"), map[string]any{"name": "Try 1", "collectionId": collection}, cookie, csrf)
	if saved.Status != http.StatusCreated {
		t.Fatalf("save = %d %s", saved.Status, saved.Body)
	}
	site := saved.site(t)
	if site.Name != "Try 1" || site.Config["host"] != "8.8.8.8" || site.CollectionId == nil || *site.CollectionId != collection || len(site.Secrets) != 1 || site.Secrets[0] != "password" {
		t.Errorf("saved site = %+v", site)
	}
	if strings.Contains(string(saved.Body), "hunter2") {
		t.Error("the saved site exposes the password")
	}
	if list := a.do(request{method: http.MethodGet, path: quickPath(ws), cookie: cookie}); strings.Contains(string(list.Body), q.Id.String()) {
		t.Error("the quick connection survived being saved")
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: files("", map[string]string{"path": "/"}), cookie: cookie}), 404, "site_not_found")
	expectProblem(t, a.send(http.MethodPost, quickPath(ws, "/", q.Id.String(), "/save"), map[string]any{"name": "Again"}, cookie, csrf), 404, "quick_connection_not_found")

	second := a.send(http.MethodPost, quickPath(ws), quickBody("1.1.1.1"), cookie, csrf).quickConnection(t)
	if r := a.send(http.MethodDelete, quickPath(ws, "/", second.Id.String()), nil, cookie, csrf); r.Status != http.StatusNoContent {
		t.Errorf("close = %d %s", r.Status, r.Body)
	}
	expectProblem(t, a.send(http.MethodDelete, quickPath(ws, "/", second.Id.String()), nil, cookie, csrf), 404, "quick_connection_not_found")
	expectProblem(t, a.do(request{method: http.MethodGet, path: filesPath(ws, second.Id, "", q2(map[string]string{"path": "/"})), cookie: cookie}), 404, "site_not_found")
}

// q2 builds query parameters from a map.
func q2(m map[string]string) map[string][]string {
	out := map[string][]string{}
	for k, v := range m {
		out[k] = []string{v}
	}
	return out
}

// TestQuickConnections_Rules verifies the limits and the ownership of quick connections.
//
// Scenarios:
//   - invalid settings and private destinations are 422 with stable codes, malformed bodies 400
//   - starting connections is rate limited with Retry-After, and a user keeps at most five
//   - other users cannot use, list, close or save a connection they do not own
//   - signing out ends the user's quick connections
func TestQuickConnections_Rules(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	bad := map[string]func(map[string]any){
		"invalid_config":      func(b map[string]any) { set(b, "port", "70000") },
		"destination_blocked": func(b map[string]any) { set(b, "host", "169.254.169.254") },
	}
	for code, mutate := range bad {
		a.clock.Advance(time.Minute)
		body := quickBody("8.8.8.8")
		mutate(body)
		expectProblem(t, a.send(http.MethodPost, quickPath(ws), body, cookie, csrf), 422, code)
	}
	a.clock.Advance(time.Minute)
	expectProblem(t, a.send(http.MethodPost, quickPath(ws), "{not json", cookie, csrf), 400, "invalid_request")

	a.clock.Advance(40 * time.Second)
	var first httpapi.QuickConnection
	for i := 0; i < 3; i++ {
		r := a.send(http.MethodPost, quickPath(ws), quickBody("8.8.4.4"), cookie, csrf)
		if r.Status != http.StatusCreated {
			t.Fatalf("connection %d = %d %s", i+1, r.Status, r.Body)
		}
		if i == 0 {
			first = r.quickConnection(t)
		}
	}
	limited := a.send(http.MethodPost, quickPath(ws), quickBody("8.8.4.4"), cookie, csrf)
	expectProblem(t, limited, 429, "rate_limited")
	if limited.Header.Get("Retry-After") == "" {
		t.Error("the rate limited answer has no Retry-After")
	}
	a.clock.Advance(40 * time.Second)
	for i := 0; i < 2; i++ {
		a.send(http.MethodPost, quickPath(ws), quickBody("9.9.9.9"), cookie, csrf)
	}
	a.clock.Advance(40 * time.Second)
	expectProblem(t, a.send(http.MethodPost, quickPath(ws), quickBody("9.9.9.9"), cookie, csrf), 409, "too_many_quick_connections")

	otherCookie, other := a.signIn("other@example.com")
	otherCSRF := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: otherCookie}).session(t).CsrfToken
	a.addMember(ws, other.ID, workspace.RoleEditor)
	if list := a.do(request{method: http.MethodGet, path: quickPath(ws), cookie: otherCookie}); strings.Contains(string(list.Body), first.Id.String()) {
		t.Error("another user lists someone else's quick connection")
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: filesPath(ws, first.Id, "", q2(map[string]string{"path": "/"})), cookie: otherCookie}), 404, "site_not_found")
	expectProblem(t, a.send(http.MethodDelete, quickPath(ws, "/", first.Id.String()), nil, otherCookie, otherCSRF), 404, "quick_connection_not_found")
	expectProblem(t, a.send(http.MethodPost, quickPath(ws, "/", first.Id.String(), "/save"), map[string]any{"name": "Stolen"}, otherCookie, otherCSRF), 404, "quick_connection_not_found")

	if r := a.do(request{method: http.MethodPost, path: "/api/v1/auth/logout", cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf}}); r.Status != http.StatusNoContent {
		t.Fatalf("logout = %d", r.Status)
	}
	again := a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": adminEmail, "password": adminPassword}})
	if list := a.do(request{method: http.MethodGet, path: quickPath(ws), cookie: again.sessionCookie()}); string(list.Body) != "[]\n" && string(list.Body) != "[]" {
		t.Errorf("quick connections after signing out = %s; want none", list.Body)
	}
}

// TestQuickConnections_AuthorizationMatrix verifies who may reach each quick connection endpoint.
//
// Scenarios:
//   - anonymous callers get 401 and writers without the anti-CSRF token 403 csrf_invalid
//   - a stranger gets 404 workspace_not_found
//   - a viewer may list (their own, none) but gets 403 forbidden creating; an editor may create
func TestQuickConnections_AuthorizationMatrix(t *testing.T) {
	a := newAPI(t)
	ownerCookie, _ := a.setup()
	ws := a.personal(ownerCookie)
	strangerCookie, _ := a.signIn("stranger@example.com")
	viewerCookie, viewer := a.signIn("viewer@example.com")
	editorCookie, editor := a.signIn("editor@example.com")
	a.addMember(ws, viewer.ID, workspace.RoleViewer)
	a.addMember(ws, editor.ID, workspace.RoleEditor)
	csrfOf := func(c *http.Cookie) string {
		return a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: c}).session(t).CsrfToken
	}
	strangerCSRF, viewerCSRF, editorCSRF := csrfOf(strangerCookie), csrfOf(viewerCookie), csrfOf(editorCookie)
	id := uuid.NewString()

	calls := []struct {
		name, method, path string
		body               any
	}{
		{"list", http.MethodGet, quickPath(ws), nil},
		{"create", http.MethodPost, quickPath(ws), quickBody("8.8.8.8")},
		{"close", http.MethodDelete, quickPath(ws, "/", id), nil},
		{"save", http.MethodPost, quickPath(ws, "/", id, "/save"), map[string]any{"name": "X"}},
	}
	for _, c := range calls {
		expectProblem(t, a.do(request{method: c.method, path: c.path, body: c.body}), 401, "unauthenticated")
		if c.method != http.MethodGet {
			expectProblem(t, a.do(request{method: c.method, path: c.path, body: c.body, cookie: editorCookie}), 403, "csrf_invalid")
		}
	}
	for _, c := range []int{0, 1} {
		expectProblem(t, a.send(calls[c].method, calls[c].path, calls[c].body, strangerCookie, strangerCSRF), 404, "workspace_not_found")
	}
	if r := a.send(calls[0].method, calls[0].path, nil, viewerCookie, viewerCSRF); r.Status != http.StatusOK {
		t.Errorf("viewer list = %d %s", r.Status, r.Body)
	}
	expectProblem(t, a.send(calls[1].method, calls[1].path, calls[1].body, viewerCookie, viewerCSRF), 403, "forbidden")
	if r := a.send(calls[1].method, calls[1].path, calls[1].body, editorCookie, editorCSRF); r.Status != http.StatusCreated {
		t.Errorf("editor create = %d %s", r.Status, r.Body)
	}
}

// flakyAuthz allows everything until told to fail.
type flakyAuthz struct{ fail bool }

// Authorize allows the action, or fails with an unexpected error.
func (f *flakyAuthz) Authorize(context.Context, uuid.UUID, uuid.UUID, workspace.Action) (workspace.Role, error) {
	if f.fail {
		return "", errInjected
	}
	return workspace.RoleOwner, nil
}

// failingSaver settles quick connections with the real rules but cannot save sites.
type failingSaver struct{ *site.Service }

// Create always fails.
func (failingSaver) Create(context.Context, uuid.UUID, uuid.UUID, string, site.Input, site.SecretChanges) (site.Site, error) {
	return site.Site{}, errInjected
}

// TestQuickConnectionFailures verifies that unexpected failures are 500 problems without details.
//
// Scenarios:
//   - listing and creating answer 500 when authorization fails unexpectedly, and saving does when the site cannot be stored
func TestQuickConnectionFailures(t *testing.T) {
	authz := &flakyAuthz{}
	a := newAPI(t, func(d *httpapi.Deps) {
		d.Quick = quick.NewService(authz, failingSaver{d.Sites}, realClock{}, nil)
	})
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	q := a.send(http.MethodPost, quickPath(ws), quickBody("8.8.8.8"), cookie, csrf).quickConnection(t)

	expectProblem(t, a.send(http.MethodPost, quickPath(ws, "/", q.Id.String(), "/save"), map[string]any{"name": "X"}, cookie, csrf), 500, "internal_error")
	authz.fail = true
	expectProblem(t, a.do(request{method: http.MethodGet, path: quickPath(ws), cookie: cookie}), 500, "internal_error")
	expectProblem(t, a.send(http.MethodPost, quickPath(ws), quickBody("8.8.4.4"), cookie, csrf), 500, "internal_error")
}
