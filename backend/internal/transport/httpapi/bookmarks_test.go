package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// bookmarksPath returns the bookmarks path of a site, or one bookmark when id is given.
func bookmarksPath(ws uuid.UUID, siteID string, id ...string) string {
	path := sitesPath(ws, siteID) + "/bookmarks"
	if len(id) > 0 {
		path += "/" + id[0]
	}
	return path
}

// bookmark decodes the reply as a bookmark.
func (r reply) bookmark(t *testing.T) httpapi.Bookmark {
	t.Helper()
	var b httpapi.Bookmark
	mustDecode(t, r.Body, &b)
	return b
}

// TestBookmarks_Lifecycle verifies bookmarking folders of a saved site over HTTP (4.14b).
//
// Scenarios:
//   - creating a bookmark returns it with a cleaned path and lists it; the list comes by name
//   - a taken name is 409 bookmark_name_taken, a bad name 422 invalid_bookmark_name, a relative path 422 invalid_bookmark_path, and a
//     malformed body 400
//   - renaming and moving returns the changed bookmark; a missing bookmark is 404 bookmark_not_found
//   - removing is 204 and removing again 404; a site that is not saved is 404 site_not_found
//   - creating, changing and removing are audited with the name, the site and the path
func TestBookmarks_Lifecycle(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	siteID := a.send(http.MethodPost, sitesPath(ws), siteBody("Prod"), cookie, csrf).site(t).Id.String()

	created := a.send(http.MethodPost, bookmarksPath(ws, siteID), map[string]string{"name": "Logs", "path": "/var/log/"}, cookie, csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Status, created.Body)
	}
	logs := created.bookmark(t)
	if logs.Name != "Logs" || logs.Path != "/var/log" || logs.CreatedAt.IsZero() {
		t.Errorf("created = %+v", logs)
	}
	a.send(http.MethodPost, bookmarksPath(ws, siteID), map[string]string{"name": "app", "path": "/srv/app"}, cookie, csrf)
	var list []httpapi.Bookmark
	mustDecode(t, a.do(request{method: http.MethodGet, path: bookmarksPath(ws, siteID), cookie: cookie}).Body, &list)
	if len(list) != 2 || list[0].Name != "app" || list[1].Name != "Logs" {
		t.Errorf("list = %+v; want app then Logs", list)
	}

	expectProblem(t, a.send(http.MethodPost, bookmarksPath(ws, siteID), map[string]string{"name": "LOGS", "path": "/"}, cookie, csrf), 409, "bookmark_name_taken")
	expectProblem(t, a.send(http.MethodPost, bookmarksPath(ws, siteID), map[string]string{"name": " ", "path": "/"}, cookie, csrf), 422, "invalid_bookmark_name")
	expectProblem(t, a.send(http.MethodPost, bookmarksPath(ws, siteID), map[string]string{"name": "Rel", "path": "var/log"}, cookie, csrf), 422, "invalid_bookmark_path")
	expectProblem(t, a.send(http.MethodPost, bookmarksPath(ws, siteID), "{oops", cookie, csrf), 400, "invalid_request")

	moved := a.send(http.MethodPatch, bookmarksPath(ws, siteID, logs.Id.String()), map[string]string{"name": "Nginx logs", "path": "/var/log/nginx"}, cookie, csrf)
	if got := moved.bookmark(t); moved.Status != http.StatusOK || got.Name != "Nginx logs" || got.Path != "/var/log/nginx" {
		t.Errorf("update = %d %s", moved.Status, moved.Body)
	}
	expectProblem(t, a.send(http.MethodPatch, bookmarksPath(ws, siteID, uuid.NewString()), map[string]string{"name": "x"}, cookie, csrf), 404, "bookmark_not_found")

	if r := a.send(http.MethodDelete, bookmarksPath(ws, siteID, logs.Id.String()), nil, cookie, csrf); r.Status != http.StatusNoContent {
		t.Fatalf("delete = %d %s", r.Status, r.Body)
	}
	expectProblem(t, a.send(http.MethodDelete, bookmarksPath(ws, siteID, logs.Id.String()), nil, cookie, csrf), 404, "bookmark_not_found")
	expectProblem(t, a.do(request{method: http.MethodGet, path: bookmarksPath(ws, uuid.NewString()), cookie: cookie}), 404, "site_not_found")

	var events []httpapi.AuditEvent
	mustDecode(t, a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + ws.String() + "/audit?action=bookmark.", cookie: cookie}).Body, &events)
	seen := map[string]bool{}
	for _, e := range events {
		if e.Meta["site"] == siteID && e.Meta["path"] != "" {
			seen[e.Action+":"+e.Target] = true
		}
	}
	for _, want := range []string{"bookmark.created:Logs", "bookmark.updated:Nginx logs", "bookmark.deleted:Nginx logs"} {
		if !seen[want] {
			t.Errorf("audit misses %s; events = %+v", want, events)
		}
	}
}

// TestBookmarks_AuthorizationMatrix verifies who may reach each bookmark endpoint.
//
// Scenarios:
//   - anonymous callers get 401 and writes without the anti-CSRF token get 403 csrf_invalid
//   - a stranger gets 404 workspace_not_found on every endpoint
//   - a viewer may list but gets 403 forbidden on create, update and delete
//   - an editor may create, update and delete
func TestBookmarks_AuthorizationMatrix(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	ownerCookie, ownerCSRF := a.setup()
	ws := a.personal(ownerCookie)
	siteID := a.send(http.MethodPost, sitesPath(ws), siteBody("Prod"), ownerCookie, ownerCSRF).site(t).Id.String()
	existing := a.send(http.MethodPost, bookmarksPath(ws, siteID), map[string]string{"name": "Home", "path": "/home"}, ownerCookie, ownerCSRF).bookmark(t).Id.String()
	strangerCookie, _ := a.signIn("stranger@example.com")
	viewerCookie, viewer := a.signIn("viewer@example.com")
	editorCookie, editor := a.signIn("editor@example.com")
	a.addMember(ws, viewer.ID, workspace.RoleViewer)
	a.addMember(ws, editor.ID, workspace.RoleEditor)
	csrfOf := func(c *http.Cookie) string {
		return a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: c}).session(t).CsrfToken
	}
	strangerCSRF, viewerCSRF, editorCSRF := csrfOf(strangerCookie), csrfOf(viewerCookie), csrfOf(editorCookie)

	calls := []struct {
		method, path string
		body         any
		ok           int
	}{
		{http.MethodGet, bookmarksPath(ws, siteID), nil, http.StatusOK},
		{http.MethodPost, bookmarksPath(ws, siteID), map[string]string{"name": "New", "path": "/new"}, http.StatusCreated},
		{http.MethodPatch, bookmarksPath(ws, siteID, existing), map[string]string{"name": "Renamed"}, http.StatusOK},
		{http.MethodDelete, bookmarksPath(ws, siteID, existing), nil, http.StatusNoContent},
	}
	for _, c := range calls {
		expectProblem(t, a.do(request{method: c.method, path: c.path, body: c.body}), 401, "unauthenticated")
		if c.method != http.MethodGet {
			expectProblem(t, a.do(request{method: c.method, path: c.path, body: c.body, cookie: editorCookie}), 403, "csrf_invalid")
		}
		expectProblem(t, a.send(c.method, c.path, c.body, strangerCookie, strangerCSRF), 404, "workspace_not_found")
		if c.method == http.MethodGet {
			if r := a.send(c.method, c.path, nil, viewerCookie, viewerCSRF); r.Status != c.ok {
				t.Errorf("viewer list = %d %s", r.Status, r.Body)
			}
			continue
		}
		expectProblem(t, a.send(c.method, c.path, c.body, viewerCookie, viewerCSRF), 403, "forbidden")
	}
	for _, c := range calls[1:] {
		if r := a.send(c.method, c.path, c.body, editorCookie, editorCSRF); r.Status != c.ok {
			t.Errorf("editor %s = %d %s", c.method, r.Status, r.Body)
		}
	}
}
