package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/workspace"
)

// TestPhaseTwoEndpoints_AuthorizationMatrix verifies who may reach the endpoints added in phase 2.
//
// Scenarios:
//   - anonymous callers get 401 and writers without the anti-CSRF token get 403 csrf_invalid
//   - a stranger gets 404 workspace_not_found on the archive, the checksum, the time zone probe, the copy and the import
//   - a viewer may download an archive and read a checksum, but gets 403 forbidden on the time zone probe, the copy and the import
//   - an editor may copy and import
//   - a stranger reads an empty activity log, and never the entries of the owner
//   - a copy that names a site of another workspace is 404 site_not_found
func TestPhaseTwoEndpoints_AuthorizationMatrix(t *testing.T) {
	e := newFileEnv(t)
	a := e.a
	other := e.secondSite()
	strangerCookie, _ := a.signIn("stranger@example.com")
	viewerCookie, viewer := a.signIn("viewer@example.com")
	editorCookie, editor := a.signIn("editor@example.com")
	a.addMember(e.ws, viewer.ID, workspace.RoleViewer)
	a.addMember(e.ws, editor.ID, workspace.RoleEditor)
	if r := a.upload(e.ws, e.site, q("path", "/f"), "data", e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("upload = %d %s", r.Status, r.Body)
	}
	if r := a.send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), map[string]string{"path": "/dir"}, e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("mkdir = %d %s", r.Status, r.Body)
	}
	csrfOf := func(c *http.Cookie) string {
		return a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: c}).session(t).CsrfToken
	}
	strangerCSRF, viewerCSRF, editorCSRF := csrfOf(strangerCookie), csrfOf(viewerCookie), csrfOf(editorCookie)
	workspacePath := "/api/v1/workspaces/" + e.ws.String()

	type call struct {
		name, method, path string
		body               any
		viewerMay          bool
	}
	calls := []call{
		{name: "archive", method: http.MethodGet, path: filesPath(e.ws, e.site, "/archive", q("path", "/dir", "transferId", uuid.NewString())), viewerMay: true},
		{name: "checksum", method: http.MethodGet, path: filesPath(e.ws, e.site, "/checksum", q("path", "/f")), viewerMay: true},
		{name: "probe", method: http.MethodPost, path: filesPath(e.ws, e.site, "/timezone-probe", q("path", "/dir"))},
		{name: "copy", method: http.MethodPost, path: workspacePath + "/copies", body: e.copyBody(other, "/f", "/g", nil)},
		{name: "import", method: http.MethodPost, path: workspacePath + "/imports/filezilla?dryRun=true", body: manager},
	}
	send := func(c call, cookie *http.Cookie, csrf string) reply {
		headers := map[string]string{"X-CSRF-Token": csrf}
		if c.name == "import" {
			headers["Content-Type"] = "application/xml"
		}
		return a.do(request{method: c.method, path: c.path, body: c.body, cookie: cookie, headers: headers})
	}
	for _, c := range calls {
		expectProblem(t, a.do(request{method: c.method, path: c.path, body: c.body}), 401, "unauthenticated")
		if c.method == http.MethodPost {
			expectProblem(t, send(c, editorCookie, ""), 403, "csrf_invalid")
		}
		expectProblem(t, send(c, strangerCookie, strangerCSRF), 404, "workspace_not_found")
		if c.viewerMay {
			if r := send(c, viewerCookie, viewerCSRF); r.Status != http.StatusOK {
				t.Errorf("viewer %s = %d %s; want 200", c.name, r.Status, r.Body)
			}
		} else {
			expectProblem(t, send(c, viewerCookie, viewerCSRF), 403, "forbidden")
		}
	}
	if r := send(calls[3], editorCookie, editorCSRF); r.Status != http.StatusAccepted {
		t.Errorf("editor copy = %d %s; want 202", r.Status, r.Body)
	}
	if r := send(calls[4], editorCookie, editorCSRF); r.Status != http.StatusOK {
		t.Errorf("editor import = %d %s; want 200", r.Status, r.Body)
	}

	ghost := e.copyBody(uuid.New(), "/f", "/h", nil)
	expectProblem(t, a.send(http.MethodPost, workspacePath+"/copies", ghost, e.cookie, e.csrf), 404, "site_not_found")

	own := a.do(request{method: http.MethodGet, path: workspacePath + "/activity", cookie: e.cookie})
	if own.Status != http.StatusOK || strings.TrimSpace(string(own.Body)) == "[]" {
		t.Fatalf("owner activity = %d %s; want entries", own.Status, own.Body)
	}
	foreign := a.do(request{method: http.MethodGet, path: workspacePath + "/activity", cookie: strangerCookie})
	if foreign.Status != http.StatusOK || strings.TrimSpace(string(foreign.Body)) != "[]" {
		t.Errorf("stranger activity = %d %s; want an empty list", foreign.Status, foreign.Body)
	}
}
