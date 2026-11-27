package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/ratelimit"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// draftPath is where site drafts are tried.
func draftPath(ws uuid.UUID) string { return "/api/v1/workspaces/" + ws.String() + "/sites/test" }

// sftpDraft is the body of a site form for an SFTP server, with a password when one is given.
func sftpDraft(host, username, password string) map[string]any {
	body := map[string]any{"name": "Draft", "connector": "sftp", "config": map[string]any{"host": host, "username": username}}
	if password != "" {
		body["secrets"] = map[string]string{"password": password}
	}
	return body
}

// TestSiteDraft verifies trying a site form before it is saved.
//
// Scenarios:
//   - a new site is tried with the password typed in the form, listing the folder the server opens sessions in, or its default folder;
//     a server that cannot tell that folder, or cannot list it, is unavailable, and an unexpected failure is an internal error
//   - an edit tried with the password left blank uses the stored password with the settings of the form, not the saved ones; a
//     password typed in the edit replaces the stored one, so a wrong one is refused; nothing is saved
//   - a server key not trusted yet is host_key_unknown, and a vault that is locked when a stored password is needed is vault_locked
//   - settings that break the schema are invalid_config, an unknown site is site_not_found, and only those who may change sites may try
//     them; attempts are limited like quick connections
func TestSiteDraft(t *testing.T) {
	t.Parallel()
	e := newTeamEnv(t, func(d *httpapi.Deps) {
		d.Limits.QuickConnect = ratelimit.New(realClock{}, ratelimit.Rule{Burst: 100, Every: time.Second}, 10)
	})
	owner, csrf := e.cookie["owner"], e.csrf["owner"]
	try := func(body map[string]any) reply {
		t.Helper()
		return e.a.send(http.MethodPost, draftPath(e.ws), body, owner, csrf)
	}
	result := func(r reply) httpapi.SiteDraftTestResult {
		t.Helper()
		if r.Status != http.StatusOK {
			t.Fatalf("draft = %d %s", r.Status, r.Body)
		}
		var out httpapi.SiteDraftTestResult
		mustDecode(t, r.Body, &out)
		return out
	}
	if got := result(try(map[string]any{"site": sftpDraft("8.8.8.8", "deploy", "draft secret")})); got.Path != "/" || got.Entries != 1 {
		t.Errorf("new site = %+v; want the root with its folder", got)
	}
	inFolder := sftpDraft("8.8.8.8", "deploy", "draft secret")
	inFolder["remoteDir"] = "/draft"
	if got := result(try(map[string]any{"site": inFolder})); got.Path != "/draft" || got.Entries != 0 {
		t.Errorf("new site with a default folder = %+v", got)
	}

	if got := result(try(map[string]any{"site": sftpDraft("2.2.2.2", "deploy", "draft secret")})); got.Path != "/draft" {
		t.Errorf("server that opens sessions in a folder = %+v; want that folder listed", got)
	}
	expectProblem(t, try(map[string]any{"site": sftpDraft("3.3.3.3", "deploy", "draft secret")}), http.StatusBadGateway, "remote_unavailable")
	expectProblem(t, try(map[string]any{"site": sftpDraft("4.4.4.4", "deploy", "draft secret")}), http.StatusBadGateway, "remote_unavailable")
	expectProblem(t, try(map[string]any{"site": sftpDraft("5.5.5.5", "deploy", "draft secret")}), http.StatusInternalServerError, "internal_error")

	saved := e.a.send(http.MethodPost, "/api/v1/workspaces/"+e.ws.String()+"/sites", map[string]any{"name": "Saved", "connector": "ftp",
		"config": map[string]any{"host": "9.9.9.9", "username": "old"}, "secrets": map[string]string{"password": "draft secret"}}, owner, csrf)
	if saved.Status != http.StatusCreated {
		t.Fatalf("create = %d %s", saved.Status, saved.Body)
	}
	id := saved.site(t).Id
	if got := result(try(map[string]any{"siteId": id, "site": sftpDraft("8.8.4.4", "other", "")})); got.Entries != 1 {
		t.Errorf("edit with the stored password = %+v", got)
	}
	expectProblem(t, try(map[string]any{"siteId": id, "site": sftpDraft("8.8.4.4", "other", "typed wrong")}), http.StatusBadGateway, "remote_auth_failed")
	if after := e.a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + e.ws.String() + "/sites/" + id.String(), cookie: owner}); after.site(t).Connector != "ftp" {
		t.Error("trying a draft changed the saved site")
	}

	untrusted := try(map[string]any{"site": sftpDraft(unknownProbeHost, "deploy", "draft secret")})
	expectProblem(t, untrusted, http.StatusConflict, "host_key_unknown")
	params := untrusted.problem(t).Params
	if params == nil || (*params)["host"] != unknownProbeHost || (*params)["port"] != float64(22) {
		t.Errorf("untrusted peer address is missing or incorrect: %s", untrusted.Body)
	}
	expectProblem(t, try(map[string]any{"site": map[string]any{"name": "Bad", "connector": "sftp", "config": map[string]any{"host": "8.8.8.8"}}}), http.StatusUnprocessableEntity, "invalid_config")
	expectProblem(t, try(map[string]any{"siteId": uuid.New(), "site": sftpDraft("8.8.8.8", "u", "")}), http.StatusNotFound, "site_not_found")
	expectProblem(t, e.a.send(http.MethodPost, draftPath(e.ws), map[string]any{"site": sftpDraft("8.8.8.8", "u", "x")}, e.cookie["viewer"], e.csrf["viewer"]), http.StatusForbidden, "forbidden")
	if r := e.a.vaultCall("/lock", nil, owner, csrf); r.Status != http.StatusNoContent {
		t.Fatalf("lock = %d", r.Status)
	}
	expectProblem(t, try(map[string]any{"siteId": id, "site": sftpDraft("8.8.4.4", "other", "")}), http.StatusConflict, "vault_locked")
}

// TestSiteDraft_RateLimited verifies that trying drafts is limited like quick connections.
func TestSiteDraft_RateLimited(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	var last reply
	for range 4 {
		last = a.send(http.MethodPost, draftPath(ws), map[string]any{"site": sftpDraft("8.8.8.8", "u", "draft secret")}, cookie, csrf)
	}
	expectProblem(t, last, http.StatusTooManyRequests, "rate_limited")
}

// TestCreateSite_Copy verifies duplicating a site over HTTP.
//
// Scenarios:
//   - a site created with secretsFrom takes the stored password of that site, sealed for it, so the copy has a password of its own
//   - an unknown site to copy from is site_not_found
func TestCreateSite_Copy(t *testing.T) {
	t.Parallel()
	e := newTeamEnv(t)
	owner, csrf := e.cookie["owner"], e.csrf["owner"]
	sites := "/api/v1/workspaces/" + e.ws.String() + "/sites"
	original := e.a.send(http.MethodPost, sites, map[string]any{"name": "Original", "connector": "sftp", "config": map[string]any{"host": "8.8.8.8", "username": "a"}, "secrets": map[string]string{"password": "stored"}}, owner, csrf)
	if original.Status != http.StatusCreated {
		t.Fatalf("create = %d %s", original.Status, original.Body)
	}
	copied := e.a.send(http.MethodPost, sites, map[string]any{"name": "Original (copy)", "connector": "sftp", "config": map[string]any{"host": "8.8.8.8", "username": "b"}, "secretsFrom": original.site(t).Id}, owner, csrf)
	if copied.Status != http.StatusCreated || len(copied.site(t).Secrets) != 1 || copied.site(t).Id == original.site(t).Id {
		t.Fatalf("copy = %d %s", copied.Status, copied.Body)
	}
	expectProblem(t, e.a.send(http.MethodPost, sites, map[string]any{"name": "Orphan", "connector": "sftp", "config": map[string]any{"host": "8.8.8.8", "username": "c"}, "secretsFrom": uuid.New()}, owner, csrf), http.StatusNotFound, "site_not_found")
}
