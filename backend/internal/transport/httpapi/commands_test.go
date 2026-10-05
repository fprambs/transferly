package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// newShellEnv returns a file environment whose site is on a server that runs commands.
func newShellEnv(t *testing.T) *fileEnv {
	t.Helper()
	e := newFileEnv(t)
	created := e.a.send(http.MethodPost, sitesPath(e.ws), siteBody(shellSite), e.cookie, e.csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("create site = %d %s", created.Status, created.Body)
	}
	e.site = created.site(t).Id
	return e
}

// tools reads what the server of the site of a file environment can do.
func (e *fileEnv) tools() reply {
	e.a.t.Helper()
	return e.a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + e.ws.String() + "/sites/" + e.site.String() + "/tools", cookie: e.cookie})
}

// TestServerCommands verifies the operations a server runs on its own (ADR-0027).
//
// Scenarios:
//   - a server that runs commands tells its tools, measures folders, packs entries into an archive and unpacks one into a new
//     folder, and computes checksums; compressing and extracting leave records in the activity
//   - a refusal of the server reads as its problem, and a file that is not an archive is 422 invalid_path
//   - a connection to a storage without a home folder starts at the root
//   - a server that cannot run commands tells no tools and answers 422 server_commands_unsupported
//   - once an administrator turns commands off, the server tells no tools; the limits say so, and turning them on again restores them
func TestServerCommands(t *testing.T) {
	e := newShellEnv(t)
	a := e.a
	if r := a.upload(e.ws, e.site, q("path", "/a.txt"), "alpha", e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("upload = %d %s", r.Status, r.Body)
	}
	var tools httpapi.ServerTools
	r := e.tools()
	mustDecode(t, r.Body, &tools)
	if r.Status != http.StatusOK || !tools.Copy || !tools.Usage || len(tools.Checksums) != 1 || tools.Archive[0] != "zip" || tools.Extract[0] != "zip" {
		t.Fatalf("tools = %d %+v", r.Status, tools)
	}
	var usage httpapi.DiskUsage
	r = e.get("/usage", q("path", "/"), nil)
	mustDecode(t, r.Body, &usage)
	if r.Status != http.StatusOK || usage.Bytes != 8192 {
		t.Errorf("usage = %d %+v", r.Status, usage)
	}
	r = a.send(http.MethodPost, filesPath(e.ws, e.site, "/compress", nil), map[string]any{"dir": "/", "names": []string{"a.txt"}, "name": "bundle.zip", "format": "zip"}, e.cookie, e.csrf)
	if r.Status != http.StatusCreated || r.entry(t).Path != "/bundle.zip" {
		t.Fatalf("compress = %d %s", r.Status, r.Body)
	}
	r = a.send(http.MethodPost, filesPath(e.ws, e.site, "/extract", nil), map[string]string{"path": "/bundle.zip"}, e.cookie, e.csrf)
	if r.Status != http.StatusCreated || r.entry(t).Path != "/bundle" {
		t.Fatalf("extract = %d %s", r.Status, r.Body)
	}
	if r := a.upload(e.ws, e.site, q("path", "/refuse.zip"), "z", e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatal(r.Status)
	}
	expectProblem(t, a.send(http.MethodPost, filesPath(e.ws, e.site, "/extract", nil), map[string]string{"path": "/refuse.zip"}, e.cookie, e.csrf), 403, "remote_permission_denied")
	expectProblem(t, a.send(http.MethodPost, filesPath(e.ws, e.site, "/extract", nil), map[string]string{"path": "/a.txt"}, e.cookie, e.csrf), 422, "invalid_path")
	var sum struct {
		Checksum string `json:"checksum"`
	}
	mustDecode(t, e.get("/checksum", q("path", "/a.txt"), nil).Body, &sum)
	if sum.Checksum != strings.Repeat("0", 64) {
		t.Errorf("checksum = %s, want the server's", sum.Checksum)
	}
	var kinds []string
	for _, entry := range e.activityOf("") {
		kinds = append(kinds, string(entry.Kind))
	}
	if joined := strings.Join(kinds, ","); !strings.Contains(joined, "compress") || !strings.Contains(joined, "extract") {
		t.Errorf("activity = %s, want compress and extract", joined)
	}

	limits := e.readLimits().Current
	if limits.ServerCommands == nil || !*limits.ServerCommands {
		t.Errorf("limits = %+v, want commands allowed", limits)
	}
	off := false
	limits.ServerCommands = &off
	if r := e.setLimits(limits); r.Status != http.StatusOK {
		t.Fatalf("set limits = %d %s", r.Status, r.Body)
	}
	mustDecode(t, e.tools().Body, &tools)
	if tools.Copy || len(tools.Archive) != 0 {
		t.Errorf("tools with commands off = %+v, want none", tools)
	}
	if got := e.readLimits().Current.ServerCommands; got == nil || *got {
		t.Errorf("limits.serverCommands = %v, want false", got)
	}
	limits.ServerCommands = nil
	_ = e.setLimits(limits)
	mustDecode(t, e.tools().Body, &tools)
	if !tools.Copy {
		t.Errorf("tools with commands on again = %+v", tools)
	}

	var home httpapi.HomeFolder
	r = e.get("/home", nil, nil)
	mustDecode(t, r.Body, &home)
	if r.Status != http.StatusOK || home.Path != "/" {
		t.Errorf("home = %d %+v, want the root of a storage without a home folder", r.Status, home)
	}

	plain := newFileEnv(t)
	mustDecode(t, plain.tools().Body, &tools)
	if tools.Copy || tools.Checksums == nil {
		t.Errorf("tools of a plain server = %+v, want none, as empty lists", tools)
	}
	expectProblem(t, plain.get("/usage", q("path", "/"), nil), 422, "server_commands_unsupported")
	expectProblem(t, plain.a.send(http.MethodPost, filesPath(plain.ws, plain.site, "/compress", nil), map[string]any{"dir": "/", "names": []string{"a"}, "name": "b.zip", "format": "zip"}, plain.cookie, plain.csrf), 422, "server_commands_unsupported")
	expectProblem(t, plain.a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + plain.ws.String() + "/sites/" + plain.site.String() + "/tools"}), 401, "unauthenticated")
}
