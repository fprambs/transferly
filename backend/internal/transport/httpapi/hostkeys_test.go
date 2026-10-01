package httpapi_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

const (
	fingerprintC = "SHA256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU"
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

// forgetOnePath returns the path that forgets one key of a type by its fingerprint.
func forgetOnePath(ws uuid.UUID, host string, keyType, fingerprint string) string {
	q := url.Values{"host": {host}, "port": {"22"}, "keyType": {keyType}, "fingerprint": {fingerprint}}
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

// TestKnownHosts_SeveralKeys verifies servers that present several keys of the same type (4.13b).
//
// Scenarios:
//   - trusting a changed key with mode add keeps both keys, and with mode replace leaves only the new one
//   - an unknown mode is 422 invalid_host_key
//   - forgetting by fingerprint removes only that key, and a fingerprint that is not trusted is 404 host_key_not_found
//   - each decision is audited with the fingerprint and the mode
func TestKnownHosts_SeveralKeys(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	host := "rotating.example.com"
	withMode := func(fingerprint, mode string) map[string]any {
		body := hostKeyBody(host, fingerprint)
		body["mode"] = mode
		return body
	}
	list := func() []string {
		var keys []httpapi.KnownHost
		mustDecode(t, a.do(request{method: http.MethodGet, path: hostKeysPath(ws), cookie: cookie}).Body, &keys)
		out := make([]string, 0, len(keys))
		for _, k := range keys {
			if k.Host == host {
				out = append(out, k.Fingerprint)
			}
		}
		return out
	}

	a.send(http.MethodPost, hostKeysPath(ws), hostKeyBody(host, fingerprintA), cookie, csrf)
	if r := a.send(http.MethodPost, hostKeysPath(ws), withMode(fingerprintB, "add"), cookie, csrf); r.Status != http.StatusCreated {
		t.Fatalf("add = %d %s", r.Status, r.Body)
	}
	if got := list(); len(got) != 2 {
		t.Fatalf("keys after add = %v; want two", got)
	}
	expectProblem(t, a.send(http.MethodPost, hostKeysPath(ws), withMode(fingerprintC, "merge"), cookie, csrf), 422, "invalid_host_key")
	if r := a.send(http.MethodPost, hostKeysPath(ws), withMode(fingerprintC, "replace"), cookie, csrf); r.Status != http.StatusCreated {
		t.Fatalf("replace = %d %s", r.Status, r.Body)
	}
	if got := list(); len(got) != 1 || got[0] != fingerprintC {
		t.Fatalf("keys after replace = %v; want only %s", got, fingerprintC)
	}

	a.send(http.MethodPost, hostKeysPath(ws), withMode(fingerprintA, "add"), cookie, csrf)
	expectProblem(t, a.send(http.MethodDelete, forgetOnePath(ws, host, "ssh-ed25519", fingerprintB), nil, cookie, csrf), 404, "host_key_not_found")
	if r := a.send(http.MethodDelete, forgetOnePath(ws, host, "ssh-ed25519", fingerprintA), nil, cookie, csrf); r.Status != http.StatusNoContent {
		t.Fatalf("forget one = %d %s", r.Status, r.Body)
	}
	if got := list(); len(got) != 1 || got[0] != fingerprintC {
		t.Errorf("keys after forgetting one = %v; want only %s", got, fingerprintC)
	}

	var events []httpapi.AuditEvent
	mustDecode(t, a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + ws.String() + "/audit", cookie: cookie}).Body, &events)
	modes := map[string]bool{}
	forgotten := false
	for _, e := range events {
		meta := e.Meta
		if e.Action == "host_key.trusted" && meta["fingerprint"] != "" {
			modes[meta["mode"]] = true
		}
		if e.Action == "host_key.forgotten" && meta["fingerprint"] == fingerprintA {
			forgotten = true
		}
	}
	if !modes[""] || !modes["add"] || !modes["replace"] || !forgotten {
		t.Errorf("audited modes = %v, forgotten by fingerprint = %v; events = %+v", modes, forgotten, events)
	}
}
