package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// secondSite creates another site in the workspace of the environment.
func (e *fileEnv) secondSite() uuid.UUID {
	e.a.t.Helper()
	created := e.a.send(http.MethodPost, sitesPath(e.ws), siteBody("Second"), e.cookie, e.csrf)
	if created.Status != http.StatusCreated {
		e.a.t.Fatalf("create site = %d %s", created.Status, created.Body)
	}
	return created.site(e.a.t).Id
}

// copyBody builds the body of a copy request.
func (e *fileEnv) copyBody(target uuid.UUID, from, to string, extra map[string]any) map[string]any {
	body := map[string]any{"sourceSiteId": e.site, "sourcePath": from, "targetSiteId": target, "targetPath": to}
	for key, value := range extra {
		body[key] = value
	}
	return body
}

// TestTransfers_Copy verifies copying files between two sites of a workspace through the server.
//
// Scenarios:
//   - a copy answers 202 with a transfer that runs in the background, ends done with the size, and leaves the same content at the target
//   - a copy onto an existing file fails as a transfer with path_exists unless the policy says otherwise, and the skip policy leaves it and still finishes
//   - a copy asked to verify reads the destination back and still finishes when the bytes match
//   - a folder is copied whole with its subfolders, whatever the size of its files
//   - a missing source fails as a transfer with path_not_found
//   - copying onto itself, an empty path, an unknown source or target site and a taken transfer identifier are refused at once
//   - the request needs a session and the anti-CSRF token
func TestTransfers_Copy(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	a := e.a
	other := e.secondSite()
	if r := a.upload(e.ws, e.site, q("path", "/a.txt"), "hello copy", e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("upload = %d %s", r.Status, r.Body)
	}
	path := "/api/v1/workspaces/" + e.ws.String() + "/copies"

	started := a.send(http.MethodPost, path, e.copyBody(other, "/a.txt", "/b.txt", nil), e.cookie, e.csrf)
	if started.Status != http.StatusAccepted {
		t.Fatalf("copy = %d %s", started.Status, started.Body)
	}
	var record struct {
		ID        uuid.UUID `json:"id"`
		Direction string    `json:"direction"`
		Name      string    `json:"name"`
		SiteName  string    `json:"siteName"`
	}
	mustDecode(t, started.Body, &record)
	if record.Direction != "copy" || record.Name != "b.txt" || record.SiteName != "Files → Second" {
		t.Errorf("record = %+v", record)
	}
	waitFor(t, "the copy to finish", func() bool { return a.transferState(e.cookie, record.ID) == "done" })
	if done := e.transferRecord(record.ID); done.TotalBytes == nil || *done.TotalBytes != 10 || done.BytesDone != 10 {
		t.Errorf("finished copy = %+v; want 10 of 10 bytes", done)
	}
	downloaded := a.do(request{method: http.MethodGet, path: filesPath(e.ws, other, "/content", q("path", "/b.txt")), cookie: e.cookie})
	if downloaded.Status != http.StatusOK || string(downloaded.Body) != "hello copy" {
		t.Errorf("the copy holds %d %q", downloaded.Status, downloaded.Body)
	}

	refused := a.send(http.MethodPost, path, e.copyBody(other, "/a.txt", "/b.txt", nil), e.cookie, e.csrf)
	var second struct {
		ID uuid.UUID `json:"id"`
	}
	mustDecode(t, refused.Body, &second)
	waitFor(t, "the second copy to fail", func() bool { return a.transferState(e.cookie, second.ID) == "failed" })
	if got := e.transferRecord(second.ID); got.ErrorCode == nil || *got.ErrorCode != "path_exists" {
		t.Errorf("error code = %v; want path_exists", got.ErrorCode)
	}
	forced := a.send(http.MethodPost, path, e.copyBody(other, "/a.txt", "/b.txt", map[string]any{"policy": "overwrite"}), e.cookie, e.csrf)
	mustDecode(t, forced.Body, &second)
	waitFor(t, "the overwriting copy to finish", func() bool { return a.transferState(e.cookie, second.ID) == "done" })

	skipped := a.send(http.MethodPost, path, e.copyBody(other, "/a.txt", "/b.txt", map[string]any{"policy": "skip"}), e.cookie, e.csrf)
	mustDecode(t, skipped.Body, &second)
	waitFor(t, "the skipping copy to finish", func() bool { return a.transferState(e.cookie, second.ID) == "done" })

	if r := a.send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), map[string]string{"path": "/tree"}, e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("mkdir = %d %s", r.Status, r.Body)
	}
	if r := a.upload(e.ws, e.site, q("path", "/tree/leaf.txt"), "leaf", e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("upload = %d %s", r.Status, r.Body)
	}
	folder := a.send(http.MethodPost, path, e.copyBody(other, "/tree", "/tree-copy", nil), e.cookie, e.csrf)
	mustDecode(t, folder.Body, &second)
	waitFor(t, "the folder copy to finish", func() bool { return a.transferState(e.cookie, second.ID) == "done" })
	leaf := a.do(request{method: http.MethodGet, path: filesPath(e.ws, other, "/content", q("path", "/tree-copy/leaf.txt")), cookie: e.cookie})
	if leaf.Status != http.StatusOK || string(leaf.Body) != "leaf" {
		t.Errorf("the copied folder holds %d %q", leaf.Status, leaf.Body)
	}

	verified := a.send(http.MethodPost, path, e.copyBody(other, "/a.txt", "/verified.txt", map[string]any{"verify": true}), e.cookie, e.csrf)
	mustDecode(t, verified.Body, &second)
	waitFor(t, "the verified copy to finish", func() bool { return a.transferState(e.cookie, second.ID) == "done" })

	missing := a.send(http.MethodPost, path, e.copyBody(other, "/nothing", "/c.txt", nil), e.cookie, e.csrf)
	mustDecode(t, missing.Body, &second)
	waitFor(t, "the copy of a missing file to fail", func() bool { return a.transferState(e.cookie, second.ID) == "failed" })
	if got := e.transferRecord(second.ID); got.ErrorCode == nil || *got.ErrorCode != "path_not_found" {
		t.Errorf("error code = %v; want path_not_found", got.ErrorCode)
	}

	expectProblem(t, a.send(http.MethodPost, path, map[string]any{"sourceSiteId": e.site, "sourcePath": "/a.txt", "targetSiteId": e.site, "targetPath": "/a.txt"}, e.cookie, e.csrf), 422, "invalid_path")
	expectProblem(t, a.send(http.MethodPost, path, e.copyBody(other, "/a.txt", "", nil), e.cookie, e.csrf), 422, "invalid_path")
	expectProblem(t, a.send(http.MethodPost, path, e.copyBody(uuid.New(), "/a.txt", "/d.txt", nil), e.cookie, e.csrf), 404, "site_not_found")
	unknownSource := e.copyBody(other, "/a.txt", "/d.txt", map[string]any{"sourceSiteId": uuid.New()})
	expectProblem(t, a.send(http.MethodPost, path, unknownSource, e.cookie, e.csrf), 404, "site_not_found")
	expectProblem(t, a.send(http.MethodPost, path, e.copyBody(other, "/a.txt", "/e.txt", map[string]any{"transferId": record.ID}), e.cookie, e.csrf), 409, "transfer_id_taken")
	expectProblem(t, a.do(request{method: http.MethodPost, path: path, body: e.copyBody(other, "/a.txt", "/f.txt", nil)}), 401, "unauthenticated")
	expectProblem(t, a.do(request{method: http.MethodPost, path: path, cookie: e.cookie, body: e.copyBody(other, "/a.txt", "/f.txt", nil)}), 403, "csrf_invalid")
}
