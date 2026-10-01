package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// TestPhaseThree_CrossWorkspaceAndEscalation is the regression net of the security review of phase 3: what the members of one team can do
// with the resources of another, and what a role can do to reach a higher one.
//
// Scenarios:
//   - the admin of one team gets 404 on the members, the invitations, the audit log and the invitation form of another team, with no hint that it exists
//   - the identifiers of another team's invitation and members, sent through the admin's own team, are 404 and change nothing
//   - an admin cannot invite as an admin, hand out the owner role, remove the owner or an admin, or transfer the workspace
//   - a member who is demoted loses what the old role allowed on the next request, and one who is removed loses the workspace altogether
//   - the audit log of a team is refused to editors and viewers and hidden from strangers
func TestPhaseThree_CrossWorkspaceAndEscalation(t *testing.T) {
	e := newTeamEnv(t)
	created := e.a.send(http.MethodPost, "/api/v1/workspaces", map[string]string{"name": "Second team"}, e.cookie["owner"], e.csrf["owner"])
	if created.Status != http.StatusCreated {
		t.Fatalf("create second team = %d %s", created.Status, created.Body)
	}
	var other httpapi.Workspace
	mustDecode(t, created.Body, &other)
	otherPath := "/api/v1/workspaces/" + other.Id.String()
	stranger := e.user["stranger"].String()
	e.a.addMember(other.Id, e.user["stranger"], workspace.RoleViewer)
	invited := e.a.send(http.MethodPost, otherPath+"/invitations", map[string]string{"email": "guest@example.com", "role": "viewer"}, e.cookie["owner"], e.csrf["owner"])
	var invitation httpapi.InvitationCreated
	mustDecode(t, invited.Body, &invitation)

	send := func(as, method, path string, body any) reply {
		return e.a.send(method, path, body, e.cookie[as], e.csrf[as])
	}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, otherPath + "/members", nil},
		{http.MethodGet, otherPath + "/invitations", nil},
		{http.MethodGet, otherPath + "/audit", nil},
		{http.MethodPost, otherPath + "/invitations", map[string]string{"email": "x@example.com", "role": "viewer"}},
		{http.MethodPatch, otherPath + "/members/" + stranger, map[string]string{"role": "editor"}},
		{http.MethodDelete, otherPath + "/members/" + stranger, nil},
	} {
		expectProblem(t, send("admin", c.method, c.path, c.body), 404, "workspace_not_found")
	}

	expectProblem(t, e.call("admin", http.MethodDelete, "/invitations/"+invitation.Invitation.Id.String(), nil), 404, "invitation_not_found")
	expectProblem(t, e.call("admin", http.MethodPatch, "/members/"+stranger, map[string]string{"role": "editor"}), 404, "member_not_found")
	expectProblem(t, e.call("admin", http.MethodDelete, "/members/"+stranger, nil), 404, "member_not_found")
	var pending []httpapi.Invitation
	mustDecode(t, send("owner", http.MethodGet, otherPath+"/invitations", nil).Body, &pending)
	if len(pending) != 1 {
		t.Errorf("the invitations of the second team = %+v; want the one that was made, untouched", pending)
	}

	expectProblem(t, e.call("admin", http.MethodPost, "/invitations", map[string]string{"email": "boss@example.com", "role": "admin"}), 422, "invalid_role")
	expectProblem(t, e.call("admin", http.MethodPost, "/invitations", map[string]string{"email": "boss@example.com", "role": "owner"}), 422, "invalid_role")
	expectProblem(t, e.call("admin", http.MethodPatch, "/members/"+e.user["viewer"].String(), map[string]string{"role": "owner"}), 422, "invalid_role")
	expectProblem(t, e.call("admin", http.MethodDelete, "/members/"+e.user["owner"].String(), nil), 409, "owner_bound")
	expectProblem(t, e.call("admin", http.MethodPatch, "/members/"+e.user["owner"].String(), map[string]string{"role": "viewer"}), 409, "owner_bound")
	expectProblem(t, e.call("admin", http.MethodPost, "/transfer", map[string]string{"userId": e.user["admin"].String()}), 403, "forbidden")
	expectProblem(t, e.call("editor", http.MethodPatch, "/members/"+e.user["editor"].String(), map[string]string{"role": "admin"}), 403, "forbidden")

	if r := e.call("owner", http.MethodPatch, "/members/"+e.user["admin"].String(), map[string]string{"role": "viewer"}); r.Status != http.StatusNoContent {
		t.Fatalf("demote = %d %s", r.Status, r.Body)
	}
	expectProblem(t, e.call("admin", http.MethodPost, "/invitations", map[string]string{"email": "late@example.com", "role": "viewer"}), 403, "forbidden")
	expectProblem(t, e.call("admin", http.MethodGet, "/audit", nil), 403, "forbidden")
	if r := e.call("owner", http.MethodDelete, "/members/"+e.user["admin"].String(), nil); r.Status != http.StatusNoContent {
		t.Fatalf("remove = %d %s", r.Status, r.Body)
	}
	expectProblem(t, e.call("admin", http.MethodGet, "/members", nil), 404, "workspace_not_found")

	for _, as := range []string{"editor", "viewer"} {
		expectProblem(t, e.call(as, http.MethodGet, "/audit", nil), 403, "forbidden")
	}
	expectProblem(t, e.call("stranger", http.MethodGet, "/audit", nil), 404, "workspace_not_found")
}
