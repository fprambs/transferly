package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/scheduler"
	"github.com/fprambs/transferly/internal/core/transfer"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// heldUpload is an upload whose body the test feeds by hand, so it can hold a slot for as long as needed.
type heldUpload struct {
	id      uuid.UUID
	body    *io.PipeWriter
	replies chan reply
}

// startHeldUpload sends an upload of path with a transfer identifier and a body the test controls.
func (e *fileEnv) startHeldUpload(path string) *heldUpload {
	e.a.t.Helper()
	u := &heldUpload{id: uuid.New(), replies: make(chan reply, 1)}
	pr, pw := io.Pipe()
	u.body = pw
	go func() {
		req, err := http.NewRequestWithContext(e.a.t.Context(), http.MethodPut, e.a.srv.URL+filesPath(e.ws, e.site, "/content", q("path", path, "transferId", u.id.String())), pr)
		if err != nil {
			e.a.t.Error(err)
			return
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-CSRF-Token", e.csrf)
		req.AddCookie(e.cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			e.a.t.Error(err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		u.replies <- reply{Status: resp.StatusCode, Header: resp.Header, Body: raw}
	}()
	return u
}

// finish ends the body, so the upload can complete.
func (u *heldUpload) finish() { _ = u.body.Close() }

// transferRecord reads a transfer of the user.
func (e *fileEnv) transferRecord(id uuid.UUID) httpapi.Transfer {
	e.a.t.Helper()
	r := e.a.do(request{method: http.MethodGet, path: "/api/v1/transfers/" + id.String(), cookie: e.cookie})
	var tr httpapi.Transfer
	if err := json.Unmarshal(r.Body, &tr); err != nil {
		e.a.t.Fatalf("decoding %q: %v", r.Body, err)
	}
	return tr
}

// control performs a pause, resume or priority call on a transfer.
func (e *fileEnv) control(method, id, tail string, body any) reply {
	e.a.t.Helper()
	return e.a.send(method, "/api/v1/transfers/"+id+tail, body, e.cookie, e.csrf)
}

// TestTransfers_Queue verifies that transfers wait for their turn when a site is at its limit.
//
// Scenarios:
//   - with one slot per site, a second upload to the site waits as queued while the first runs, and starts when it ends
//   - a waiting transfer can be paused, which the record shows, and it does not start when the slot frees until it is resumed
//   - a waiting transfer can be given a higher priority and then starts before older ones
//   - a waiting transfer can be canceled: its upload answers 409 transfer_canceled and it never takes a slot
//   - an unknown priority is 422 invalid_priority
//   - pausing, resuming or reprioritizing a transfer that runs, is over or does not exist is refused, and other users cannot reach it
func TestTransfers_Queue(t *testing.T) {
	t.Parallel()
	a := newAPIWithTransfers(t, transfer.Config{EventEvery: time.Millisecond, Limits: scheduler.Limits{PerSite: 1}})
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	created := a.send(http.MethodPost, sitesPath(ws), siteBody("Files"), cookie, csrf)
	e := &fileEnv{a: a, cookie: cookie, csrf: csrf, ws: ws, site: created.site(t).Id}

	first := e.startHeldUpload("/first")
	_, _ = first.body.Write([]byte("running"))
	waitFor(t, "the first upload to run", func() bool { return a.transferState(cookie, first.id) == "running" })

	second := e.startHeldUpload("/second")
	third := e.startHeldUpload("/third")
	fourth := e.startHeldUpload("/fourth")
	for _, u := range []*heldUpload{second, third, fourth} {
		waitFor(t, "an upload to queue", func() bool { return a.transferState(cookie, u.id) == "queued" })
	}
	waitFor(t, "the second and third to be waiting in the scheduler", func() bool {
		return e.transferRecord(second.id).Priority != nil && e.transferRecord(third.id).Priority != nil
	})

	paused := e.control(http.MethodPost, second.id.String(), "/pause", nil)
	if paused.Status != http.StatusOK {
		t.Fatalf("pause = %d %s", paused.Status, paused.Body)
	}
	var record httpapi.Transfer
	mustDecode(t, paused.Body, &record)
	if record.Paused == nil || !*record.Paused || record.Status != "queued" {
		t.Errorf("paused record = %+v", record)
	}
	if tr := e.transferRecord(second.id); tr.Paused == nil || !*tr.Paused {
		t.Errorf("the listing does not show the pause: %+v", tr)
	}

	raised := e.control(http.MethodPatch, fourth.id.String(), "", map[string]int{"priority": 1})
	if raised.Status != http.StatusOK {
		t.Fatalf("priority = %d %s", raised.Status, raised.Body)
	}
	mustDecode(t, raised.Body, &record)
	if record.Priority == nil || *record.Priority != 1 {
		t.Errorf("record after raising the priority = %+v", record)
	}

	canceled := e.control(http.MethodPost, third.id.String(), "/cancel", nil)
	if canceled.Status != http.StatusNoContent {
		t.Fatalf("cancel of a waiting transfer = %d %s", canceled.Status, canceled.Body)
	}
	third.finish()
	expectProblem(t, <-third.replies, 409, "transfer_canceled")
	waitFor(t, "the waiting transfer to be canceled", func() bool { return a.transferState(cookie, third.id) == "canceled" })

	first.finish()
	if r := <-first.replies; r.Status != http.StatusCreated {
		t.Fatalf("first upload = %d %s", r.Status, r.Body)
	}
	waitFor(t, "the raised upload to start before the older paused one", func() bool { return a.transferState(cookie, fourth.id) == "running" })
	if state := a.transferState(cookie, second.id); state != "queued" {
		t.Errorf("the paused upload is %s; want it still queued", state)
	}
	fourth.finish()
	if r := <-fourth.replies; r.Status != http.StatusCreated {
		t.Fatalf("fourth upload = %d %s", r.Status, r.Body)
	}
	time.Sleep(30 * time.Millisecond)
	if state := a.transferState(cookie, second.id); state != "queued" {
		t.Errorf("the paused upload is %s after a slot freed; want it still queued", state)
	}

	if r := e.control(http.MethodPost, second.id.String(), "/resume", nil); r.Status != http.StatusOK {
		t.Fatalf("resume = %d %s", r.Status, r.Body)
	}
	waitFor(t, "the resumed upload to run", func() bool { return a.transferState(cookie, second.id) == "running" })
	expectProblem(t, e.control(http.MethodPost, second.id.String(), "/pause", nil), 409, "transfer_not_queued")
	expectProblem(t, e.control(http.MethodPost, second.id.String(), "/resume", nil), 409, "transfer_not_queued")
	expectProblem(t, e.control(http.MethodPatch, second.id.String(), "", map[string]int{"priority": 0}), 409, "transfer_not_queued")
	expectProblem(t, e.control(http.MethodPost, first.id.String(), "/pause", nil), 409, "transfer_not_queued")
	unknown := uuid.NewString()
	expectProblem(t, e.control(http.MethodPost, unknown, "/pause", nil), 404, "transfer_not_found")
	expectProblem(t, e.control(http.MethodPost, unknown, "/resume", nil), 404, "transfer_not_found")
	expectProblem(t, e.control(http.MethodPatch, unknown, "", map[string]int{"priority": 1}), 404, "transfer_not_found")
	expectProblem(t, e.control(http.MethodPatch, second.id.String(), "", map[string]int{"priority": 5}), 422, "invalid_priority")

	if _, err := a.auth.Register(t.Context(), "other@example.com", adminPassword); err != nil {
		t.Fatal(err)
	}
	login := a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": "other@example.com", "password": adminPassword}})
	otherCookie, otherCSRF := login.sessionCookie(), login.session(t).CsrfToken
	expectProblem(t, a.send(http.MethodPost, "/api/v1/transfers/"+second.id.String()+"/pause", nil, otherCookie, otherCSRF), 404, "transfer_not_found")
	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/transfers/" + second.id.String() + "/pause"}), 401, "unauthenticated")
	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/transfers/" + second.id.String() + "/pause", cookie: cookie}), 403, "csrf_invalid")

	second.finish()
	if r := <-second.replies; r.Status != http.StatusCreated {
		t.Errorf("second upload = %d %s", r.Status, r.Body)
	}
}
