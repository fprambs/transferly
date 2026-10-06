package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// TestGetVersion verifies the version endpoint.
//
// Use cases:
//   - a signed-in person reads the version of the build, dev for builds that do not set it
//   - an anonymous request is refused, so the version is not told to anyone who asks
func TestGetVersion(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, _ := a.setup()
	r := a.do(request{method: http.MethodGet, path: "/api/v1/version", cookie: cookie})
	var got httpapi.Version
	mustDecode(t, r.Body, &got)
	if r.Status != http.StatusOK || got.Version != "dev" {
		t.Errorf("version = %d %+v", r.Status, got)
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/version"}), http.StatusUnauthorized, "unauthenticated")
}
