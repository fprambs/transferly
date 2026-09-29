package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

// collectionsPath returns the collections path of a workspace, or one collection when id is given.
func collectionsPath(ws uuid.UUID, id ...string) string {
	path := "/api/v1/workspaces/" + ws.String() + "/collections"
	if len(id) > 0 {
		path += "/" + id[0]
	}
	return path
}

// collection decodes the reply as a collection.
func (r reply) collection(t *testing.T) httpapi.Collection {
	t.Helper()
	var c httpapi.Collection
	mustDecode(t, r.Body, &c)
	return c
}

// TestCollections_Lifecycle verifies creating, listing, moving and deleting collections over HTTP.
//
// Scenarios:
//   - collections are created at the top level and inside others, listed by name with their parents
//   - renaming, moving under another collection and moving to the top level work
//   - duplicate names among siblings are 409, invalid names 422, missing parents and collections 404
//   - moves into a descendant are 422 collection_cycle and the ninth level is 422 collection_too_deep
//   - only empty collections can be deleted (409 collection_not_empty)
//   - sites are placed in collections, listed with them, moved between them and back to the top level; a missing
//     collection is 404; a collection holding a site cannot be deleted
func TestCollections_Lifecycle(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	acme := a.send(http.MethodPost, collectionsPath(ws), map[string]any{"name": "Acme"}, cookie, csrf)
	if acme.Status != http.StatusCreated || acme.collection(t).ParentId != nil {
		t.Fatalf("create = %d %s", acme.Status, acme.Body)
	}
	acmeID := acme.collection(t).Id
	prod := a.send(http.MethodPost, collectionsPath(ws), map[string]any{"name": "Production", "parentId": acmeID}, cookie, csrf)
	if prod.Status != http.StatusCreated || prod.collection(t).ParentId == nil || *prod.collection(t).ParentId != acmeID {
		t.Fatalf("create inside = %d %s", prod.Status, prod.Body)
	}
	prodID := prod.collection(t).Id
	expectProblem(t, a.send(http.MethodPost, collectionsPath(ws), map[string]any{"name": "acme"}, cookie, csrf), 409, "collection_name_taken")
	expectProblem(t, a.send(http.MethodPost, collectionsPath(ws), map[string]any{"name": " "}, cookie, csrf), 422, "invalid_name")
	expectProblem(t, a.send(http.MethodPost, collectionsPath(ws), map[string]any{"name": "Orphan", "parentId": uuid.New()}, cookie, csrf), 404, "collection_not_found")
	expectProblem(t, a.send(http.MethodPost, collectionsPath(ws), "{not json", cookie, csrf), 400, "invalid_request")

	var list []httpapi.Collection
	mustDecode(t, a.do(request{method: http.MethodGet, path: collectionsPath(ws), cookie: cookie}).Body, &list)
	if len(list) != 2 || list[0].Name != "Acme" || list[1].Name != "Production" {
		t.Errorf("list = %+v", list)
	}

	renamed := a.send(http.MethodPatch, collectionsPath(ws, prodID.String()), map[string]any{"name": "Prod"}, cookie, csrf)
	if renamed.Status != http.StatusOK || renamed.collection(t).Name != "Prod" {
		t.Errorf("rename = %d %s", renamed.Status, renamed.Body)
	}
	other := a.send(http.MethodPost, collectionsPath(ws), map[string]any{"name": "Clients"}, cookie, csrf).collection(t).Id
	moved := a.send(http.MethodPatch, collectionsPath(ws, prodID.String()), map[string]any{"parentId": other}, cookie, csrf)
	if moved.Status != http.StatusOK || *moved.collection(t).ParentId != other {
		t.Errorf("move = %d %s", moved.Status, moved.Body)
	}
	top := a.send(http.MethodPatch, collectionsPath(ws, prodID.String()), map[string]any{"moveToTop": true}, cookie, csrf)
	if top.Status != http.StatusOK || top.collection(t).ParentId != nil {
		t.Errorf("move to the top = %d %s", top.Status, top.Body)
	}
	expectProblem(t, a.send(http.MethodPatch, collectionsPath(ws, acmeID.String()), map[string]any{"parentId": acmeID}, cookie, csrf), 422, "collection_cycle")
	expectProblem(t, a.send(http.MethodPatch, collectionsPath(ws, uuid.NewString()), map[string]any{"name": "x"}, cookie, csrf), 404, "collection_not_found")

	deep := acmeID
	for i := 2; i <= 8; i++ {
		deep = a.send(http.MethodPost, collectionsPath(ws), map[string]any{"name": "L" + string(rune('0'+i)), "parentId": deep}, cookie, csrf).collection(t).Id
	}
	expectProblem(t, a.send(http.MethodPost, collectionsPath(ws), map[string]any{"name": "L9", "parentId": deep}, cookie, csrf), 422, "collection_too_deep")
	expectProblem(t, a.send(http.MethodPatch, collectionsPath(ws, acmeID.String()), map[string]any{"parentId": deep}, cookie, csrf), 422, "collection_cycle")

	placed := siteBody("In a collection")
	placed["collectionId"] = other
	created := a.send(http.MethodPost, sitesPath(ws), placed, cookie, csrf)
	if created.Status != http.StatusCreated || created.site(t).CollectionId == nil || *created.site(t).CollectionId != other {
		t.Fatalf("create site in a collection = %d %s", created.Status, created.Body)
	}
	siteID := created.site(t).Id.String()
	var sites []httpapi.Site
	mustDecode(t, a.do(request{method: http.MethodGet, path: sitesPath(ws), cookie: cookie}).Body, &sites)
	if len(sites) != 1 || sites[0].CollectionId == nil || *sites[0].CollectionId != other {
		t.Errorf("listed sites = %+v", sites)
	}
	expectProblem(t, a.send(http.MethodDelete, collectionsPath(ws, other.String()), nil, cookie, csrf), 409, "collection_not_empty")
	invalid := siteBody("In a collection")
	invalid["collectionId"] = uuid.New()
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), siteBody2(invalid, "Elsewhere"), cookie, csrf), 404, "collection_not_found")
	expectProblem(t, a.send(http.MethodPut, sitesPath(ws, siteID), siteBody2(invalid, "In a collection"), cookie, csrf), 404, "collection_not_found")

	back := a.send(http.MethodPut, sitesPath(ws, siteID), siteBody("In a collection"), cookie, csrf)
	if back.Status != http.StatusOK || back.site(t).CollectionId != nil {
		t.Errorf("move the site to the top level = %d %s", back.Status, back.Body)
	}
	if r := a.send(http.MethodDelete, collectionsPath(ws, other.String()), nil, cookie, csrf); r.Status != http.StatusNoContent {
		t.Errorf("delete an empty collection = %d %s", r.Status, r.Body)
	}
	expectProblem(t, a.send(http.MethodDelete, collectionsPath(ws, other.String()), nil, cookie, csrf), 404, "collection_not_found")
	expectProblem(t, a.send(http.MethodDelete, collectionsPath(ws, acmeID.String()), nil, cookie, csrf), 409, "collection_not_empty")
}

// siteBody2 copies site settings under another name.
func siteBody2(from map[string]any, name string) map[string]any {
	out := map[string]any{}
	for k, v := range from {
		out[k] = v
	}
	out["name"] = name
	return out
}

// TestCollections_AuthorizationMatrix verifies who may reach each collection endpoint.
//
// Scenarios:
//   - anonymous callers get 401 and writers without the anti-CSRF token 403 csrf_invalid
//   - a stranger gets 404 workspace_not_found everywhere
//   - a viewer may list but gets 403 forbidden on create, update and delete
//   - an editor may change collections
//   - a collection of another workspace is 404 collection_not_found
func TestCollections_AuthorizationMatrix(t *testing.T) {
	a := newAPI(t)
	ownerCookie, ownerCSRF := a.setup()
	ws := a.personal(ownerCookie)
	strangerCookie, _ := a.signIn("stranger@example.com")
	viewerCookie, viewer := a.signIn("viewer@example.com")
	editorCookie, editor := a.signIn("editor@example.com")
	a.addMember(ws, viewer.ID, workspace.RoleViewer)
	a.addMember(ws, editor.ID, workspace.RoleEditor)
	id := a.send(http.MethodPost, collectionsPath(ws), map[string]any{"name": "Mine"}, ownerCookie, ownerCSRF).collection(t).Id.String()
	csrfOf := func(c *http.Cookie) string {
		return a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: c}).session(t).CsrfToken
	}
	strangerCSRF, viewerCSRF, editorCSRF := csrfOf(strangerCookie), csrfOf(viewerCookie), csrfOf(editorCookie)

	calls := []struct {
		name, method, path string
		body               any
	}{
		{"list", http.MethodGet, collectionsPath(ws), nil},
		{"create", http.MethodPost, collectionsPath(ws), map[string]any{"name": "New"}},
		{"update", http.MethodPatch, collectionsPath(ws, id), map[string]any{"name": "Renamed"}},
		{"delete", http.MethodDelete, collectionsPath(ws, uuid.NewString()), nil},
	}
	for _, c := range calls {
		expectProblem(t, a.do(request{method: c.method, path: c.path, body: c.body}), 401, "unauthenticated")
		if c.method != http.MethodGet {
			expectProblem(t, a.do(request{method: c.method, path: c.path, body: c.body, cookie: editorCookie}), 403, "csrf_invalid")
		}
		expectProblem(t, a.send(c.method, c.path, c.body, strangerCookie, strangerCSRF), 404, "workspace_not_found")
	}
	if r := a.send(calls[0].method, calls[0].path, nil, viewerCookie, viewerCSRF); r.Status != http.StatusOK {
		t.Errorf("viewer list = %d %s", r.Status, r.Body)
	}
	for _, c := range calls[1:] {
		expectProblem(t, a.send(c.method, c.path, c.body, viewerCookie, viewerCSRF), 403, "forbidden")
	}
	if r := a.send(calls[1].method, calls[1].path, calls[1].body, editorCookie, editorCSRF); r.Status != http.StatusCreated {
		t.Errorf("editor create = %d %s", r.Status, r.Body)
	}
	if r := a.send(calls[2].method, calls[2].path, calls[2].body, editorCookie, editorCSRF); r.Status != http.StatusOK {
		t.Errorf("editor update = %d %s", r.Status, r.Body)
	}

	strangerWS := a.personal(strangerCookie)
	expectProblem(t, a.send(http.MethodPatch, collectionsPath(strangerWS, id), map[string]any{"name": "Taken"}, strangerCookie, strangerCSRF), 404, "collection_not_found")
	expectProblem(t, a.send(http.MethodDelete, collectionsPath(strangerWS, id), nil, strangerCookie, strangerCSRF), 404, "collection_not_found")
	if !strings.Contains(string(a.do(request{method: http.MethodGet, path: collectionsPath(strangerWS), cookie: strangerCookie}).Body), "[]") {
		t.Error("another workspace lists collections that are not its own")
	}
}

// TestSites_Move verifies moving a saved site between collections over HTTP (4.14h).
//
// Scenarios:
//   - a site moves into a collection and back to the top level, and each move is audited as a change of the site
//   - a missing collection is 404 collection_not_found, a missing site 404 site_not_found, a malformed body 400
//   - a viewer gets 403 forbidden and a stranger 404 workspace_not_found
func TestSites_Move(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	siteID := a.send(http.MethodPost, sitesPath(ws), siteBody("Prod"), cookie, csrf).site(t).Id.String()
	var clients httpapi.Collection
	mustDecode(t, a.send(http.MethodPost, "/api/v1/workspaces/"+ws.String()+"/collections", map[string]string{"name": "Clients"}, cookie, csrf).Body, &clients)
	move := sitesPath(ws, siteID) + "/move"

	in := a.send(http.MethodPost, move, map[string]string{"collectionId": clients.Id.String()}, cookie, csrf)
	if got := in.site(t); in.Status != http.StatusOK || got.CollectionId == nil || *got.CollectionId != clients.Id {
		t.Fatalf("move in = %d %s", in.Status, in.Body)
	}
	if out := a.send(http.MethodPost, move, map[string]string{}, cookie, csrf); out.Status != http.StatusOK || out.site(t).CollectionId != nil {
		t.Errorf("move out = %d %s", out.Status, out.Body)
	}
	expectProblem(t, a.send(http.MethodPost, move, map[string]string{"collectionId": uuid.NewString()}, cookie, csrf), 404, "collection_not_found")
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws, uuid.NewString())+"/move", map[string]string{}, cookie, csrf), 404, "site_not_found")
	expectProblem(t, a.send(http.MethodPost, move, "{oops", cookie, csrf), 400, "invalid_request")

	var events []httpapi.AuditEvent
	mustDecode(t, a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + ws.String() + "/audit?action=site.updated", cookie: cookie}).Body, &events)
	if len(events) != 2 {
		t.Errorf("audited moves = %+v; want two", events)
	}

	viewerCookie, viewer := a.signIn("viewer@example.com")
	a.addMember(ws, viewer.ID, workspace.RoleViewer)
	viewerCSRF := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: viewerCookie}).session(t).CsrfToken
	expectProblem(t, a.send(http.MethodPost, move, map[string]string{}, viewerCookie, viewerCSRF), 403, "forbidden")
	strangerCookie, _ := a.signIn("stranger@example.com")
	strangerCSRF := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: strangerCookie}).session(t).CsrfToken
	expectProblem(t, a.send(http.MethodPost, move, map[string]string{}, strangerCookie, strangerCSRF), 404, "workspace_not_found")
}
