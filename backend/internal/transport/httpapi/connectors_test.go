package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

// TestListConnectors verifies the catalog of connectors over HTTP (5.1).
//
// Use cases:
//   - a signed-in user gets the built-in connectors with their capabilities, settings with defaults, bounds and choices, and ways of
//     authenticating; settings without them leave them out
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
	if !sftp.EnabledByDefault || sftp.Maturity != "stable" || !sftp.Capabilities.Resume || sftp.Capabilities.ServerCopy || len(sftp.Auth) != 4 {
		t.Errorf("sftp = %+v", sftp)
	}
	host, port := sftp.Fields[0], sftp.Fields[1]
	if host.Name != "host" || !host.Required || host.Default != nil || *host.MaxLength != 255 || host.Choices != nil {
		t.Errorf("host = %+v", host)
	}
	if *port.Default != "22" || *port.Min != 1 || *port.Max != 65535 {
		t.Errorf("port = %+v", port)
	}
	if encryption := list[0].Fields[3]; len(*encryption.Choices) != 4 {
		t.Errorf("encryption = %+v", encryption)
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/connectors"}), http.StatusUnauthorized, "unauthenticated")
}
