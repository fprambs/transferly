package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// probeCall asks the server to probe the time zone of a site in a folder.
func (e *fileEnv) probeCall(site uuid.UUID, dir string) reply {
	e.a.t.Helper()
	return e.a.send(http.MethodPost, filesPath(e.ws, site, "/timezone-probe", q("path", dir)), nil, e.cookie, e.csrf)
}

// TestRemoteFiles_ProbeTimeZone verifies probing the listing time zone of a connection.
//
// Scenarios:
//   - a server that lists exact times answers exact, for a saved site and for a quick connection, and leaves no probe file behind
//   - a folder that does not exist is 404, an unknown site 404, a relative-looking invalid path 422 and anonymous callers 401
func TestRemoteFiles_ProbeTimeZone(t *testing.T) {
	e := newFileEnv(t)
	a := e.a
	if r := a.send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), map[string]string{"path": "/dir"}, e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("mkdir = %d %s", r.Status, r.Body)
	}

	r := e.probeCall(e.site, "/dir")
	var probe struct {
		Exact         bool    `json:"exact"`
		OffsetMinutes int     `json:"offsetMinutes"`
		SuggestedZone *string `json:"suggestedZone"`
	}
	mustDecode(t, r.Body, &probe)
	if r.Status != http.StatusOK || !probe.Exact || probe.SuggestedZone != nil {
		t.Errorf("probe = %d %s", r.Status, r.Body)
	}
	var listing []listedEntry
	mustDecode(t, e.get("", q("path", "/dir"), nil).Body, &listing)
	if len(listing) != 0 {
		t.Errorf("the probe file was left behind: %+v", listing)
	}

	quick := a.do(request{method: http.MethodPost, path: "/api/v1/workspaces/" + e.ws.String() + "/quick-connections", cookie: e.cookie,
		headers: map[string]string{"X-CSRF-Token": e.csrf}, body: quickBody("8.8.8.8")})
	var made struct {
		ID uuid.UUID `json:"id"`
	}
	mustDecode(t, quick.Body, &made)
	if r := e.probeCall(made.ID, "/"); r.Status != http.StatusOK {
		t.Errorf("probe of a quick connection = %d %s", r.Status, r.Body)
	}

	expectProblem(t, e.probeCall(e.site, "/missing"), 404, "path_not_found")
	expectProblem(t, e.probeCall(uuid.New(), "/dir"), 404, "site_not_found")
	expectProblem(t, e.probeCall(e.site, "/bad\nname"), 422, "invalid_path")
	expectProblem(t, a.do(request{method: http.MethodPost, path: filesPath(e.ws, e.site, "/timezone-probe", q("path", "/dir"))}), 401, "unauthenticated")
}

// listedEntry is the part of a listing entry the test reads.
type listedEntry struct {
	Name string `json:"name"`
}
