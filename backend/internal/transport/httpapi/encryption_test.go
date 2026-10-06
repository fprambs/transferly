package httpapi_test

import (
	"net/http"
	"testing"
)

// TestFTPEncryption covers the FTP encryption of sites and quick connections in the API (ADR-0012).
//
// Use cases:
//   - an FTP site is saved and answered with the encryption it was given and its port, and can change it; implicit TLS defaults to port 990
//   - a new FTP site requires TLS when it names no encryption, an SFTP site has none, and an unknown encryption is refused
//   - a quick connection keeps the encryption it was given
func TestFTPEncryption(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	body := set(siteBody("Optional"), "encryption", "optional")
	body["connector"] = "ftp"
	created := a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Status, created.Body)
	}
	optional := created.site(t)
	if optional.Connector != "ftp" || optional.Config["encryption"] != "optional" || optional.Config["port"] != "21" {
		t.Errorf("created = %+v; want ftp with optional encryption on port 21", optional)
	}
	set(body, "encryption", "implicit")
	if updated := a.send(http.MethodPut, sitesPath(ws, optional.Id.String()), body, cookie, csrf).site(t); updated.Config["encryption"] != "implicit" || updated.Config["port"] != "990" {
		t.Errorf("updated = %+v; want implicit on port 990", updated.Config)
	}

	plain := siteBody("Plain")
	plain["connector"] = "ftp"
	if got := a.send(http.MethodPost, sitesPath(ws), plain, cookie, csrf).site(t); got.Config["encryption"] != "explicit" {
		t.Errorf("a new FTP site = %+v; want explicit TLS", got.Config)
	}
	if got := a.send(http.MethodPost, sitesPath(ws), siteBody("Secure shell"), cookie, csrf).site(t); got.Config["encryption"] != "" {
		t.Errorf("an SFTP site shows the encryption %s", got.Config["encryption"])
	}
	unknown := set(siteBody("Unknown"), "encryption", "tls")
	unknown["connector"] = "ftp"
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), unknown, cookie, csrf), http.StatusUnprocessableEntity, "invalid_config")

	quickBody := set(quickBody("8.8.8.8"), "encryption", "explicit")
	quickBody["connector"] = "ftp"
	quick := a.send(http.MethodPost, quickPath(ws), quickBody, cookie, csrf)
	if quick.Status != http.StatusCreated {
		t.Fatalf("quick connection = %d %s", quick.Status, quick.Body)
	}
	if got := quick.quickConnection(t); got.Config["encryption"] != "explicit" {
		t.Errorf("quick connection encryption = %q; want explicit", got.Config["encryption"])
	}
}
