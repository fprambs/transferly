package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// TestListConnectors verifies the catalog of connectors over HTTP (5.1).
//
// Use cases:
//   - a signed-in user gets the built-in connectors with their capabilities, settings with defaults, bounds and choices, ways of
//     authenticating and whether the instance offers them; settings without them leave them out
//   - an anonymous request is refused
func TestListConnectors(t *testing.T) {
	a := newAPI(t)
	cookie, _ := a.setup()
	r := a.do(request{method: http.MethodGet, path: "/api/v1/connectors", cookie: cookie})
	var list []httpapi.Connector
	mustDecode(t, r.Body, &list)
	if r.Status != http.StatusOK || len(list) != 2 || list[0].Type != "ftp" || list[1].Type != "sftp" {
		t.Fatalf("connectors = %d %+v", r.Status, list)
	}
	sftp := list[1]
	if !sftp.EnabledByDefault || !sftp.Enabled || !list[0].Enabled || sftp.Maturity != "stable" || !sftp.Capabilities.Resume || sftp.Capabilities.ServerCopy {
		t.Errorf("sftp = %+v", sftp)
	}
	host, port := sftp.Fields[0], sftp.Fields[1]
	if host.Name != "host" || host.Kind != "host" || host.Group != "connection" || !host.Required || host.Default != nil || *host.Port != "port" || host.Choices != nil {
		t.Errorf("host = %+v", host)
	}
	if *port.Default != "22" || *port.Min != 1 || *port.Max != 65535 {
		t.Errorf("port = %+v", port)
	}
	if username := sftp.Fields[3]; username.Name != "username" || username.Fixed == nil || (*username.Fixed)[0].Value != "anonymous" {
		t.Errorf("username = %+v", username)
	}
	key := sftp.Fields[5]
	if key.Name != "private_key" || key.Kind != "private_key" || *key.Passphrase != "passphrase" || key.When == nil || key.When.Field != "auth" || (*key.When.In)[0] != "key" {
		t.Errorf("private key = %+v", key)
	}
	if proxyHost := sftp.Fields[8]; proxyHost.Name != "proxy_host" || proxyHost.When == nil || proxyHost.When.In != nil {
		t.Errorf("proxy host = %+v", proxyHost)
	}
	ftpPort := list[0].Fields[2]
	if encryption := list[0].Fields[1]; len(*encryption.Choices) != 4 || ftpPort.Defaults == nil || (*ftpPort.Defaults)[0].Value != "990" {
		t.Errorf("encryption = %+v, port = %+v", encryption, ftpPort)
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/connectors"}), http.StatusUnauthorized, "unauthenticated")
}

// connectorsOf reads the catalog as a user and returns whether each connector is offered, by type.
func connectorsOf(t *testing.T, u *mfaUser) map[string]bool {
	t.Helper()
	r := u.call(http.MethodGet, "/api/v1/connectors", nil)
	var list []httpapi.Connector
	mustDecode(t, r.Body, &list)
	out := map[string]bool{}
	for _, c := range list {
		out[c.Type] = c.Enabled
	}
	return out
}

// TestSetConnectorEnabled verifies that administrators choose the connectors the instance offers (5.1).
//
// Use cases:
//   - an administrator disables FTP and every user sees it disabled, no FTP site can be created, the change is in the audit log, and
//     enabling it again restores it
//   - a regular account is refused and changes nothing, an anonymous caller too, and an unknown type is not found
func TestSetConnectorEnabled(t *testing.T) {
	e := newAdminEnv(t)
	r := e.root.call(http.MethodPut, "/api/v1/admin/connectors/ftp", map[string]bool{"enabled": false})
	var changed httpapi.Connector
	mustDecode(t, r.Body, &changed)
	if r.Status != http.StatusOK || changed.Type != "ftp" || changed.Enabled {
		t.Fatalf("disable ftp = %d %s", r.Status, r.Body)
	}
	if got := connectorsOf(t, e.other); got["ftp"] || !got["sftp"] {
		t.Errorf("offered after disabling ftp = %v", got)
	}
	trail := e.root.call(http.MethodGet, "/api/v1/admin/audit?action=admin.connector", nil)
	var events []httpapi.AuditEvent
	mustDecode(t, trail.Body, &events)
	if len(events) != 1 || events[0].Action != audit.AdminConnectorChanged || events[0].Target != "ftp" {
		t.Errorf("audit = %+v; want one connector change of ftp", actions(events))
	}
	ws := e.a.personal(e.other.cookie)
	ftpSite := siteBody("Old FTP")
	ftpSite["connector"] = "ftp"
	expectProblem(t, e.other.call(http.MethodPost, sitesPath(ws), ftpSite), http.StatusConflict, "connector_disabled")
	if r := e.root.call(http.MethodPut, "/api/v1/admin/connectors/ftp", map[string]bool{"enabled": true}); r.Status != http.StatusOK {
		t.Fatalf("enable ftp = %d %s", r.Status, r.Body)
	}
	if got := connectorsOf(t, e.other); !got["ftp"] {
		t.Errorf("offered after enabling ftp again = %v", got)
	}

	expectProblem(t, e.other.call(http.MethodPut, "/api/v1/admin/connectors/sftp", map[string]bool{"enabled": false}), http.StatusForbidden, "forbidden")
	if got := connectorsOf(t, e.other); !got["sftp"] {
		t.Error("a refused change disabled sftp")
	}
	if r := e.a.do(request{method: http.MethodPut, path: "/api/v1/admin/connectors/sftp", body: map[string]bool{"enabled": false}}); r.Status != http.StatusUnauthorized {
		t.Errorf("anonymous change = %d; want 401", r.Status)
	}
	expectProblem(t, e.root.call(http.MethodPut, "/api/v1/admin/connectors/s3", map[string]bool{"enabled": true}), http.StatusNotFound, "connector_not_found")
}

// brokenConnectorSettings is a store whose connector settings cannot be read or saved.
type brokenConnectorSettings struct{ *postgres.Store }

// ConnectorSettings always fails.
func (brokenConnectorSettings) ConnectorSettings(context.Context) (map[string]bool, error) {
	return nil, errInjected
}

// SetConnector always fails.
func (brokenConnectorSettings) SetConnector(context.Context, string, bool, uuid.UUID, time.Time) error {
	return errInjected
}

// TestConnectorFailures verifies that a failing store is an internal error when reading or changing the connectors.
//
// Use cases:
//   - listing and changing the connectors answer 500 when the store fails
func TestConnectorFailures(t *testing.T) {
	e := newAdminEnv(t, func(d *httpapi.Deps) {
		d.Connectors = connectors.NewService(connectors.Builtin(), brokenConnectorSettings{d.Readiness.(*postgres.Store)}, realClock{})
	})
	expectProblem(t, e.root.call(http.MethodGet, "/api/v1/connectors", nil), http.StatusInternalServerError, "internal_error")
	expectProblem(t, e.root.call(http.MethodPut, "/api/v1/admin/connectors/ftp", map[string]bool{"enabled": false}), http.StatusInternalServerError, "internal_error")
}

// connectorsFixture is where the interface's tests keep the catalog the server answers, so they draw real schemas.
const connectorsFixture = "../../../../frontend/src/test/connectors.json"

// TestConnectors_FrontendFixture verifies that the catalog the interface's tests use is the one the server answers (ADR-0022).
//
// Use cases:
//   - the fixture equals the answer of GET /api/v1/connectors; with TRANSFERLY_UPDATE_FIXTURES set it is written again instead
func TestConnectors_FrontendFixture(t *testing.T) {
	a := newAPI(t)
	cookie, _ := a.setup()
	r := a.do(request{method: http.MethodGet, path: "/api/v1/connectors", cookie: cookie})
	var catalog any
	mustDecode(t, r.Body, &catalog)
	want, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if os.Getenv("TRANSFERLY_UPDATE_FIXTURES") != "" {
		if err := os.WriteFile(connectorsFixture, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(connectorsFixture)
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("%s is out of date (%v); run the test with TRANSFERLY_UPDATE_FIXTURES=1", connectorsFixture, err)
	}
}
