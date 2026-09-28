package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/transport/httpapi"

	"github.com/fprambs/sentinel/internal/testutil"
)

// siteBody returns valid site settings with the given name.
func siteBody(name string) map[string]any {
	return map[string]any{"name": name, "protocol": "sftp", "host": "8.8.8.8", "username": "deploy"}
}

// sitesPath returns the sites collection path of a workspace, or one site when id is given.
func sitesPath(ws uuid.UUID, id ...string) string {
	path := "/api/v1/workspaces/" + ws.String() + "/sites"
	if len(id) > 0 {
		path += "/" + id[0]
	}
	return path
}

// send performs a state-changing call with the session and its anti-CSRF token.
func (a *api) send(method, path string, body any, cookie *http.Cookie, csrf string) reply {
	a.t.Helper()
	return a.do(request{method: method, path: path, body: body, cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf}})
}

// site decodes the reply as a site.
func (r reply) site(t *testing.T) httpapi.Site {
	t.Helper()
	var s httpapi.Site
	mustDecode(t, r.Body, &s)
	return s
}

// personal returns the identifier of the personal workspace of a session.
func (a *api) personal(cookie *http.Cookie) uuid.UUID {
	a.t.Helper()
	return a.do(request{method: http.MethodGet, path: "/api/v1/workspaces", cookie: cookie}).workspaces(a.t)[0].Id
}

// TestSites_Lifecycle verifies saving, reading, changing and deleting sites over HTTP.
//
// Scenarios:
//   - a site is created with defaults, listed and read back, and no response ever contains a secret
//   - secrets are reported by kind; writing one while the vault is locked is 409 vault_locked
//   - an update replaces the settings, keeps the secrets it does not mention and removes emptied ones
//   - duplicate names are 409, invalid settings, private destinations and oversized secrets are 422 with stable codes
//   - deleting removes the site; reading, updating or deleting it again is 404 site_not_found
func TestSites_Lifecycle(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	created := a.send(http.MethodPost, sitesPath(ws), siteBody("Production"), cookie, csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Status, created.Body)
	}
	site := created.site(t)
	if site.Port != 22 || site.AuthMethod != "password" || site.ListTimezone != "UTC" || len(site.Secrets) != 0 {
		t.Errorf("created site = %+v; want defaults", site)
	}
	id := site.Id.String()

	locked := siteBody("With secret")
	locked["password"] = "hunter2-hunter2"
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), locked, cookie, csrf), 409, "vault_locked")
	if r := a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, cookie, csrf); r.Status != http.StatusCreated {
		t.Fatalf("vault setup = %d %s", r.Status, r.Body)
	}
	withSecret := a.send(http.MethodPost, sitesPath(ws), locked, cookie, csrf)
	if withSecret.Status != http.StatusCreated || len(withSecret.site(t).Secrets) != 1 || withSecret.site(t).Secrets[0] != "password" {
		t.Fatalf("create with secret = %d %s", withSecret.Status, withSecret.Body)
	}
	secretID := withSecret.site(t).Id.String()

	for _, r := range []reply{
		withSecret,
		a.do(request{method: http.MethodGet, path: sitesPath(ws, secretID), cookie: cookie}),
		a.do(request{method: http.MethodGet, path: sitesPath(ws), cookie: cookie}),
	} {
		if strings.Contains(string(r.Body), "hunter2") {
			t.Fatalf("a response contains the secret: %s", r.Body)
		}
	}
	list := a.do(request{method: http.MethodGet, path: sitesPath(ws), cookie: cookie})
	var sites []httpapi.Site
	mustDecode(t, list.Body, &sites)
	if len(sites) != 2 || sites[0].Name != "Production" || sites[1].Name != "With secret" {
		t.Errorf("list = %s", list.Body)
	}

	update := siteBody("With secret")
	update["port"], update["protocol"], update["host"], update["authMethod"], update["listTimezone"] = 2222, "sftp", "example.com", "key", "America/Santiago"
	update["privateKey"] = testutil.PrivateKey(t, "")
	update["proxy"] = map[string]any{"kind": "socks5", "host": "8.8.4.4", "port": 1080, "username": "proxyuser"}
	update["proxyPassword"] = "proxypass"
	update["jump"] = map[string]any{"host": "1.1.1.1", "port": 22, "username": "bastion", "authMethod": "password"}
	update["jumpPassword"] = "bastionpass"
	updated := a.send(http.MethodPut, sitesPath(ws, secretID), update, cookie, csrf)
	if updated.Status != http.StatusOK {
		t.Fatalf("update = %d %s", updated.Status, updated.Body)
	}
	if got := updated.site(t); got.Port != 2222 || got.AuthMethod != "key" || got.ListTimezone != "America/Santiago" || len(got.Secrets) != 4 {
		t.Errorf("updated = %+v; want the password kept, the key, the proxy password and the jump password added", got)
	}
	if got := updated.site(t); got.Proxy == nil || got.Proxy.Host != "8.8.4.4" || got.Proxy.Port != 1080 || got.Proxy.Username == nil || *got.Proxy.Username != "proxyuser" {
		t.Errorf("updated proxy = %+v", got.Proxy)
	}
	if got := updated.site(t); got.Jump == nil || got.Jump.Host != "1.1.1.1" || got.Jump.Username != "bastion" || got.Jump.AuthMethod != "password" {
		t.Errorf("updated jump = %+v", got.Jump)
	}
	update["password"], update["proxyPassword"], update["jumpPassword"] = "", "", ""
	if got := a.send(http.MethodPut, sitesPath(ws, secretID), update, cookie, csrf).site(t); len(got.Secrets) != 1 || got.Secrets[0] != "private_key" {
		t.Errorf("after emptying the password, proxy password and jump password = %+v", got.Secrets)
	}

	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), siteBody("production"), cookie, csrf), 409, "site_name_taken")
	expectProblem(t, a.send(http.MethodPut, sitesPath(ws, secretID), siteBody("PRODUCTION"), cookie, csrf), 409, "site_name_taken")
	invalid := map[string]func(map[string]any){
		"invalid_name":        func(b map[string]any) { b["name"] = " " },
		"invalid_protocol":    func(b map[string]any) { b["protocol"] = "gopher" },
		"invalid_host":        func(b map[string]any) { b["host"] = "ftp://x" },
		"invalid_port":        func(b map[string]any) { b["port"] = 70000 },
		"invalid_username":    func(b map[string]any) { b["username"] = "" },
		"invalid_auth_method": func(b map[string]any) { b["authMethod"], b["protocol"] = "key", "ftp" },
		"invalid_timezone":    func(b map[string]any) { b["listTimezone"] = "Mars/Base" },
		"invalid_charset":     func(b map[string]any) { b["charset"] = "klingon" },
		"destination_blocked": func(b map[string]any) { b["host"] = "169.254.169.254" },
		"secret_too_large":    func(b map[string]any) { b["password"] = strings.Repeat("p", 2000) },
		"invalid_speed_limit": func(b map[string]any) { b["speedLimitBps"] = -1 },
		"invalid_proxy": func(b map[string]any) {
			b["proxy"] = map[string]any{"kind": "wireguard", "host": "p.example", "port": 1080}
		},
		"invalid_jump": func(b map[string]any) {
			b["protocol"], b["jump"] = "ftp", map[string]any{"host": "b.example", "port": 22, "username": "u", "authMethod": "password"}
		},
	}
	for code, mutate := range invalid {
		body := siteBody("Invalid " + code)
		mutate(body)
		status := 422
		expectProblem(t, a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf), status, code)
	}
	unresolvable := siteBody("Nowhere")
	unresolvable["host"] = "nowhere.invalid"
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), unresolvable, cookie, csrf), 422, "host_unresolvable")

	if r := a.send(http.MethodDelete, sitesPath(ws, id), nil, cookie, csrf); r.Status != http.StatusNoContent {
		t.Fatalf("delete = %d %s", r.Status, r.Body)
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: sitesPath(ws, id), cookie: cookie}), 404, "site_not_found")
	expectProblem(t, a.send(http.MethodPut, sitesPath(ws, id), siteBody("Ghost"), cookie, csrf), 404, "site_not_found")
	expectProblem(t, a.send(http.MethodDelete, sitesPath(ws, id), nil, cookie, csrf), 404, "site_not_found")
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), "{not json", cookie, csrf), 400, "invalid_request")
}

// TestSites_AuthorizationMatrix verifies who may reach each site endpoint, by role and workspace.
//
// Scenarios:
//   - anonymous callers get 401 everywhere and callers without the anti-CSRF token get 403 csrf_invalid on writes
//   - a stranger gets 404 workspace_not_found on every endpoint, even with a real site identifier
//   - a viewer may list and read but gets 403 forbidden on create, update and delete
//   - an editor may create, update and delete
//   - a site of one workspace is 404 when addressed through another workspace of the same user
func TestSites_AuthorizationMatrix(t *testing.T) {
	a := newAPI(t)
	ownerCookie, ownerCSRF := a.setup()
	ws := a.personal(ownerCookie)
	strangerCookie, strangerUser := a.signIn("stranger@example.com")
	viewerCookie, viewer := a.signIn("viewer@example.com")
	editorCookie, editor := a.signIn("editor@example.com")
	a.addMember(ws, viewer.ID, workspace.RoleViewer)
	a.addMember(ws, editor.ID, workspace.RoleEditor)
	_ = strangerUser

	created := a.send(http.MethodPost, sitesPath(ws), siteBody("Production"), ownerCookie, ownerCSRF).site(t)
	id := created.Id.String()
	csrfOf := func(c *http.Cookie) string {
		return a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: c}).session(t).CsrfToken
	}
	strangerCSRF, viewerCSRF, editorCSRF := csrfOf(strangerCookie), csrfOf(viewerCookie), csrfOf(editorCookie)

	type call struct {
		name, method, path string
		body               any
	}
	calls := []call{
		{"list", http.MethodGet, sitesPath(ws), nil},
		{"get", http.MethodGet, sitesPath(ws, id), nil},
		{"create", http.MethodPost, sitesPath(ws), siteBody("New")},
		{"update", http.MethodPut, sitesPath(ws, id), siteBody("Production")},
		{"delete", http.MethodDelete, sitesPath(ws, uuid.NewString()), nil},
	}
	for _, c := range calls {
		expectProblem(t, a.do(request{method: c.method, path: c.path, body: c.body}), 401, "unauthenticated")
		if c.method != http.MethodGet {
			expectProblem(t, a.do(request{method: c.method, path: c.path, body: c.body, cookie: editorCookie}), 403, "csrf_invalid")
		}
		expectProblem(t, a.send(c.method, c.path, c.body, strangerCookie, strangerCSRF), 404, "workspace_not_found")
	}

	for _, c := range calls[:2] {
		if r := a.send(c.method, c.path, c.body, viewerCookie, viewerCSRF); r.Status != http.StatusOK {
			t.Errorf("viewer %s = %d %s; want 200", c.name, r.Status, r.Body)
		}
	}
	for _, c := range calls[2:] {
		expectProblem(t, a.send(c.method, c.path, c.body, viewerCookie, viewerCSRF), 403, "forbidden")
	}

	if r := a.send(http.MethodPost, sitesPath(ws), siteBody("By editor"), editorCookie, editorCSRF); r.Status != http.StatusCreated {
		t.Errorf("editor create = %d %s", r.Status, r.Body)
	}
	if r := a.send(http.MethodPut, sitesPath(ws, id), siteBody("Renamed"), editorCookie, editorCSRF); r.Status != http.StatusOK {
		t.Errorf("editor update = %d %s", r.Status, r.Body)
	}
	if r := a.send(http.MethodDelete, sitesPath(ws, id), nil, editorCookie, editorCSRF); r.Status != http.StatusNoContent {
		t.Errorf("editor delete = %d %s", r.Status, r.Body)
	}

	strangerWS := a.personal(strangerCookie)
	survivor := a.send(http.MethodPost, sitesPath(ws), siteBody("Survivor"), ownerCookie, ownerCSRF).site(t).Id.String()
	expectProblem(t, a.do(request{method: http.MethodGet, path: sitesPath(strangerWS, survivor), cookie: strangerCookie}), 404, "site_not_found")
	expectProblem(t, a.send(http.MethodPut, sitesPath(strangerWS, survivor), siteBody("Taken over"), strangerCookie, strangerCSRF), 404, "site_not_found")
	expectProblem(t, a.send(http.MethodDelete, sitesPath(strangerWS, survivor), nil, strangerCookie, strangerCSRF), 404, "site_not_found")
	if r := a.do(request{method: http.MethodGet, path: sitesPath(ws, survivor), cookie: ownerCookie}); r.Status != http.StatusOK {
		t.Errorf("the survivor was affected by another workspace: %d", r.Status)
	}
}

// TestSites_Charset verifies the character set of the file names of a site.
//
// Scenarios:
//   - a site is UTF-8 unless told otherwise, an FTP site can use a legacy character set, which is kept when the site is changed, and the name of the
//     character set is not case sensitive
//   - SFTP sites only use UTF-8
func TestSites_Charset(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	plain := a.send(http.MethodPost, sitesPath(ws), siteBody("Plain"), cookie, csrf)
	if got := plain.site(t).Charset; plain.Status != http.StatusCreated || got != "utf-8" {
		t.Errorf("default charset = %q (%d)", got, plain.Status)
	}
	legacy := siteBody("Legacy")
	legacy["protocol"], legacy["charset"] = "ftp", "Windows-1252"
	made := a.send(http.MethodPost, sitesPath(ws), legacy, cookie, csrf)
	if made.Status != http.StatusCreated || made.site(t).Charset != "windows-1252" {
		t.Fatalf("legacy site = %d %s", made.Status, made.Body)
	}
	legacy["charset"] = "koi8-r"
	if r := a.send(http.MethodPut, sitesPath(ws, made.site(t).Id.String()), legacy, cookie, csrf); r.Status != http.StatusOK || r.site(t).Charset != "koi8-r" {
		t.Errorf("update = %d %s", r.Status, r.Body)
	}
	sftp := siteBody("Secure")
	sftp["charset"] = "iso-8859-1"
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), sftp, cookie, csrf), 422, "invalid_charset")
}
