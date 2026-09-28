package httpapi_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
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
