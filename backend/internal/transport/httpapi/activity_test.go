package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/core/activity"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// activityOf lists the activity log of the environment's workspace.
func (e *fileEnv) activityOf(query string) []httpapi.ActivityEntry {
	e.a.t.Helper()
	r := e.a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + e.ws.String() + "/activity" + query, cookie: e.cookie})
	if r.Status != http.StatusOK {
		e.a.t.Fatalf("activity = %d %s", r.Status, r.Body)
	}
	var list []httpapi.ActivityEntry
	mustDecode(e.a.t, r.Body, &list)
	return list
}

// TestActivity_Log verifies the log of what a user did on their connections.
//
// Scenarios:
//   - creating, renaming and deleting entries, computing a checksum and finishing an upload each leave an entry with the site, the
//     path, the detail and no error code, newest first
//   - a failed action leaves an entry with its stable code
//   - the log can be narrowed to a site and limited, and it is private to the user who acted
//   - an action on a site that does not exist leaves nothing, and anonymous callers are refused
func TestActivity_Log(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	a := e.a
	other := e.secondSite()
	send := func(method, path string, body any) reply { return a.send(method, path, body, e.cookie, e.csrf) }

	if r := send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), map[string]string{"path": "/docs"}); r.Status != http.StatusCreated {
		t.Fatalf("mkdir = %d %s", r.Status, r.Body)
	}
	if r := a.upload(e.ws, e.site, q("path", "/docs/a.txt"), "hello", e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("upload = %d %s", r.Status, r.Body)
	}
	waitFor(t, "the upload to be logged", func() bool { return len(e.activityOf("")) >= 2 })
	send(http.MethodPost, filesPath(e.ws, e.site, "/rename", nil), map[string]string{"from": "/docs/a.txt", "to": "/docs/b.txt"})
	e.get("/checksum", q("path", "/docs/b.txt", "algorithm", "md5"), nil)
	send(http.MethodDelete, filesPath(e.ws, e.site, "", q("path", "/docs/b.txt")), nil)
	send(http.MethodDelete, filesPath(e.ws, e.site, "", q("path", "/docs/missing.txt")), nil)
	send(http.MethodPost, filesPath(e.ws, other, "/directories", nil), map[string]string{"path": "/elsewhere"})
	unknown := uuid.New()
	send(http.MethodDelete, filesPath(e.ws, unknown, "", q("path", "/x")), nil)

	all := e.activityOf("")
	kinds := make([]string, 0, len(all))
	for _, entry := range all {
		kinds = append(kinds, string(entry.Kind))
	}
	want := []string{"mkdir", "delete", "delete", "checksum", "rename", "transfer", "mkdir"}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v; want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v; want %v", kinds, want)
		}
	}
	if all[1].ErrorCode == nil || *all[1].ErrorCode != "path_not_found" || all[1].Path != "/docs/missing.txt" || all[1].SiteName != "Files" {
		t.Errorf("failed delete = %+v", all[1])
	}
	if all[2].ErrorCode != nil || all[2].SiteId == nil || *all[2].SiteId != e.site {
		t.Errorf("delete = %+v", all[2])
	}
	if all[3].Detail == nil || *all[3].Detail != "md5" || all[4].Detail == nil || *all[4].Detail != "/docs/b.txt" {
		t.Errorf("details = %v and %v", all[3].Detail, all[4].Detail)
	}
	if all[5].Detail == nil || *all[5].Detail != "upload:done" || all[5].Path != "/docs/a.txt" {
		t.Errorf("transfer = %+v", all[5])
	}

	if onSite := e.activityOf("?siteId=" + other.String()); len(onSite) != 1 || onSite[0].Path != "/elsewhere" || onSite[0].SiteName != "Second" {
		t.Errorf("entries of the second site = %+v", onSite)
	}
	if limited := e.activityOf("?limit=2"); len(limited) != 2 {
		t.Errorf("limited to 2 = %d entries", len(limited))
	}

	stranger, _ := a.signIn("stranger@example.com")
	if r := a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + e.ws.String() + "/activity", cookie: stranger}); r.Status != http.StatusOK || string(r.Body) != "[]\n" && string(r.Body) != "[]" {
		t.Errorf("another user sees %d %s", r.Status, r.Body)
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + e.ws.String() + "/activity"}), 401, "unauthenticated")
}

// brokenActivity is a log that cannot be written or read.
type brokenActivity struct{ *postgres.Store }

// InsertActivity always fails.
func (brokenActivity) InsertActivity(context.Context, activity.Entry) error {
	return errors.New("log is down")
}

// ActivityByUser always fails.
func (brokenActivity) ActivityByUser(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int) ([]activity.Entry, error) {
	return nil, errors.New("log is down")
}

// TestActivity_LogFailures verifies that a log that is down never fails what users do.
//
// Scenarios:
//   - creating a folder still works when the log cannot be written, and reading the log answers 500 without details
func TestActivity_LogFailures(t *testing.T) {
	t.Parallel()
	a := newAPI(t, func(d *httpapi.Deps) {
		d.Activity = activity.NewService(brokenActivity{d.Readiness.(*postgres.Store)}, realClock{}, time.Hour, nil)
	})
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	created := a.send(http.MethodPost, sitesPath(ws), siteBody("S"), cookie, csrf)
	site := created.site(t).Id

	if r := a.send(http.MethodPost, filesPath(ws, site, "/directories", nil), map[string]string{"path": "/d"}, cookie, csrf); r.Status != http.StatusCreated {
		t.Errorf("mkdir with a broken log = %d %s", r.Status, r.Body)
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + ws.String() + "/activity", cookie: cookie}), 500, "internal_error")
}
