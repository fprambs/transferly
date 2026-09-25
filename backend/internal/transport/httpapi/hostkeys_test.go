package httpapi_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

const (
	fingerprintA = "SHA256:uU0nuZNNPgilLlLX2n2r+sSE7+N6U4DukIj3rOLvzek"
	fingerprintB = "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8"
)

// hostKeysPath returns the known hosts path of a workspace.
func hostKeysPath(ws uuid.UUID) string {
	return "/api/v1/workspaces/" + ws.String() + "/known-hosts"
}

// forgetPath returns the path that forgets one key.
func forgetPath(ws uuid.UUID, host string, port int, keyType string) string {
	q := url.Values{"host": {host}, "port": {strconv.Itoa(port)}, "keyType": {keyType}}
	return hostKeysPath(ws) + "?" + q.Encode()
}

// hostKeyBody returns the request that trusts a key.
func hostKeyBody(host, fingerprint string) map[string]any {
	return map[string]any{"host": host, "port": 22, "keyType": "ssh-ed25519", "fingerprint": fingerprint}
}

// TestKnownHosts_Lifecycle verifies trusting, listing and forgetting server keys over HTTP.
//
// Scenarios:
//   - trusting a key returns it and lists it; trusting the same key again succeeds
//   - a different key of the same type is 409 host_key_changed until the old one is forgotten
//   - malformed keys are 422 invalid_host_key, malformed bodies are 400
//   - forgetting removes the key; forgetting it again is 404 host_key_not_found; missing query parameters are 400
func TestKnownHosts_Lifecycle(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	created := a.send(http.MethodPost, hostKeysPath(ws), hostKeyBody("files.example.com", fingerprintA), cookie, csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("trust = %d %s", created.Status, created.Body)
	}
	var key httpapi.KnownHost
	mustDecode(t, created.Body, &key)
	if key.Host != "files.example.com" || key.Port != 22 || key.KeyType != "ssh-ed25519" || key.Fingerprint != fingerprintA || key.FirstSeen.IsZero() {
		t.Errorf("trusted key = %+v", key)
	}
	if again := a.send(http.MethodPost, hostKeysPath(ws), hostKeyBody("files.example.com", fingerprintA), cookie, csrf); again.Status != http.StatusCreated {
		t.Errorf("trusting twice = %d %s", again.Status, again.Body)
	}
	var list []httpapi.KnownHost
	mustDecode(t, a.do(request{method: http.MethodGet, path: hostKeysPath(ws), cookie: cookie}).Body, &list)
	if len(list) != 1 {
		t.Fatalf("list = %+v; want one key", list)
	}

	expectProblem(t, a.send(http.MethodPost, hostKeysPath(ws), hostKeyBody("files.example.com", fingerprintB), cookie, csrf), 409, "host_key_changed")
	expectProblem(t, a.send(http.MethodPost, hostKeysPath(ws), hostKeyBody("files.example.com", "MD5:aa"), cookie, csrf), 422, "invalid_host_key")
	expectProblem(t, a.send(http.MethodPost, hostKeysPath(ws), "{not json", cookie, csrf), 400, "invalid_request")

	if r := a.send(http.MethodDelete, forgetPath(ws, "files.example.com", 22, "ssh-ed25519"), nil, cookie, csrf); r.Status != http.StatusNoContent {
		t.Fatalf("forget = %d %s", r.Status, r.Body)
	}
	expectProblem(t, a.send(http.MethodDelete, forgetPath(ws, "files.example.com", 22, "ssh-ed25519"), nil, cookie, csrf), 404, "host_key_not_found")
	expectProblem(t, a.send(http.MethodDelete, hostKeysPath(ws), nil, cookie, csrf), 400, "invalid_request")
	if r := a.send(http.MethodPost, hostKeysPath(ws), hostKeyBody("files.example.com", fingerprintB), cookie, csrf); r.Status != http.StatusCreated {
		t.Errorf("trusting the new key after forgetting the old = %d %s", r.Status, r.Body)
	}
}

// TestKnownHosts_AuthorizationMatrix verifies who may reach each known host endpoint.
//
// Scenarios:
//   - anonymous callers get 401 and callers without the anti-CSRF token get 403 csrf_invalid on writes
//   - a stranger gets 404 workspace_not_found on every endpoint
//   - a viewer may list but gets 403 forbidden on trust and forget
//   - an editor may trust and forget
//   - keys trusted in one workspace do not exist in another
func TestKnownHosts_AuthorizationMatrix(t *testing.T) {
	a := newAPI(t)
	ownerCookie, ownerCSRF := a.setup()
	ws := a.personal(ownerCookie)
	strangerCookie, _ := a.signIn("stranger@example.com")
	viewerCookie, viewer := a.signIn("viewer@example.com")
	editorCookie, editor := a.signIn("editor@example.com")
	a.addMember(ws, viewer.ID, workspace.RoleViewer)
	a.addMember(ws, editor.ID, workspace.RoleEditor)
	a.send(http.MethodPost, hostKeysPath(ws), hostKeyBody("files.example.com", fingerprintA), ownerCookie, ownerCSRF)

	csrfOf := func(c *http.Cookie) string {
		return a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: c}).session(t).CsrfToken
	}
	strangerCSRF, viewerCSRF, editorCSRF := csrfOf(strangerCookie), csrfOf(viewerCookie), csrfOf(editorCookie)

	calls := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, hostKeysPath(ws), nil},
		{http.MethodPost, hostKeysPath(ws), hostKeyBody("other.example.com", fingerprintA)},
		{http.MethodDelete, forgetPath(ws, "files.example.com", 22, "ssh-ed25519"), nil},
	}
	for _, c := range calls {
		expectProblem(t, a.do(request{method: c.method, path: c.path, body: c.body}), 401, "unauthenticated")
		if c.method != http.MethodGet {
			expectProblem(t, a.do(request{method: c.method, path: c.path, body: c.body, cookie: editorCookie}), 403, "csrf_invalid")
		}
		expectProblem(t, a.send(c.method, c.path, c.body, strangerCookie, strangerCSRF), 404, "workspace_not_found")
	}
	if r := a.send(http.MethodGet, calls[0].path, nil, viewerCookie, viewerCSRF); r.Status != http.StatusOK {
		t.Errorf("viewer list = %d %s", r.Status, r.Body)
	}
	for _, c := range calls[1:] {
		expectProblem(t, a.send(c.method, c.path, c.body, viewerCookie, viewerCSRF), 403, "forbidden")
	}
	if r := a.send(http.MethodPost, calls[1].path, calls[1].body, editorCookie, editorCSRF); r.Status != http.StatusCreated {
		t.Errorf("editor trust = %d %s", r.Status, r.Body)
	}
	if r := a.send(http.MethodDelete, calls[2].path, nil, editorCookie, editorCSRF); r.Status != http.StatusNoContent {
		t.Errorf("editor forget = %d %s", r.Status, r.Body)
	}

	var strangerKeys []httpapi.KnownHost
	mustDecode(t, a.do(request{method: http.MethodGet, path: hostKeysPath(a.personal(strangerCookie)), cookie: strangerCookie}).Body, &strangerKeys)
	if len(strangerKeys) != 0 {
		t.Errorf("another workspace sees keys: %+v", strangerKeys)
	}
}
