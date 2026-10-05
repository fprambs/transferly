package httpapi_test

import (
	"net/http"
	"testing"
)

// TestSudoNeedsAdmin verifies who may turn on the sudo mode of a site (ADR-0027).
//
// Scenarios:
//   - an editor creating an SFTP site with sudo on is refused with elevation_forbidden
//   - an admin creates it
func TestSudoNeedsAdmin(t *testing.T) {
	e := newTeamEnv(t)
	body := map[string]any{"name": "Root", "connector": "sftp", "config": map[string]any{"host": "8.8.8.8", "username": "deploy", "sudo": "true"}}
	expectProblem(t, e.call("editor", http.MethodPost, "/sites", body), http.StatusForbidden, "elevation_forbidden")
	if r := e.call("admin", http.MethodPost, "/sites", body); r.Status != http.StatusCreated {
		t.Errorf("admin create = %d %s", r.Status, r.Body)
	}
}
