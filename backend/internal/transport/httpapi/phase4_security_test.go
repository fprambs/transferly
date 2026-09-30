package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestPhaseFour_BodyLimits is the regression net of the body limits added by the security review of phase 4: no request body is read
// past its limit, whatever type it declares.
//
// Scenarios:
//   - a JSON body over 1 MiB is refused as invalid_request before it is decoded, and so is one that declares another type
//   - a Sentinel import, whose file travels inside the JSON body, takes more than 1 MiB and reaches the export check
//   - uploads stream files larger than the limit, which another test covers with a 4 MiB file
func TestPhaseFour_BodyLimits(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	huge := strings.Repeat("x", 2<<20)

	expectProblem(t, a.send(http.MethodPatch, "/api/v1/me", map[string]string{"timezone": huge}, cookie, csrf), http.StatusBadRequest, "invalid_request")
	disguised := a.do(request{method: http.MethodPatch, path: "/api/v1/me", body: `{"timezone":"` + huge + `"}`, cookie: cookie,
		headers: map[string]string{"X-CSRF-Token": csrf, "Content-Type": "text/plain"}})
	if disguised.Status < 400 || disguised.Status >= 500 {
		t.Errorf("a large body declared as text = %d %s; want a refusal", disguised.Status, disguised.Body)
	}

	expectProblem(t, a.sentinelImport(ws, "", exportPassword, strings.Repeat("y", 3<<19), cookie, csrf), http.StatusUnprocessableEntity, "invalid_export")
}

// TestPhaseFour_ExportNeedsAdmin is the regression net of the export rule of the security review of phase 4: an export shows every
// secret of a team in clear text inside its file, which editors never read otherwise, so it stays with the admins and the owner.
//
// Scenarios:
//   - editors and viewers are refused with forbidden, and a stranger does not learn that the team exists
//   - the owner exports the sites of the team
func TestPhaseFour_ExportNeedsAdmin(t *testing.T) {
	e := newTeamEnv(t)
	for _, role := range []string{"editor", "viewer"} {
		expectProblem(t, e.a.exportCall(e.ws, exportPassword, e.cookie[role], e.csrf[role]), http.StatusForbidden, "forbidden")
	}
	expectProblem(t, e.a.exportCall(e.ws, exportPassword, e.cookie["stranger"], e.csrf["stranger"]), http.StatusNotFound, "workspace_not_found")
	if r := e.a.exportCall(e.ws, exportPassword, e.cookie["owner"], e.csrf["owner"]); r.Status != http.StatusOK {
		t.Errorf("owner export = %d %s", r.Status, r.Body)
	}
}
