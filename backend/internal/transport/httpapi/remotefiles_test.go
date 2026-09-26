package httpapi_test

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

// filesPath returns the path of a file endpoint of a site, with query parameters.
func filesPath(ws, siteID uuid.UUID, endpoint string, query url.Values) string {
	p := "/api/v1/workspaces/" + ws.String() + "/sites/" + siteID.String() + "/files" + endpoint
	if len(query) > 0 {
		p += "?" + query.Encode()
	}
	return p
}

// q builds query parameters from pairs.
func q(pairs ...string) url.Values {
	v := url.Values{}
	for i := 0; i < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return v
}

// upload sends a file body with the given query.
func (a *api) upload(ws, siteID uuid.UUID, query url.Values, body string, cookie *http.Cookie, csrf string) reply {
	a.t.Helper()
	return a.do(request{method: http.MethodPut, path: filesPath(ws, siteID, "/content", query), body: body, cookie: cookie,
		headers: map[string]string{"X-CSRF-Token": csrf, "Content-Type": "application/octet-stream"}})
}

// entry decodes the reply as a remote entry.
func (r reply) entry(t *testing.T) httpapi.RemoteEntry {
	t.Helper()
	var e httpapi.RemoteEntry
	mustDecode(t, r.Body, &e)
	return e
}

// fileEnv is a signed-in owner with a site and its personal workspace.
type fileEnv struct {
	a      *api
	cookie *http.Cookie
	csrf   string
	ws     uuid.UUID
	site   uuid.UUID
}

// newFileEnv creates the owner, a site and returns helpers for it.
func newFileEnv(t *testing.T) *fileEnv {
	t.Helper()
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	created := a.send(http.MethodPost, sitesPath(ws), siteBody("Files"), cookie, csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("create site = %d %s", created.Status, created.Body)
	}
	return &fileEnv{a: a, cookie: cookie, csrf: csrf, ws: ws, site: created.site(t).Id}
}

// get performs a read call on a file endpoint.
func (e *fileEnv) get(endpoint string, query url.Values, headers map[string]string) reply {
	e.a.t.Helper()
	return e.a.do(request{method: http.MethodGet, path: filesPath(e.ws, e.site, endpoint, query), cookie: e.cookie, headers: headers})
}

// TestRemoteFiles_Lifecycle verifies browsing and changing remote files over HTTP.
//
// Scenarios:
//   - a new site lists as empty; directories and files can be created, described, listed, renamed and deleted
//   - entries carry type, size, mode, UTC time and precision
//   - duplicate creation is 409 path_exists, missing paths are 404 path_not_found, listing a file is 422 not_a_directory
//   - deleting a non-empty directory is 409 directory_not_empty unless recursive is set
//   - invalid paths, modes, offsets (also negative ones) and sizes (negative ones) are 422, malformed bodies are 400
func TestRemoteFiles_Lifecycle(t *testing.T) {
	e := newFileEnv(t)
	a := e.a

	var listing []httpapi.RemoteEntry
	mustDecode(t, e.get("", q("path", "/"), nil).Body, &listing)
	if len(listing) != 0 {
		t.Fatalf("a new site lists %+v", listing)
	}

	dir := a.send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), map[string]string{"path": "/docs"}, e.cookie, e.csrf)
	if dir.Status != http.StatusCreated || dir.entry(t).Type != "directory" {
		t.Fatalf("mkdir = %d %s", dir.Status, dir.Body)
	}
	expectProblem(t, a.send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), map[string]string{"path": "/docs"}, e.cookie, e.csrf), 409, "path_exists")
	expectProblem(t, a.send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), map[string]string{"path": "/no/parent"}, e.cookie, e.csrf), 404, "path_not_found")

	up := e.a.upload(e.ws, e.site, q("path", "/docs/a.txt"), "hello", e.cookie, e.csrf)
	if up.Status != http.StatusCreated {
		t.Fatalf("upload = %d %s", up.Status, up.Body)
	}
	if got := up.entry(t); got.Name != "a.txt" || got.Size != 5 || got.Type != "file" || got.Path != "/docs/a.txt" || got.ModifiedAt.Location().String() != "UTC" ||
		got.ModifiedPrecision != "second" || !strings.HasPrefix(got.Mode, "-") {
		t.Errorf("uploaded entry = %+v", got)
	}
	expectProblem(t, e.a.upload(e.ws, e.site, q("path", "/docs/a.txt"), "again", e.cookie, e.csrf), 409, "path_exists")
	if r := e.a.upload(e.ws, e.site, q("path", "/docs/a.txt", "mode", "overwrite"), "over", e.cookie, e.csrf); r.Status != http.StatusCreated || r.entry(t).Size != 4 {
		t.Errorf("overwrite = %d %s", r.Status, r.Body)
	}

	stat := e.get("/entry", q("path", "/docs/a.txt"), nil)
	if stat.Status != http.StatusOK || stat.entry(t).Size != 4 {
		t.Errorf("stat = %d %s", stat.Status, stat.Body)
	}
	expectProblem(t, e.get("/entry", q("path", "/nope"), nil), 404, "path_not_found")
	expectProblem(t, e.get("", q("path", "/docs/a.txt"), nil), 422, "not_a_directory")
	expectProblem(t, e.get("", q("path", "/nope"), nil), 404, "path_not_found")

	moved := a.send(http.MethodPost, filesPath(e.ws, e.site, "/rename", nil), map[string]string{"from": "/docs/a.txt", "to": "/b.txt"}, e.cookie, e.csrf)
	if moved.Status != http.StatusOK || moved.entry(t).Path != "/b.txt" {
		t.Fatalf("rename = %d %s", moved.Status, moved.Body)
	}
	e.a.upload(e.ws, e.site, q("path", "/c.txt"), "c", e.cookie, e.csrf)
	expectProblem(t, a.send(http.MethodPost, filesPath(e.ws, e.site, "/rename", nil), map[string]string{"from": "/b.txt", "to": "/c.txt"}, e.cookie, e.csrf), 409, "path_exists")
	expectProblem(t, a.send(http.MethodPost, filesPath(e.ws, e.site, "/rename", nil), map[string]string{"from": "/ghost", "to": "/x"}, e.cookie, e.csrf), 404, "path_not_found")

	e.a.upload(e.ws, e.site, q("path", "/docs/inner"), "i", e.cookie, e.csrf)
	del := func(p string, recursive string) reply {
		query := q("path", p)
		if recursive != "" {
			query.Set("recursive", recursive)
		}
		return a.send(http.MethodDelete, filesPath(e.ws, e.site, "", query), nil, e.cookie, e.csrf)
	}
	expectProblem(t, del("/docs", ""), 409, "directory_not_empty")
	expectProblem(t, del("/docs", "false"), 409, "directory_not_empty")
	if r := del("/docs", "true"); r.Status != http.StatusNoContent {
		t.Errorf("recursive delete = %d %s", r.Status, r.Body)
	}
	if r := del("/b.txt", ""); r.Status != http.StatusNoContent {
		t.Errorf("delete = %d %s", r.Status, r.Body)
	}
	expectProblem(t, del("/b.txt", ""), 404, "path_not_found")

	for _, bad := range []string{"", "/a\nb", strings.Repeat("a", 5000)} {
		expectProblem(t, e.get("/entry", q("path", bad), nil), 422, "invalid_path")
	}
	expectProblem(t, e.a.upload(e.ws, e.site, q("path", "/x", "mode", "sideways"), "x", e.cookie, e.csrf), 422, "invalid_mode")
	expectProblem(t, e.a.upload(e.ws, e.site, q("path", "/x", "offset", "5"), "x", e.cookie, e.csrf), 422, "invalid_path")
	expectProblem(t, e.a.upload(e.ws, e.site, q("path", "/x", "mode", "resume", "offset", "-1"), "x", e.cookie, e.csrf), 422, "invalid_offset")
	expectProblem(t, e.a.upload(e.ws, e.site, q("path", "/x", "size", "-5"), "x", e.cookie, e.csrf), 422, "invalid_size")
	expectProblem(t, e.a.upload(e.ws, e.site, q("path", "/c.txt", "mode", "resume", "offset", "99"), "x", e.cookie, e.csrf), 422, "invalid_path")
	expectProblem(t, e.get("", nil, nil), 400, "invalid_request")
	expectProblem(t, a.send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), "{not json", e.cookie, e.csrf), 400, "invalid_request")
}

// TestRemoteFiles_Download verifies streaming downloads.
//
// Scenarios:
//   - a whole file is an attachment of type octet-stream with its length, byte ranges are advertised and the content is exact
//   - a file named to attack the header or the page (quotes, non-ASCII, a script) stays an inert attachment
//   - ranges from an offset, bounded, and suffix answer 206 with Content-Range; a range past the end is 416 with the size
//   - a malformed or multiple range is ignored and the whole file is sent
//   - directories cannot be downloaded and missing files are 404
//   - a large file survives a round trip
func TestRemoteFiles_Download(t *testing.T) {
	e := newFileEnv(t)
	e.a.upload(e.ws, e.site, q("path", "/f.bin"), "0123456789", e.cookie, e.csrf)

	whole := e.get("/content", q("path", "/f.bin"), nil)
	if whole.Status != http.StatusOK || string(whole.Body) != "0123456789" {
		t.Fatalf("download = %d %q", whole.Status, whole.Body)
	}
	if whole.Header.Get("Content-Type") != "application/octet-stream" || whole.Header.Get("Content-Length") != "10" || whole.Header.Get("Accept-Ranges") != "bytes" ||
		whole.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(whole.Header.Get("Content-Disposition"), "attachment") ||
		whole.Header.Get("Last-Modified") == "" {
		t.Errorf("headers = %v", whole.Header)
	}

	for _, name := range []string{`a "quoted" name.txt`, "résumé.txt", "page.html", "x;y=z.txt"} {
		e.a.upload(e.ws, e.site, q("path", "/"+name), "<script>alert(1)</script>", e.cookie, e.csrf)
		r := e.get("/content", q("path", "/"+name), nil)
		disposition, params, err := mime.ParseMediaType(r.Header.Get("Content-Disposition"))
		if err != nil || disposition != "attachment" || params["filename"] != name {
			t.Errorf("Content-Disposition for %q = %q (%v)", name, r.Header.Get("Content-Disposition"), err)
		}
		if r.Header.Get("Content-Type") != "application/octet-stream" || strings.ContainsAny(r.Header.Get("Content-Disposition"), "\r\n") {
			t.Errorf("download of %q is not inert: %v", name, r.Header)
		}
	}

	ranges := []struct {
		header, want, contentRange string
	}{
		{"bytes=2-5", "2345", "bytes 2-5/10"},
		{"bytes=7-", "789", "bytes 7-9/10"},
		{"bytes=-3", "789", "bytes 7-9/10"},
		{"bytes=8-99", "89", "bytes 8-9/10"},
	}
	for _, tt := range ranges {
		r := e.get("/content", q("path", "/f.bin"), map[string]string{"Range": tt.header})
		if r.Status != http.StatusPartialContent || string(r.Body) != tt.want || r.Header.Get("Content-Range") != tt.contentRange {
			t.Errorf("%s = %d %q %q; want 206 %q %q", tt.header, r.Status, r.Body, r.Header.Get("Content-Range"), tt.want, tt.contentRange)
		}
	}
	for _, header := range []string{"bytes=10-", "bytes=-0", "bytes=50-60"} {
		r := e.get("/content", q("path", "/f.bin"), map[string]string{"Range": header})
		expectProblem(t, r, 416, "range_not_satisfiable")
		if r.Header.Get("Content-Range") != "bytes */10" {
			t.Errorf("%s: Content-Range = %q; want bytes */10", header, r.Header.Get("Content-Range"))
		}
	}
	for _, header := range []string{"bytes=1-2,4-5", "items=1-2", "bytes=abc", "bytes=5-2", "bytes=-x", "bytes=1", "bytes=x-", "bytes=99999999999999999999-", "bytes=-99999999999999999999", "bytes=1-x"} {
		if r := e.get("/content", q("path", "/f.bin"), map[string]string{"Range": header}); r.Status != http.StatusOK || string(r.Body) != "0123456789" {
			t.Errorf("Range %q = %d %q; want it ignored", header, r.Status, r.Body)
		}
	}

	e.a.send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), map[string]string{"path": "/d"}, e.cookie, e.csrf)
	expectProblem(t, e.get("/content", q("path", "/d"), nil), 422, "invalid_path")
	expectProblem(t, e.get("/content", q("path", "/missing"), nil), 404, "path_not_found")

	e.a.upload(e.ws, e.site, q("path", "/empty"), "", e.cookie, e.csrf)
	if r := e.get("/content", q("path", "/empty"), nil); r.Status != http.StatusOK || len(r.Body) != 0 {
		t.Errorf("empty file = %d %q", r.Status, r.Body)
	}

	big := strings.Repeat("0123456789abcdef", 1<<18)
	if r := e.a.upload(e.ws, e.site, q("path", "/big"), big, e.cookie, e.csrf); r.Status != http.StatusCreated || r.entry(t).Size != int64(len(big)) {
		t.Fatalf("upload of %d bytes = %d", len(big), r.Status)
	}
	if r := e.get("/content", q("path", "/big"), nil); string(r.Body) != big {
		t.Errorf("a %d byte file did not survive the round trip", len(big))
	}
}

// TestRemoteFiles_ResumableUpload verifies completing an interrupted upload.
//
// Scenarios:
//   - after a partial upload the size is read with stat, and mode=resume with that offset completes the file
//   - resuming at an offset inside the file rewrites from there
func TestRemoteFiles_ResumableUpload(t *testing.T) {
	e := newFileEnv(t)
	e.a.upload(e.ws, e.site, q("path", "/movie"), "first-half|", e.cookie, e.csrf)

	size := e.get("/entry", q("path", "/movie"), nil).entry(t).Size
	done := e.a.upload(e.ws, e.site, q("path", "/movie", "mode", "resume", "offset", "11"), "second-half", e.cookie, e.csrf)
	if size != 11 || done.Status != http.StatusCreated || done.entry(t).Size != 22 {
		t.Fatalf("resume from %d = %d %s", size, done.Status, done.Body)
	}
	if got := e.get("/content", q("path", "/movie"), nil); string(got.Body) != "first-half|second-half" {
		t.Errorf("content = %q", got.Body)
	}
	e.a.upload(e.ws, e.site, q("path", "/movie", "mode", "resume", "offset", "6"), "HALF|SECOND", e.cookie, e.csrf)
	if got := e.get("/content", q("path", "/movie"), nil); !strings.HasPrefix(string(got.Body), "first-HALF|SECOND") {
		t.Errorf("content after a rewrite = %q", got.Body)
	}
}

// TestRemoteFiles_AuthorizationMatrix verifies who may reach each file endpoint.
//
// Scenarios:
//   - anonymous callers get 401 and writers without the anti-CSRF token get 403 csrf_invalid
//   - a stranger gets 404 workspace_not_found everywhere
//   - a viewer may list, stat and download but gets 403 forbidden on upload, mkdir, rename and delete
//   - an editor may change files
//   - a site of another workspace, or a missing one, is 404 site_not_found
func TestRemoteFiles_AuthorizationMatrix(t *testing.T) {
	e := newFileEnv(t)
	a := e.a
	strangerCookie, _ := a.signIn("stranger@example.com")
	viewerCookie, viewer := a.signIn("viewer@example.com")
	editorCookie, editor := a.signIn("editor@example.com")
	a.addMember(e.ws, viewer.ID, workspace.RoleViewer)
	a.addMember(e.ws, editor.ID, workspace.RoleEditor)
	e.a.upload(e.ws, e.site, q("path", "/f"), "data", e.cookie, e.csrf)
	csrfOf := func(c *http.Cookie) string {
		return a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: c}).session(t).CsrfToken
	}
	strangerCSRF, viewerCSRF, editorCSRF := csrfOf(strangerCookie), csrfOf(viewerCookie), csrfOf(editorCookie)

	type call struct {
		name, method, path, body string
		json                     any
	}
	calls := []call{
		{name: "list", method: http.MethodGet, path: filesPath(e.ws, e.site, "", q("path", "/"))},
		{name: "stat", method: http.MethodGet, path: filesPath(e.ws, e.site, "/entry", q("path", "/f"))},
		{name: "download", method: http.MethodGet, path: filesPath(e.ws, e.site, "/content", q("path", "/f"))},
		{name: "upload", method: http.MethodPut, path: filesPath(e.ws, e.site, "/content", q("path", "/new")), body: "x"},
		{name: "mkdir", method: http.MethodPost, path: filesPath(e.ws, e.site, "/directories", nil), json: map[string]string{"path": "/dir"}},
		{name: "rename", method: http.MethodPost, path: filesPath(e.ws, e.site, "/rename", nil), json: map[string]string{"from": "/f", "to": "/g"}},
		{name: "delete", method: http.MethodDelete, path: filesPath(e.ws, e.site, "", q("path", "/f"))},
	}
	send := func(c call, cookie *http.Cookie, csrf string) reply {
		body := any(c.json)
		if c.body != "" {
			body = c.body
		}
		headers := map[string]string{"X-CSRF-Token": csrf}
		if c.body != "" {
			headers["Content-Type"] = "application/octet-stream"
		}
		return a.do(request{method: c.method, path: c.path, body: body, cookie: cookie, headers: headers})
	}
	for _, c := range calls {
		if c.name == "upload" || c.name == "mkdir" || c.name == "rename" {
			expectProblem(t, send(c, nil, ""), 401, "unauthenticated")
		} else {
			expectProblem(t, a.do(request{method: c.method, path: c.path}), 401, "unauthenticated")
		}
		if c.method != http.MethodGet {
			expectProblem(t, send(c, editorCookie, ""), 403, "csrf_invalid")
		}
		expectProblem(t, send(c, strangerCookie, strangerCSRF), 404, "workspace_not_found")
	}
	for _, c := range calls[:3] {
		if r := send(c, viewerCookie, viewerCSRF); r.Status != http.StatusOK {
			t.Errorf("viewer %s = %d %s; want 200", c.name, r.Status, r.Body)
		}
	}
	for _, c := range calls[3:] {
		expectProblem(t, send(c, viewerCookie, viewerCSRF), 403, "forbidden")
	}
	if r := send(calls[3], editorCookie, editorCSRF); r.Status != http.StatusCreated {
		t.Errorf("editor upload = %d %s", r.Status, r.Body)
	}
	if r := send(calls[5], editorCookie, editorCSRF); r.Status != http.StatusOK {
		t.Errorf("editor rename = %d %s", r.Status, r.Body)
	}

	ghost := uuid.New()
	expectProblem(t, a.do(request{method: http.MethodGet, path: filesPath(e.ws, ghost, "", q("path", "/")), cookie: e.cookie}), 404, "site_not_found")
	otherWS := a.personal(strangerCookie)
	expectProblem(t, a.do(request{method: http.MethodGet, path: filesPath(otherWS, e.site, "", q("path", "/")), cookie: strangerCookie}), 404, "site_not_found")
}

// TestRemoteFiles_SharedByOneUser verifies that a user's files stay theirs.
//
// Scenarios:
//   - a second workspace member sees the files of the site through the same site, without a shared connection
//     between users
func TestRemoteFiles_SharedByOneUser(t *testing.T) {
	e := newFileEnv(t)
	viewerCookie, viewer := e.a.signIn("viewer@example.com")
	e.a.addMember(e.ws, viewer.ID, workspace.RoleViewer)
	e.a.upload(e.ws, e.site, q("path", "/shared.txt"), "hello", e.cookie, e.csrf)

	var listing []httpapi.RemoteEntry
	r := e.a.do(request{method: http.MethodGet, path: filesPath(e.ws, e.site, "", q("path", "/")), cookie: viewerCookie})
	if err := json.Unmarshal(r.Body, &listing); err != nil || len(listing) != 1 || listing[0].Name != "shared.txt" {
		t.Errorf("viewer listing = %s", r.Body)
	}
}
