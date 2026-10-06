package httpapi_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/transport/httpapi"

	"github.com/fprambs/transferly/internal/testutil"
)

// siteBody returns valid site settings with the given name.
func siteBody(name string) map[string]any {
	return map[string]any{"name": name, "connector": "sftp", "config": map[string]any{"host": "8.8.8.8", "username": "deploy"}}
}

// set changes settings of the configuration in the body of a site or a quick connection, given as name and value pairs.
func set(body map[string]any, settings ...string) map[string]any {
	config, ok := body["config"].(map[string]any)
	if !ok {
		config = map[string]any{}
		body["config"] = config
	}
	for i := 0; i+1 < len(settings); i += 2 {
		config[settings[i]] = settings[i+1]
	}
	return body
}

// secret sets secrets in the body of a site or a quick connection, given as kind and value pairs.
func secret(body map[string]any, secrets ...string) map[string]any {
	changes, ok := body["secrets"].(map[string]any)
	if !ok {
		changes = map[string]any{}
		body["secrets"] = changes
	}
	for i := 0; i+1 < len(secrets); i += 2 {
		changes[secrets[i]] = secrets[i+1]
	}
	return body
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
//   - duplicate names are 409; invalid site settings, private destinations and oversized secrets are 422 with stable codes, and a
//     configuration or a secret that breaks the connector's schema is 422 invalid_config naming the field
//   - deleting removes the site; reading, updating or deleting it again is 404 site_not_found
func TestSites_Lifecycle(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	created := a.send(http.MethodPost, sitesPath(ws), siteBody("Production"), cookie, csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Status, created.Body)
	}
	site := created.site(t)
	if site.Connector != "sftp" || site.Config["port"] != "22" || site.Config["auth"] != "password" || len(site.Secrets) != 0 {
		t.Errorf("created site = %+v; want defaults", site)
	}
	id := site.Id.String()

	locked := secret(siteBody("With secret"), "password", "hunter2-hunter2")
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

	update := set(siteBody("With secret"), "port", "2222", "host", "example.com",
		"proxy_kind", "socks5", "proxy_host", "8.8.4.4", "proxy_port", "1080", "proxy_username", "proxyuser",
		"jump_host", "1.1.1.1", "jump_username", "bastion")
	secret(update, "proxy_password", "proxypass", "jump_password", "bastionpass")
	updated := a.send(http.MethodPut, sitesPath(ws, secretID), update, cookie, csrf)
	if updated.Status != http.StatusOK {
		t.Fatalf("update = %d %s", updated.Status, updated.Body)
	}
	if got := updated.site(t); got.Config["port"] != "2222" || got.Config["proxy_username"] != "proxyuser" || got.Config["jump_port"] != "22" || len(got.Secrets) != 3 {
		t.Errorf("updated = %+v; want the password kept, and the proxy password and the jump password added", got)
	}
	keyed := set(siteBody("With secret"), "host", "example.com", "auth", "key")
	secret(keyed, "private_key", testutil.PrivateKey(t, ""))
	if got := a.send(http.MethodPut, sitesPath(ws, secretID), keyed, cookie, csrf).site(t); len(got.Secrets) != 1 || got.Secrets[0] != "private_key" {
		t.Errorf("after switching to a key, without a proxy or a jump host = %+v; want only the key", got.Secrets)
	}

	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), siteBody("production"), cookie, csrf), 409, "site_name_taken")
	expectProblem(t, a.send(http.MethodPut, sitesPath(ws, secretID), siteBody("PRODUCTION"), cookie, csrf), 409, "site_name_taken")
	invalid := map[string]func(map[string]any){
		"invalid_name":        func(b map[string]any) { b["name"] = " " },
		"destination_blocked": func(b map[string]any) { set(b, "host", "169.254.169.254") },
		"secret_too_large":    func(b map[string]any) { secret(b, "password", strings.Repeat("p", 2000)) },
		"invalid_speed_limit": func(b map[string]any) { b["speedLimitBps"] = -1 },
	}
	for code, mutate := range invalid {
		body := siteBody("Invalid " + code)
		mutate(body)
		expectProblem(t, a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf), 422, code)
	}
	fields := map[string]func(map[string]any){
		"connector": func(b map[string]any) { b["connector"] = "gopher" },
		"host":      func(b map[string]any) { set(b, "host", "ftp://x") },
		"port":      func(b map[string]any) { set(b, "port", "70000") },
		"auth":      func(b map[string]any) { set(b, "auth", "account") },
		"shade":     func(b map[string]any) { set(b, "shade", "blue") },
		"token":     func(b map[string]any) { secret(b, "token", "x") },
	}
	for field, mutate := range fields {
		body := siteBody("Invalid " + field)
		mutate(body)
		r := a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf)
		expectProblem(t, r, 422, "invalid_config")
		if !strings.Contains(string(r.Body), `"field":"`+field+`"`) {
			t.Errorf("invalid %s does not name the field: %s", field, r.Body)
		}
	}
	unresolvable := set(siteBody("Nowhere"), "host", "nowhere.invalid")
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
	t.Parallel()
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
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	plain := siteBody("Plain")
	plain["connector"] = "ftp"
	if r := a.send(http.MethodPost, sitesPath(ws), plain, cookie, csrf); r.Status != http.StatusCreated || r.site(t).Config["charset"] != "utf-8" {
		t.Errorf("default charset = %d %s", r.Status, r.Body)
	}
	legacy := set(siteBody("Legacy"), "charset", "Windows-1252")
	legacy["connector"] = "ftp"
	made := a.send(http.MethodPost, sitesPath(ws), legacy, cookie, csrf)
	if made.Status != http.StatusCreated || made.site(t).Config["charset"] != "windows-1252" {
		t.Fatalf("legacy site = %d %s", made.Status, made.Body)
	}
	set(legacy, "charset", "koi8-r")
	if r := a.send(http.MethodPut, sitesPath(ws, made.site(t).Id.String()), legacy, cookie, csrf); r.Status != http.StatusOK || r.site(t).Config["charset"] != "koi8-r" {
		t.Errorf("update = %d %s", r.Status, r.Body)
	}
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), set(siteBody("Secure"), "charset", "iso-8859-1"), cookie, csrf), 422, "invalid_config")
}

// TestSites_RemoteDir verifies the folder a site opens in (4.14i).
//
// Scenarios:
//   - a site without one opens at the root and does not say so; one given is returned cleaned and kept when the site is changed
//   - setting the root removes it, and a relative folder is 422 invalid_remote_dir
func TestSites_RemoteDir(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	if plain := a.send(http.MethodPost, sitesPath(ws), siteBody("Plain"), cookie, csrf).site(t); plain.RemoteDir != nil {
		t.Errorf("a site without a folder says %q", *plain.RemoteDir)
	}
	body := siteBody("Web")
	body["remoteDir"] = "/var/www/html/"
	made := a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf)
	if got := made.site(t).RemoteDir; made.Status != http.StatusCreated || got == nil || *got != "/var/www/html" {
		t.Fatalf("created = %d %s", made.Status, made.Body)
	}
	body["remoteDir"] = "/"
	if r := a.send(http.MethodPut, sitesPath(ws, made.site(t).Id.String()), body, cookie, csrf); r.Status != http.StatusOK || r.site(t).RemoteDir != nil {
		t.Errorf("setting the root = %d %s", r.Status, r.Body)
	}
	body["remoteDir"] = "var/www"
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf), 422, "invalid_remote_dir")
}

// TestSites_Color verifies the label color of a site (ADR-0021).
//
// Scenarios:
//   - a site without one does not say so; one given is returned, changed, and removed by sending it empty
//   - an unknown color is 422 invalid_color
func TestSites_Color(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	if plain := a.send(http.MethodPost, sitesPath(ws), siteBody("Plain"), cookie, csrf).site(t); plain.Color != nil {
		t.Errorf("a site without a color says %q", *plain.Color)
	}
	body := siteBody("Prod")
	body["color"] = "red"
	made := a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf)
	if got := made.site(t).Color; made.Status != http.StatusCreated || got == nil || *got != httpapi.Red {
		t.Fatalf("created = %d %s", made.Status, made.Body)
	}
	body["color"] = "teal"
	if r := a.send(http.MethodPut, sitesPath(ws, made.site(t).Id.String()), body, cookie, csrf); r.Status != http.StatusOK || *r.site(t).Color != httpapi.Teal {
		t.Errorf("changing the color = %d %s", r.Status, r.Body)
	}
	body["color"] = ""
	if r := a.send(http.MethodPut, sitesPath(ws, made.site(t).Id.String()), body, cookie, csrf); r.Status != http.StatusOK || r.site(t).Color != nil {
		t.Errorf("removing the color = %d %s", r.Status, r.Body)
	}
	body["color"] = "purple"
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf), 422, "invalid_color")
}

// TestSites_Account verifies the account logon type over HTTP (4.8e).
//
// Scenarios:
//   - an FTP site of the account logon type keeps its account as a secret, which is never returned
//   - the account logon type is refused for SFTP
func TestSites_Account(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	if r := a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, cookie, csrf); r.Status != http.StatusCreated {
		t.Fatalf("vault setup = %d %s", r.Status, r.Body)
	}
	body := secret(set(siteBody("Billed"), "auth", "account"), "password", "pw", "account", "billing")
	body["connector"] = "ftp"

	made := a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf)
	got := made.site(t)
	if made.Status != http.StatusCreated || got.Config["auth"] != "account" || !slices.Contains(got.Secrets, "account") || strings.Contains(string(made.Body), "billing") {
		t.Fatalf("created = %d %s", made.Status, made.Body)
	}
	body["connector"] = "sftp"
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf), 422, "invalid_config")
}
