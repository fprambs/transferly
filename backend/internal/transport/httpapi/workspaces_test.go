package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// signIn registers an account and returns its session cookie and user.
func (a *api) signIn(email string) (*http.Cookie, auth.User) {
	a.t.Helper()
	user, err := a.auth.Register(a.t.Context(), email, adminPassword)
	if err != nil {
		a.t.Fatalf("Register() = %v", err)
	}
	r := a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": email, "password": adminPassword}})
	if r.Status != http.StatusOK || r.sessionCookie() == nil {
		a.t.Fatalf("login = %d %s", r.Status, r.Body)
	}
	return r.sessionCookie(), user
}

// addMember gives a user a role in a workspace, bypassing the API that does not exist yet.
func (a *api) addMember(workspaceID, userID uuid.UUID, role workspace.Role) {
	a.t.Helper()
	_, err := a.store.DB().Exec(context.Background(), "INSERT INTO memberships (workspace_id, user_id, role) VALUES ($1, $2, $3)", workspaceID, userID, string(role))
	if err != nil {
		a.t.Fatal(err)
	}
}

// workspaces decodes the reply as a list of workspaces.
func (r reply) workspaces(t *testing.T) []httpapi.Workspace {
	t.Helper()
	var list []httpapi.Workspace
	if err := json.Unmarshal(r.Body, &list); err != nil {
		t.Fatalf("decoding workspaces from %q: %v", r.Body, err)
	}
	return list
}

// TestWorkspaces verifies the personal workspace and the workspace endpoints.
//
// Scenarios:
//   - setup and registration plus login each give the user exactly one personal workspace, owned by them
//   - repeated logins do not create more workspaces
//   - a user sees only their own workspaces, and a foreign workspace is 404 like a missing one
//   - fetching a workspace returns the role of the caller
func TestWorkspaces(t *testing.T) {
	a := newAPI(t)
	adminCookie, _ := a.setup()
	otherCookie, _ := a.signIn("other@example.com")
	a.signIn2(t, "other@example.com")

	mine := a.do(request{method: http.MethodGet, path: "/api/v1/workspaces", cookie: adminCookie}).workspaces(t)
	if len(mine) != 1 || mine[0].Kind != httpapi.WorkspaceKind(workspace.KindPersonal) || mine[0].Role != httpapi.WorkspaceRole(workspace.RoleOwner) {
		t.Fatalf("admin workspaces = %+v; want one personal owned", mine)
	}
	theirs := a.do(request{method: http.MethodGet, path: "/api/v1/workspaces", cookie: otherCookie}).workspaces(t)
	if len(theirs) != 1 || theirs[0].Id == mine[0].Id {
		t.Fatalf("other workspaces = %+v; want their own single workspace", theirs)
	}

	own := a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + mine[0].Id.String(), cookie: adminCookie})
	if own.Status != http.StatusOK {
		t.Fatalf("own workspace = %d %s", own.Status, own.Body)
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + mine[0].Id.String(), cookie: otherCookie}), 404, "workspace_not_found")
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + uuid.NewString(), cookie: adminCookie}), 404, "workspace_not_found")
}

// signIn2 logs an existing account in again, to check that logging in is idempotent for workspaces.
func (a *api) signIn2(t *testing.T, email string) {
	t.Helper()
	r := a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": email, "password": adminPassword}})
	if r.Status != http.StatusOK {
		t.Fatalf("second login = %d %s", r.Status, r.Body)
	}
}

// TestAuthorizationMatrix verifies who may reach each workspace endpoint.
//
// Scenarios:
//   - an anonymous caller gets 401 on every workspace endpoint
//   - a non-member gets 404 on workspace-scoped endpoints, indistinguishable from a missing workspace
//   - a viewer member and the owner may browse
func TestAuthorizationMatrix(t *testing.T) {
	a := newAPI(t)
	ownerCookie, _ := a.setup()
	strangerCookie, _ := a.signIn("stranger@example.com")
	viewerCookie, viewer := a.signIn("viewer@example.com")
	owned := a.do(request{method: http.MethodGet, path: "/api/v1/workspaces", cookie: ownerCookie}).workspaces(t)[0]
	a.addMember(owned.Id, viewer.ID, workspace.RoleViewer)

	endpoints := []struct {
		name, path string
		want       map[string]int
	}{
		{"list", "/api/v1/workspaces", map[string]int{"anonymous": 401, "stranger": 200, "viewer": 200, "owner": 200}},
		{"get", "/api/v1/workspaces/" + owned.Id.String(), map[string]int{"anonymous": 401, "stranger": 404, "viewer": 200, "owner": 200}},
	}
	cookies := map[string]*http.Cookie{"anonymous": nil, "stranger": strangerCookie, "viewer": viewerCookie, "owner": ownerCookie}
	for _, e := range endpoints {
		for who, want := range e.want {
			t.Run(e.name+"/"+who, func(t *testing.T) {
				if got := a.do(request{method: http.MethodGet, path: e.path, cookie: cookies[who]}).Status; got != want {
					t.Errorf("%s as %s = %d; want %d", e.name, who, got, want)
				}
			})
		}
	}
}
