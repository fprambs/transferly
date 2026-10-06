package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// invite creates an invitation as a role of the team and returns its token.
func (e *teamEnv) invite(as, email, role string) (httpapi.InvitationCreated, reply) {
	e.a.t.Helper()
	r := e.call(as, http.MethodPost, "/invitations", map[string]string{"email": email, "role": role})
	var created httpapi.InvitationCreated
	if r.Status == http.StatusCreated {
		mustDecode(e.a.t, r.Body, &created)
	}
	return created, r
}

// anonymous sends a request without a session.
func (e *teamEnv) anonymous(path string, body any) reply {
	e.a.t.Helper()
	return e.a.do(request{method: http.MethodPost, path: path, body: body})
}

// TestInvitations_Lifecycle verifies inviting, previewing, accepting and joining.
//
// Scenarios:
//   - an admin invites an address: the token comes once, the invitation is listed without it, and the preview names the team, role and address
//   - an account with the invited address accepts and becomes a member with that role, and the token works only once
//   - an account with another address cannot accept, and the invitation still works for the right one
//   - an address without an account joins with a password, is signed in and becomes a member
//   - an address that already has an account cannot join and is told so, and short passwords are refused without spending the token
//   - revoked invitations, unknown tokens and inviting an existing member fail with stable codes
func TestInvitations_Lifecycle(t *testing.T) {
	t.Parallel()
	e := newTeamEnv(t)
	created, r := e.invite("admin", "  Stranger@Example.com ", "editor")
	if r.Status != http.StatusCreated || len(created.Token) < 43 || created.Invitation.Email != "stranger@example.com" || created.Invitation.Role != "editor" {
		t.Fatalf("create = %d %s", r.Status, r.Body)
	}
	list := e.call("admin", http.MethodGet, "/invitations", nil)
	if list.Status != http.StatusOK || strings.Contains(string(list.Body), created.Token) || !strings.Contains(string(list.Body), "stranger@example.com") {
		t.Fatalf("list = %d %s", list.Status, list.Body)
	}

	var preview httpapi.InvitationPreview
	p := e.anonymous("/api/v1/invitations/preview", map[string]string{"token": created.Token})
	mustDecode(t, p.Body, &preview)
	if p.Status != http.StatusOK || preview.WorkspaceName != "Acme Web" || preview.Role != "editor" || preview.Email != "stranger@example.com" || !preview.AccountExists || preview.InvitedBy == nil || *preview.InvitedBy != "admin@example.com" {
		t.Fatalf("preview = %d %s", p.Status, p.Body)
	}

	accept := func(as, token string) reply {
		return e.a.send(http.MethodPost, "/api/v1/invitations/accept", map[string]string{"token": token}, e.cookie[as], e.csrf[as])
	}
	expectProblem(t, accept("viewer", created.Token), 404, "invitation_not_found")
	a := accept("stranger", created.Token)
	var joined httpapi.Workspace
	mustDecode(t, a.Body, &joined)
	if a.Status != http.StatusOK || joined.Id != e.ws || joined.Role != "editor" || joined.Kind != "team" {
		t.Fatalf("accept = %d %s", a.Status, a.Body)
	}
	if e.members("owner")["stranger@example.com"] != "editor" {
		t.Error("the new member is not listed")
	}
	expectProblem(t, accept("stranger", created.Token), 404, "invitation_not_found")
	expectProblem(t, e.anonymous("/api/v1/invitations/preview", map[string]string{"token": created.Token}), 404, "invitation_not_found")

	fresh, _ := e.invite("owner", "newcomer@example.com", "viewer")
	expectProblem(t, e.anonymous("/api/v1/invitations/join", map[string]string{"token": fresh.Token, "password": "short"}), 422, "weak_password")
	j := e.anonymous("/api/v1/invitations/join", map[string]string{"token": fresh.Token, "password": adminPassword})
	if j.Status != http.StatusCreated || j.sessionCookie() == nil {
		t.Fatalf("join = %d %s", j.Status, j.Body)
	}
	if e.members("owner")["newcomer@example.com"] != "viewer" {
		t.Error("the joined account is not a member")
	}
	if me := e.a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: j.sessionCookie()}); me.Status != http.StatusOK {
		t.Errorf("session after join = %d", me.Status)
	}
	expectProblem(t, e.anonymous("/api/v1/invitations/join", map[string]string{"token": fresh.Token, "password": adminPassword}), 404, "invitation_not_found")

	existing, _ := e.invite("owner", "solo@example.com", "viewer")
	if _, err := e.a.auth.Register(t.Context(), "solo@example.com", adminPassword); err != nil {
		t.Fatal(err)
	}
	expectProblem(t, e.anonymous("/api/v1/invitations/join", map[string]string{"token": existing.Token, "password": adminPassword}), 409, "account_exists")

	revoked, _ := e.invite("owner", "gone@example.com", "viewer")
	var listed []httpapi.Invitation
	mustDecode(t, e.call("owner", http.MethodGet, "/invitations", nil).Body, &listed)
	for _, i := range listed {
		if i.Email == "gone@example.com" {
			if r := e.call("owner", http.MethodDelete, "/invitations/"+i.Id.String(), nil); r.Status != http.StatusNoContent {
				t.Fatalf("revoke = %d %s", r.Status, r.Body)
			}
		}
	}
	expectProblem(t, e.anonymous("/api/v1/invitations/preview", map[string]string{"token": revoked.Token}), 404, "invitation_not_found")
	expectProblem(t, e.anonymous("/api/v1/invitations/preview", map[string]string{"token": "made-up"}), 404, "invitation_not_found")
	expectProblem(t, e.call("owner", http.MethodDelete, "/invitations/"+uuid.NewString(), nil), 404, "invitation_not_found")

	_, dup := e.invite("owner", "ADMIN@example.com", "viewer")
	expectProblem(t, dup, 409, "already_member")
	_, bad := e.invite("owner", "not-an-email", "viewer")
	expectProblem(t, bad, 422, "invalid_email")
	_, owner := e.invite("owner", "x@example.com", "owner")
	expectProblem(t, owner, 422, "invalid_role")
}

// TestInvitations_AuthorizationAndLimits verifies who may use the invitation endpoints and how they are protected.
//
// Scenarios:
//   - anonymous callers get 401 and writers without the anti-CSRF token get 403 csrf_invalid on the workspace endpoints
//   - a stranger gets 404 workspace_not_found, editors and viewers get 403 forbidden, and an admin cannot invite an admin
//   - accepting needs a session, and the public endpoints refuse a foreign origin
//   - too many attempts from one address are rate limited on preview, accept and join
//   - the workspace holds at most 50 pending invitations
func TestInvitations_AuthorizationAndLimits(t *testing.T) {
	t.Parallel()
	e := newTeamEnv(t)
	base := "/api/v1/workspaces/" + e.ws.String() + "/invitations"
	body := map[string]string{"email": "a@example.com", "role": "viewer"}
	expectProblem(t, e.a.do(request{method: http.MethodGet, path: base}), 401, "unauthenticated")
	expectProblem(t, e.a.do(request{method: http.MethodPost, path: base, body: body}), 401, "unauthenticated")
	expectProblem(t, e.a.do(request{method: http.MethodPost, path: base, body: body, cookie: e.cookie["admin"]}), 403, "csrf_invalid")
	expectProblem(t, e.call("stranger", http.MethodGet, "/invitations", nil), 404, "workspace_not_found")
	expectProblem(t, e.call("stranger", http.MethodPost, "/invitations", body), 404, "workspace_not_found")
	expectProblem(t, e.call("stranger", http.MethodDelete, "/invitations/"+uuid.NewString(), nil), 404, "workspace_not_found")
	for _, as := range []string{"editor", "viewer"} {
		expectProblem(t, e.call(as, http.MethodGet, "/invitations", nil), 403, "forbidden")
		expectProblem(t, e.call(as, http.MethodPost, "/invitations", body), 403, "forbidden")
		expectProblem(t, e.call(as, http.MethodDelete, "/invitations/"+uuid.NewString(), nil), 403, "forbidden")
	}
	_, adminInvite := e.invite("admin", "b@example.com", "admin")
	expectProblem(t, adminInvite, 422, "invalid_role")
	expectProblem(t, e.a.do(request{method: http.MethodPost, path: "/api/v1/invitations/accept", body: map[string]string{"token": "x"}}), 401, "unauthenticated")

	foreign := e.a.do(request{method: http.MethodPost, path: "/api/v1/invitations/preview", body: map[string]string{"token": "x"}, headers: map[string]string{"Origin": "https://evil.example"}})
	expectProblem(t, foreign, 403, "cross_site_request")

	limited := newTeamEnv(t)
	for range 20 {
		expectProblem(t, limited.anonymous("/api/v1/invitations/preview", map[string]string{"token": "x"}), 404, "invitation_not_found")
	}
	expectProblem(t, limited.anonymous("/api/v1/invitations/preview", map[string]string{"token": "x"}), 429, "rate_limited")
	expectProblem(t, limited.anonymous("/api/v1/invitations/join", map[string]string{"token": "x", "password": adminPassword}), 429, "rate_limited")
	expectProblem(t, limited.a.send(http.MethodPost, "/api/v1/invitations/accept", map[string]string{"token": "x"}, limited.cookie["viewer"], limited.csrf["viewer"]), 429, "rate_limited")

	many := newTeamEnv(t)
	for i := range 50 {
		if _, r := many.invite("owner", uuid.NewString()+"@example.com", "viewer"); r.Status != http.StatusCreated {
			t.Fatalf("invitation %d = %d %s", i, r.Status, r.Body)
		}
	}
	_, late := many.invite("owner", "late@example.com", "viewer")
	expectProblem(t, late, 409, "too_many_invitations")
}
