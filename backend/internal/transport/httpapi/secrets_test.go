package httpapi_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/fprambs/transferly/internal/transport/httpapi"

	"github.com/fprambs/transferly/internal/testutil"
)

// lockedBuffer is a log destination that the server goroutines and the test can share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends to the buffer.
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns everything written so far.
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestSecrets_NeverLeave verifies that no secret comes back in a response or reaches the logs.
//
// Scenarios:
//   - the password, private key and passphrase of a site, the password of a quick connection, the master password, the
//     account password, the session token and the anti-CSRF token never appear in any response of the run except where
//     the API hands them out on purpose (the session token in its cookie, the anti-CSRF token in the session)
//   - none of them appears in the logs, at debug level, including for failed and rejected requests that carried them
//   - a site and a quick connection only report which kinds of secret they hold
func TestSecrets_NeverLeave(t *testing.T) {
	logs := &lockedBuffer{}
	a := newAPI(t, func(d *httpapi.Deps) {
		d.Logger = slog.New(slog.NewTextHandler(io.MultiWriter(logs), &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	const (
		sitePassword = "site-password-LEAKCHECK-1"
		sitePhrase   = "site-passphrase-LEAKCHECK-2"
		quickPass    = "quick-password-LEAKCHECK-3"
		wrongMaster  = "wrong-master-LEAKCHECK-4"
	)
	siteKey := testutil.PrivateKey(t, sitePhrase)
	keyBody := strings.Split(siteKey, "\n")[2]
	secrets := []string{sitePassword, keyBody, sitePhrase, quickPass, wrongMaster, masterPassword, adminPassword, cookie.Value, csrf}

	var responses []string
	record := func(r reply) reply {
		responses = append(responses, string(r.Body))
		return r
	}
	if r := record(a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, cookie, csrf)); r.Status != http.StatusCreated {
		t.Fatalf("vault setup = %d %s", r.Status, r.Body)
	}
	responses = responses[:0]

	site := secret(set(siteBody("Leaks"), "auth", "key"), "password", sitePassword, "private_key", siteKey, "passphrase", sitePhrase)
	created := record(a.send(http.MethodPost, sitesPath(ws), site, cookie, csrf))
	if created.Status != http.StatusCreated {
		t.Fatalf("create site = %d %s", created.Status, created.Body)
	}
	id := created.site(t).Id.String()
	site["name"] = "Leaks 2"
	record(a.send(http.MethodPut, sitesPath(ws, id), site, cookie, csrf))
	record(a.do(request{method: http.MethodGet, path: sitesPath(ws, id), cookie: cookie}))
	list := record(a.do(request{method: http.MethodGet, path: sitesPath(ws), cookie: cookie}))
	if !strings.Contains(string(list.Body), `"private_key"`) || !strings.Contains(string(list.Body), `"passphrase"`) {
		t.Errorf("a site should report the kinds of secret it holds: %s", list.Body)
	}

	rejected := secret(set(siteBody("Rejected"), "port", "not a number"), "password", sitePassword)
	record(a.send(http.MethodPost, sitesPath(ws), rejected, cookie, csrf))
	rejected = secret(set(siteBody("Rejected"), "host", "127.0.0.1"), "password", sitePassword)
	record(a.send(http.MethodPost, sitesPath(ws), rejected, cookie, csrf))
	record(a.send(http.MethodPost, sitesPath(ws), "{not json "+sitePassword, cookie, csrf))

	quickIn := secret(quickBody("8.8.8.8"), "password", quickPass)
	record(a.send(http.MethodPost, quickPath(ws), quickIn, cookie, csrf))
	set(quickIn, "host", "127.0.0.1")
	record(a.send(http.MethodPost, quickPath(ws), quickIn, cookie, csrf))
	record(a.do(request{method: http.MethodGet, path: quickPath(ws), cookie: cookie}))

	record(a.vaultCall("/unlock", map[string]string{"masterPassword": wrongMaster}, cookie, csrf))
	record(a.vaultCall("/password", map[string]string{"currentPassword": wrongMaster, "newPassword": wrongMaster + "-new"}, cookie, csrf))
	record(a.do(request{method: http.MethodGet, path: "/api/v1/vault", cookie: cookie}))
	record(a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": adminEmail, "password": wrongMaster}}))

	joined := strings.Join(responses, "\n")
	for _, secret := range secrets {
		if strings.Contains(joined, secret) {
			t.Errorf("a response contains the secret %q", secret[:12]+"…")
		}
		if strings.Contains(logs.String(), secret) {
			t.Errorf("the logs contain the secret %q", secret[:12]+"…")
		}
	}
	if logs.String() == "" {
		t.Error("the logs are empty: the test would not notice a leak")
	}
}
