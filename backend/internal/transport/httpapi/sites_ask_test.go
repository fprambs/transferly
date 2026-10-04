package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestSites_AskForPassword covers the logon type that asks for the password when connecting (ADR-0010): creating such a
// site, [httpapi.Server.SetSiteCredential], and what drops the typed password.
//
// Use cases:
//   - a site that asks is saved with no secret even when the request sends a password, and needs no unlocked vault
//   - its folder cannot be listed until the session types the password, and then can
//   - the password is refused when empty, for a site that does not ask, and for a site that does not exist, and it is
//     never echoed back
//   - locking the vault and changing the site drop the password, so the folder needs it again
func TestSites_AskForPassword(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	body := secret(set(siteBody("Asks"), "auth", "ask"), "password", "ignored")
	created := a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Status, created.Body)
	}
	asking := created.site(t)
	if asking.Config["auth"] != "ask" || len(asking.Secrets) != 0 {
		t.Fatalf("created = %+v; want ask with no secret", asking)
	}
	list := func() reply {
		return a.do(request{method: http.MethodGet, path: filesPath(ws, asking.Id, "", q("path", "/")), cookie: cookie})
	}
	credential := func(id string, password string) reply {
		return a.send(http.MethodPut, sitesPath(ws, id)+"/credential", map[string]any{"password": password, "remember": true}, cookie, csrf)
	}
	expectProblem(t, list(), http.StatusConflict, "credentials_required")

	expectProblem(t, credential(asking.Id.String(), ""), http.StatusUnprocessableEntity, "password_required")
	plain := a.send(http.MethodPost, sitesPath(ws), siteBody("Plain"), cookie, csrf).site(t)
	expectProblem(t, credential(plain.Id.String(), "pw"), http.StatusConflict, "credentials_not_asked")
	expectProblem(t, credential(uuid.NewString(), "pw"), http.StatusNotFound, "site_not_found")

	given := credential(asking.Id.String(), "typed-password")
	if given.Status != http.StatusNoContent || strings.Contains(string(given.Body), "typed-password") {
		t.Fatalf("credential = %d %s", given.Status, given.Body)
	}
	if r := list(); r.Status != http.StatusOK {
		t.Fatalf("list with the password = %d %s", r.Status, r.Body)
	}

	if r := a.vaultCall("/lock", nil, cookie, csrf); r.Status != http.StatusNoContent {
		t.Fatalf("lock = %d %s", r.Status, r.Body)
	}
	expectProblem(t, list(), http.StatusConflict, "credentials_required")

	credential(asking.Id.String(), "typed-password")
	renamed := set(siteBody("Asks again"), "auth", "ask")
	if r := a.send(http.MethodPut, sitesPath(ws, asking.Id.String()), renamed, cookie, csrf); r.Status != http.StatusOK {
		t.Fatalf("update = %d %s", r.Status, r.Body)
	}
	expectProblem(t, list(), http.StatusConflict, "credentials_required")
}
