package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// logMessage is the payload of a connection.log message.
type logMessage struct {
	WorkspaceID uuid.UUID         `json:"workspaceId"`
	SiteID      uuid.UUID         `json:"siteId"`
	Time        time.Time         `json:"time"`
	Level       string            `json:"level"`
	Step        string            `json:"step"`
	Params      map[string]string `json:"params"`
}

// nextLog waits for the next connection.log message and decodes it.
func (c *wsClient) nextLog(t *testing.T) logMessage {
	t.Helper()
	var m logMessage
	if err := json.Unmarshal(c.expect(t, "connection.log").Data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestConnectionLog_Listing covers the steps listing a folder tells the connection log of the site (ADR-0011).
//
// Use cases:
//   - listing tells the folder it reads and then how many entries it found, about the site and workspace listed
//   - a listing that fails tells the stable code the API answers
func TestConnectionLog_Listing(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	conn, _ := e.a.connectEvents(e.cookie)

	e.get("", q("path", "/"), nil)
	listing := conn.nextLog(t)
	if listing.Step != "listing" || listing.Level != "info" || listing.Params["path"] != "/" || listing.SiteID != e.site || listing.WorkspaceID != e.ws || listing.Time.IsZero() {
		t.Errorf("listing = %+v", listing)
	}
	if listed := conn.nextLog(t); listed.Step != "listed" || listed.Params["count"] != "0" {
		t.Errorf("listed = %+v", listed)
	}

	e.get("", q("path", "/missing"), nil)
	conn.nextLog(t)
	if failed := conn.nextLog(t); failed.Step != "failed" || failed.Level != "error" || failed.Params["code"] != "path_not_found" || failed.Params["path"] != "/missing" {
		t.Errorf("failed = %+v", failed)
	}
}

// TestDisconnectSite covers closing the connections of the caller to a site (4.9b).
//
// Use cases:
//   - after listing, disconnecting closes the pooled connection, tells the log, and the next listing connects again
//   - a site that does not exist is not found
func TestDisconnectSite(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	conn, _ := e.a.connectEvents(e.cookie)
	e.get("", q("path", "/"), nil)
	conn.nextLog(t)
	conn.nextLog(t)
	key := connections.Key{UserID: e.a.userID(e.cookie), WorkspaceID: e.ws, SiteID: e.site}
	if idle, _ := e.a.pool.Stats(key); idle != 1 {
		t.Fatalf("idle connections after listing = %d; want 1", idle)
	}

	if r := e.a.send(http.MethodDelete, sitesPath(e.ws, e.site.String())+"/connection", nil, e.cookie, e.csrf); r.Status != http.StatusNoContent {
		t.Fatalf("disconnect = %d %s", r.Status, r.Body)
	}
	if idle, total := e.a.pool.Stats(key); idle != 0 || total != 0 {
		t.Errorf("connections after disconnecting = %d idle, %d total; want none", idle, total)
	}
	if entry := conn.nextLog(t); entry.Step != "disconnected" || entry.SiteID != e.site {
		t.Errorf("log after disconnecting = %+v", entry)
	}
	if r := e.get("", q("path", "/"), nil); r.Status != http.StatusOK {
		t.Errorf("listing after disconnecting = %d", r.Status)
	}
	expectProblem(t, e.a.send(http.MethodDelete, sitesPath(e.ws, uuid.NewString())+"/connection", nil, e.cookie, e.csrf), http.StatusNotFound, "site_not_found")
}

// TestConnectionLog_Canceled covers a listing the client gives up before it ends (4.9b).
//
// Use cases:
//   - the log tells that the listing was canceled, as a warning, instead of a failure with a code
func TestConnectionLog_Canceled(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	body := siteBody(slowSite)
	created := e.a.send(http.MethodPost, sitesPath(e.ws), body, e.cookie, e.csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Status, created.Body)
	}
	slow := created.site(t).Id
	conn, _ := e.a.connectEvents(e.cookie)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.a.srv.URL+filesPath(e.ws, slow, "", q("path", "/")), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(e.cookie)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		_ = resp.Body.Close()
		t.Fatal("a listing of a site that never answers came back")
	}
	if entry := conn.nextLog(t); entry.Step != "canceled" || entry.Level != "warn" || entry.Params["path"] != "/" || entry.SiteID != slow {
		t.Errorf("log of a canceled listing = %+v", entry)
	}
}

// TestOpenConnections covers telling which sites are connected (4.11).
//
// Use cases:
//   - listing a site opens a connection, which the event stream tells and the list of open connections shows
//   - disconnecting tells it closed and empties the list
//   - a workspace the caller does not belong to is not found
func TestOpenConnections(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	conn, _ := e.a.connectEvents(e.cookie)
	path := "/api/v1/workspaces/" + e.ws.String() + "/connections"
	open := func() []uuid.UUID {
		var got httpapi.OpenConnections
		mustDecode(t, e.a.do(request{method: http.MethodGet, path: path, cookie: e.cookie}).Body, &got)
		return got.SiteIds
	}
	if got := open(); len(got) != 0 {
		t.Fatalf("open before listing = %v", got)
	}

	e.get("", q("path", "/"), nil)
	var state struct {
		WorkspaceID uuid.UUID `json:"workspaceId"`
		SiteID      uuid.UUID `json:"siteId"`
		Open        bool      `json:"open"`
	}
	mustDecode(t, conn.expect(t, "connection.state").Data, &state)
	if !state.Open || state.SiteID != e.site || state.WorkspaceID != e.ws {
		t.Errorf("state after listing = %+v", state)
	}
	if got := open(); len(got) != 1 || got[0] != e.site {
		t.Errorf("open after listing = %v", got)
	}

	e.a.send(http.MethodDelete, sitesPath(e.ws, e.site.String())+"/connection", nil, e.cookie, e.csrf)
	mustDecode(t, conn.expect(t, "connection.state").Data, &state)
	if state.Open {
		t.Errorf("state after disconnecting = %+v", state)
	}
	if got := open(); len(got) != 0 {
		t.Errorf("open after disconnecting = %v", got)
	}
	expectProblem(t, e.a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + uuid.NewString() + "/connections", cookie: e.cookie}), http.StatusNotFound, "workspace_not_found")
}
