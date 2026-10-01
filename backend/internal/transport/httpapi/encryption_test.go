package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// TestFTPEncryption covers the FTP encryption of sites and quick connections in the API (ADR-0012).
//
// Use cases:
//   - an FTP site is saved and answered with the encryption it was given, and can change it
//   - a legacy FTPS site is answered as FTP that requires TLS: implicit on port 990
//   - an SFTP site shows no encryption, and an unknown encryption is refused
//   - a quick connection keeps the encryption it was given
func TestFTPEncryption(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)

	body := siteBody("Optional")
	body["protocol"], body["ftpEncryption"] = "ftp", "optional"
	created := a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Status, created.Body)
	}
	optional := created.site(t)
	if optional.Protocol != "ftp" || optional.FtpEncryption == nil || *optional.FtpEncryption != httpapi.Optional || optional.Port != 21 {
		t.Errorf("created = %+v; want ftp with optional encryption on port 21", optional)
	}
	body["ftpEncryption"] = "explicit"
	if updated := a.send(http.MethodPut, sitesPath(ws, optional.Id.String()), body, cookie, csrf).site(t); *updated.FtpEncryption != httpapi.Explicit {
		t.Errorf("updated encryption = %s; want explicit", *updated.FtpEncryption)
	}

	legacy := siteBody("Legacy")
	legacy["protocol"], legacy["port"] = "ftps", 990
	if got := a.send(http.MethodPost, sitesPath(ws), legacy, cookie, csrf).site(t); got.Protocol != "ftp" || *got.FtpEncryption != httpapi.Implicit {
		t.Errorf("legacy ftps = %s %v; want ftp implicit", got.Protocol, got.FtpEncryption)
	}
	if got := a.send(http.MethodPost, sitesPath(ws), siteBody("Secure shell"), cookie, csrf).site(t); got.FtpEncryption != nil {
		t.Errorf("an SFTP site shows the encryption %s", *got.FtpEncryption)
	}
	unknown := siteBody("Unknown")
	unknown["protocol"], unknown["ftpEncryption"] = "ftp", "tls"
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), unknown, cookie, csrf), http.StatusUnprocessableEntity, "invalid_ftp_encryption")

	quickBody := quickBody("8.8.8.8")
	quickBody["protocol"], quickBody["ftpEncryption"] = "ftp", "explicit"
	quick := a.send(http.MethodPost, quickPath(ws), quickBody, cookie, csrf)
	if quick.Status != http.StatusCreated {
		t.Fatalf("quick connection = %d %s", quick.Status, quick.Body)
	}
	if got := quick.quickConnection(t); got.FtpEncryption == nil || *got.FtpEncryption != httpapi.Explicit {
		t.Errorf("quick connection encryption = %v; want explicit", got.FtpEncryption)
	}
}
