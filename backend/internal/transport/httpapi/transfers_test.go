package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/transfer"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

// transfersOf lists the signed-in user's transfers.
func (a *api) transfersOf(cookie *http.Cookie, query url.Values) []httpapi.Transfer {
	a.t.Helper()
	path := "/api/v1/transfers"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	r := a.do(request{method: http.MethodGet, path: path, cookie: cookie})
	if r.Status != http.StatusOK {
		a.t.Fatalf("GET transfers = %d %s", r.Status, r.Body)
	}
	var list []httpapi.Transfer
	mustDecode(a.t, r.Body, &list)
	return list
}

// waitFor polls until the condition holds, since a download ends its record after the response was delivered.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// transferState returns the status of a transfer.
func (a *api) transferState(cookie *http.Cookie, id uuid.UUID) httpapi.TransferStatus {
	a.t.Helper()
	r := a.do(request{method: http.MethodGet, path: "/api/v1/transfers/" + id.String(), cookie: cookie})
	if r.Status != http.StatusOK {
		return ""
	}
	var tr httpapi.Transfer
	mustDecode(a.t, r.Body, &tr)
	return tr.Status
}

// TestTransfers_Recording verifies that uploads and downloads leave a record.
//
// Scenarios:
//   - an upload with a chosen identifier and size is done, keeps the identifier, and reports it in a header
//   - the record has the direction, path, name, site name, sizes, attempts, times and no error
//   - a download is recorded and ends as done once the client received everything
//   - a range download without an identifier is not recorded, with an identifier it is
//   - failed requests are recorded with their error code, and requests refused beforehand leave no trace
//   - state events are published to the owner as the transfer moves
func TestTransfers_Recording(t *testing.T) {
	e := newFileEnv(t)
	a := e.a
	id := uuid.New()

	up := a.upload(e.ws, e.site, q("path", "/docs/report.txt", "transferId", id.String(), "size", "5"), "hello", e.cookie, e.csrf)
	if up.Status == http.StatusCreated {
		t.Fatalf("upload into a missing directory succeeded")
	}
	a.send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), map[string]string{"path": "/docs"}, e.cookie, e.csrf)
	up = a.upload(e.ws, e.site, q("path", "/docs/report.txt", "transferId", id.String(), "size", "5"), "hello", e.cookie, e.csrf)
	if up.Status != http.StatusCreated || up.Header.Get("X-Transfer-Id") != id.String() {
		t.Fatalf("upload = %d %s, X-Transfer-Id %q", up.Status, up.Body, up.Header.Get("X-Transfer-Id"))
	}
	var tr httpapi.Transfer
	mustDecode(t, a.do(request{method: http.MethodGet, path: "/api/v1/transfers/" + id.String(), cookie: e.cookie}).Body, &tr)
	if tr.Status != httpapi.TransferStatus("done") || tr.Direction != "upload" || tr.Path != "/docs/report.txt" || tr.Name != "report.txt" || tr.SiteName != "Files" ||
		tr.BytesDone != 5 || tr.TotalBytes == nil || *tr.TotalBytes != 5 || tr.Attempts != 2 || tr.StartedAt == nil || tr.FinishedAt == nil || tr.ErrorCode != nil ||
		tr.SiteId == nil || *tr.SiteId != e.site || tr.WorkspaceId != e.ws {
		t.Errorf("record = %+v", tr)
	}

	states := a.events.ofType(transfer.EventState)
	seen := map[string]bool{}
	for _, ev := range states {
		if got := ev.Data.(transfer.Transfer); got.ID == id {
			seen[string(got.Status)] = true
		}
	}
	if !seen["queued"] || !seen["running"] || !seen["done"] {
		t.Errorf("state events for the transfer = %v; want queued, running and done", seen)
	}

	download := e.get("/content", q("path", "/docs/report.txt"), nil)
	downloadID, err := uuid.Parse(download.Header.Get("X-Transfer-Id"))
	if err != nil || string(download.Body) != "hello" {
		t.Fatalf("download = %d %q, X-Transfer-Id %q", download.Status, download.Body, download.Header.Get("X-Transfer-Id"))
	}
	waitFor(t, "the download to end", func() bool { return a.transferState(e.cookie, downloadID) == "done" })
	var dl httpapi.Transfer
	mustDecode(t, a.do(request{method: http.MethodGet, path: "/api/v1/transfers/" + downloadID.String(), cookie: e.cookie}).Body, &dl)
	if dl.Direction != "download" || dl.BytesDone != 5 || dl.TotalBytes == nil || *dl.TotalBytes != 5 {
		t.Errorf("download record = %+v", dl)
	}

	before := len(a.transfersOf(e.cookie, nil))
	ranged := e.get("/content", q("path", "/docs/report.txt"), map[string]string{"Range": "bytes=0-1"})
	if ranged.Status != http.StatusPartialContent || ranged.Header.Get("X-Transfer-Id") != "" || len(a.transfersOf(e.cookie, nil)) != before {
		t.Errorf("an anonymous range was recorded: %d, header %q", ranged.Status, ranged.Header.Get("X-Transfer-Id"))
	}
	rangeID := uuid.New()
	tracked := e.get("/content", q("path", "/docs/report.txt", "transferId", rangeID.String()), map[string]string{"Range": "bytes=1-3"})
	if tracked.Status != http.StatusPartialContent || tracked.Header.Get("X-Transfer-Id") != rangeID.String() {
		t.Fatalf("a range with an identifier = %d, header %q", tracked.Status, tracked.Header.Get("X-Transfer-Id"))
	}
	waitFor(t, "the range download to end", func() bool { return a.transferState(e.cookie, rangeID) == "done" })

	failing := uuid.New()
	expectProblem(t, a.upload(e.ws, e.site, q("path", "/docs/report.txt", "transferId", failing.String()), "again", e.cookie, e.csrf), 409, "path_exists")
	var failed httpapi.Transfer
	mustDecode(t, a.do(request{method: http.MethodGet, path: "/api/v1/transfers/" + failing.String(), cookie: e.cookie}).Body, &failed)
	if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != "path_exists" {
		t.Errorf("failed record = %+v", failed)
	}
	missing := uuid.New()
	expectProblem(t, e.get("/content", q("path", "/nope", "transferId", missing.String()), nil), 404, "path_not_found")
	if a.transferState(e.cookie, missing) != "failed" {
		t.Error("a failed download was not recorded as failed")
	}

	total := len(a.transfersOf(e.cookie, nil))
	expectProblem(t, a.upload(e.ws, e.site, q("path", "/x", "mode", "sideways"), "x", e.cookie, e.csrf), 422, "invalid_mode")
	expectProblem(t, a.upload(e.ws, e.site, q("path", "/x\n"), "x", e.cookie, e.csrf), 422, "invalid_path")
	expectProblem(t, a.upload(e.ws, uuid.New(), q("path", "/x"), "x", e.cookie, e.csrf), 404, "site_not_found")
	expectProblem(t, e.get("/content", q("path", "/x\n"), nil), 422, "invalid_path")
	if len(a.transfersOf(e.cookie, nil)) != total {
		t.Error("requests refused before starting left transfer records")
	}
}

// TestTransfers_Resume verifies resuming and identifier rules.
//
// Scenarios:
//   - an upload can be repeated under the same identifier with mode=resume, counting attempts and starting at the offset
//   - the identifier of a transfer of another user, or of another file, is 409 transfer_id_taken
//   - the identifier of a transfer that is still running is 409 transfer_active
func TestTransfers_Resume(t *testing.T) {
	e := newFileEnv(t)
	a := e.a
	id := uuid.New()
	a.upload(e.ws, e.site, q("path", "/movie", "transferId", id.String(), "size", "22"), "first-half|", e.cookie, e.csrf)
	done := a.upload(e.ws, e.site, q("path", "/movie", "transferId", id.String(), "size", "22", "mode", "resume", "offset", "11"), "second-half", e.cookie, e.csrf)
	if done.Status != http.StatusCreated {
		t.Fatalf("resume = %d %s", done.Status, done.Body)
	}
	var tr httpapi.Transfer
	mustDecode(t, a.do(request{method: http.MethodGet, path: "/api/v1/transfers/" + id.String(), cookie: e.cookie}).Body, &tr)
	if tr.Attempts != 2 || tr.BytesDone != 22 || tr.Status != "done" {
		t.Errorf("resumed record = %+v", tr)
	}

	expectProblem(t, a.upload(e.ws, e.site, q("path", "/other", "transferId", id.String()), "x", e.cookie, e.csrf), 409, "transfer_id_taken")
	expectProblem(t, e.get("/content", q("path", "/movie", "transferId", id.String()), nil), 409, "transfer_id_taken")

	otherCookie, other := a.signIn("other@example.com")
	otherCSRF := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: otherCookie}).session(t).CsrfToken
	otherWS := a.personal(otherCookie)
	otherSite := a.send(http.MethodPost, sitesPath(otherWS), siteBody("Mine"), otherCookie, otherCSRF).site(t).Id
	_ = other
	expectProblem(t, a.upload(otherWS, otherSite, q("path", "/f", "transferId", id.String()), "x", otherCookie, otherCSRF), 409, "transfer_id_taken")
	if r := a.do(request{method: http.MethodGet, path: "/api/v1/transfers/" + id.String(), cookie: otherCookie}); r.Status != http.StatusNotFound {
		t.Errorf("another user reads the transfer: %d", r.Status)
	}
	if len(a.transfersOf(otherCookie, nil)) != 0 {
		t.Error("another user lists transfers of someone else")
	}
}

// TestTransfers_Cancel verifies canceling a running upload.
//
// Scenarios:
//   - a running upload is visible as running with its progress, canceling it answers 204, ends the upload with
//     409 transfer_canceled and the record as canceled, keeping the partial file
//   - canceling a finished transfer is 409 transfer_not_active, an unknown one 404
//   - a running transfer cannot be deleted (409 transfer_active); a finished one can, once
func TestTransfers_Cancel(t *testing.T) {
	e := newFileEnv(t)
	a := e.a
	id := uuid.New()

	pr, pw := io.Pipe()
	replies := make(chan reply, 1)
	go func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, a.srv.URL+filesPath(e.ws, e.site, "/content", q("path", "/slow", "transferId", id.String())), pr)
		if err != nil {
			t.Error(err)
			return
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-CSRF-Token", e.csrf)
		req.AddCookie(e.cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		replies <- reply{Status: resp.StatusCode, Header: resp.Header, Body: raw}
	}()
	if _, err := pw.Write([]byte("part one")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the upload to run", func() bool {
		return a.transferState(e.cookie, id) == "running"
	})
	waitFor(t, "progress to be recorded", func() bool {
		r := a.do(request{method: http.MethodGet, path: "/api/v1/transfers/" + id.String(), cookie: e.cookie})
		var tr httpapi.Transfer
		_ = json.Unmarshal(r.Body, &tr)
		return tr.Status == "running" && tr.StartedAt != nil
	})

	expectProblem(t, a.send(http.MethodDelete, "/api/v1/transfers/"+id.String(), nil, e.cookie, e.csrf), 409, "transfer_active")
	if r := a.send(http.MethodPost, "/api/v1/transfers/"+id.String()+"/cancel", nil, e.cookie, e.csrf); r.Status != http.StatusNoContent {
		t.Fatalf("cancel = %d %s", r.Status, r.Body)
	}
	_, _ = pw.Write([]byte(" and more"))
	_ = pw.Close()
	expectProblem(t, <-replies, 409, "transfer_canceled")
	waitFor(t, "the transfer to be canceled", func() bool { return a.transferState(e.cookie, id) == "canceled" })
	if entry := e.get("/entry", q("path", "/slow"), nil); entry.Status != http.StatusOK || entry.entry(t).Size == 0 {
		t.Errorf("the partial file = %d %s; want it kept", entry.Status, entry.Body)
	}

	expectProblem(t, a.send(http.MethodPost, "/api/v1/transfers/"+id.String()+"/cancel", nil, e.cookie, e.csrf), 409, "transfer_not_active")
	expectProblem(t, a.send(http.MethodPost, "/api/v1/transfers/"+uuid.NewString()+"/cancel", nil, e.cookie, e.csrf), 404, "transfer_not_found")
	if r := a.send(http.MethodDelete, "/api/v1/transfers/"+id.String(), nil, e.cookie, e.csrf); r.Status != http.StatusNoContent {
		t.Errorf("delete of a finished transfer = %d %s", r.Status, r.Body)
	}
	expectProblem(t, a.send(http.MethodDelete, "/api/v1/transfers/"+id.String(), nil, e.cookie, e.csrf), 404, "transfer_not_found")
}

// TestTransfers_ListingAndAccess verifies listing and who may reach the endpoints.
//
// Scenarios:
//   - transfers list newest first, filter by workspace and honor the limit
//   - anonymous callers get 401, writes without the anti-CSRF token 403, and viewers of a workspace never see
//     the transfers of others
//   - a viewer's refused upload leaves no transfer, and the viewer's own downloads are theirs alone
func TestTransfers_ListingAndAccess(t *testing.T) {
	e := newFileEnv(t)
	a := e.a
	for i, name := range []string{"/one", "/two", "/three"} {
		a.upload(e.ws, e.site, q("path", name, "size", "1"), string(rune('a'+i)), e.cookie, e.csrf)
	}
	all := a.transfersOf(e.cookie, nil)
	if len(all) != 3 || all[0].Path != "/three" || all[2].Path != "/one" {
		t.Fatalf("list = %+v; want newest first", all)
	}
	if limited := a.transfersOf(e.cookie, q("limit", "2")); len(limited) != 2 {
		t.Errorf("limit 2 returned %d", len(limited))
	}
	if inWS := a.transfersOf(e.cookie, q("workspaceId", e.ws.String())); len(inWS) != 3 {
		t.Errorf("filter by workspace returned %d", len(inWS))
	}
	if none := a.transfersOf(e.cookie, q("workspaceId", uuid.NewString())); len(none) != 0 {
		t.Errorf("filter by another workspace returned %d", len(none))
	}

	id := all[0].Id.String()
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/transfers"}, {http.MethodGet, "/api/v1/transfers/" + id},
		{http.MethodDelete, "/api/v1/transfers/" + id}, {http.MethodPost, "/api/v1/transfers/" + id + "/cancel"},
	} {
		expectProblem(t, a.do(request{method: c.method, path: c.path}), 401, "unauthenticated")
		if c.method != http.MethodGet {
			expectProblem(t, a.do(request{method: c.method, path: c.path, cookie: e.cookie}), 403, "csrf_invalid")
		}
	}

	viewerCookie, viewer := a.signIn("viewer@example.com")
	a.addMember(e.ws, viewer.ID, workspace.RoleViewer)
	viewerCSRF := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: viewerCookie}).session(t).CsrfToken
	expectProblem(t, a.upload(e.ws, e.site, q("path", "/forbidden"), "x", viewerCookie, viewerCSRF), 403, "forbidden")
	if len(a.transfersOf(viewerCookie, nil)) != 0 {
		t.Error("a refused upload, or the owner's transfers, appear in the viewer's list")
	}
	got := a.do(request{method: http.MethodGet, path: filesPath(e.ws, e.site, "/content", q("path", "/one")), cookie: viewerCookie})
	viewerID, err := uuid.Parse(got.Header.Get("X-Transfer-Id"))
	if err != nil {
		t.Fatalf("the viewer's download has no transfer: %v", err)
	}
	waitFor(t, "the viewer's download to end", func() bool { return a.transferState(viewerCookie, viewerID) == "done" })
	if len(a.transfersOf(e.cookie, nil)) != 3 || len(a.transfersOf(viewerCookie, nil)) != 1 {
		t.Error("transfers leaked between users")
	}
	if !strings.Contains(string(a.do(request{method: http.MethodGet, path: "/api/v1/transfers", cookie: viewerCookie}).Body), viewerID.String()) {
		t.Error("the viewer does not see their own transfer")
	}
}
