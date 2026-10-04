package azure_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fprambs/transferly/internal/adapters/remotefs/azure"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// TestEndpoint verifies the base URLs of the services of an account.
//
// Use cases:
//   - without a host, the endpoint is that of Azure for the service
//   - a host that starts with the account is an account host, as in sovereign clouds, and other hosts are emulators that take the
//     account in the path; the port is left out when it is that of the scheme
func TestEndpoint(t *testing.T) {
	for _, tc := range []struct {
		service, host string
		port          int
		secure        bool
		want          string
	}{
		{"blob", "", 443, true, "https://acct.blob.core.windows.net"},
		{"file", "", 443, true, "https://acct.file.core.windows.net"},
		{"blob", "acct.blob.core.chinacloudapi.cn", 443, true, "https://acct.blob.core.chinacloudapi.cn"},
		{"blob", "azurite", 10000, false, "http://azurite:10000/acct"},
		{"blob", "storage.local", 80, false, "http://storage.local/acct"},
		{"file", "gateway", 8443, true, "https://gateway:8443/acct"},
	} {
		if got := azure.Endpoint(tc.service, "acct", tc.host, tc.port, tc.secure); got != tc.want {
			t.Errorf("Endpoint(%s, %s:%d) = %s, want %s", tc.service, tc.host, tc.port, got, tc.want)
		}
	}
}

// send sends a request for the resource c and closes its answer, returning its failure.
func send(t *testing.T, c *azure.Client, method string, body []byte) error {
	resp, err := c.Do(t.Context(), method, "c", nil, nil, body)
	if resp != nil {
		_ = resp.Body.Close()
	}
	return err
}

// TestClient verifies building the client and the URLs of resources.
//
// Use cases:
//   - an endpoint that is not a URL, a key that is not base64 and a SAS token that is not a query string are invalid
//   - without a key, the SAS token goes in the query of every URL; with a key it does not
//   - a request that cannot be built is invalid, and a server that cannot be reached fails as the network does
//   - an error answered without the code header is read from its XML body
func TestClient(t *testing.T) {
	for name, cfg := range map[string]azure.Config{
		"endpoint": {Endpoint: "::"},
		"key":      {Endpoint: "http://h", Key: "not base64!"},
		"sas":      {Endpoint: "http://h", SAS: "a=%zz"},
	} {
		if _, err := azure.New(cfg); !errors.Is(err, remotefs.ErrInvalid) {
			t.Errorf("New(bad %s) = %v, want ErrInvalid", name, err)
		}
	}
	shared, _ := azure.New(azure.Config{Endpoint: "https://acct.blob.core.windows.net/", Account: "acct", SAS: "?sv=1&sig=s"})
	if got := shared.URL("c/a b.txt", nil); !strings.HasPrefix(got, "https://acct.blob.core.windows.net/c/a%20b.txt?") || !strings.Contains(got, "sig=s") {
		t.Errorf("URL() with a SAS token = %s", got)
	}
	keyed, _ := azure.New(azure.Config{Endpoint: "https://acct.blob.core.windows.net", Account: "acct", Key: "a2V5", SAS: "sig=s"})
	if got := keyed.URL("c", map[string][]string{"restype": {"container"}}); got != "https://acct.blob.core.windows.net/c?restype=container" {
		t.Errorf("URL() with a key = %s", got)
	}

	if err := send(t, keyed, "BAD METHOD", nil); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Do(bad method) = %v, want ErrInvalid", err)
	}
	gone, _ := azure.New(azure.Config{Endpoint: "http://127.0.0.1:1", Account: "acct", Client: http.DefaultClient})
	if err := send(t, gone, http.MethodGet, nil); err == nil || azure.Code(err) != "" {
		t.Errorf("Do(unreachable) = %v, want a network failure", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><Error><Code>ContainerNotFound</Code></Error>`)
	}))
	t.Cleanup(server.Close)
	bodied, _ := azure.New(azure.Config{Endpoint: server.URL, Account: "acct", Client: server.Client()})
	err := send(t, bodied, http.MethodGet, []byte("x"))
	if azure.Code(err) != "ContainerNotFound" || err.Error() != "azure answered 404 ContainerNotFound" {
		t.Errorf("Do() answered with a body = %v", err)
	}
}

// TestWrap verifies how failures read as the errors of the port.
//
// Use cases:
//   - cancellations and errors that already are invalid stay as they are; destinations the guard refused stay refused
//   - each family of codes of Azure Storage reads as its error, and answers without a code read by their status
//   - other refusals are invalid, and failures of the service or the network are a lost connection, which may be retried
func TestWrap(t *testing.T) {
	for _, kept := range []error{context.Canceled, fmt.Errorf("%w: x", remotefs.ErrInvalid)} {
		if got := azure.Wrap("list", "/", kept); !errors.Is(got, kept) || got.Error() != kept.Error() {
			t.Errorf("Wrap(%v) = %v, want it unchanged", kept, got)
		}
	}
	if err := azure.Wrap("list", "/", fmt.Errorf("dial: %w", netguard.ErrBlocked)); !errors.Is(err, netguard.ErrBlocked) || remotefs.Retryable(err) {
		t.Errorf("Wrap(blocked) = %v, want netguard.ErrBlocked, not retryable", err)
	}
	for answer, want := range map[azure.Error]error{
		{Status: 404, Code: "BlobNotFound"}:                    remotefs.ErrNotFound,
		{Status: 404, Code: "ParentNotFound"}:                  remotefs.ErrNotFound,
		{Status: 404}:                                          remotefs.ErrNotFound,
		{Status: 403, Code: "AuthenticationFailed"}:            remotefs.ErrAuth,
		{Status: 403, Code: "AuthorizationPermissionMismatch"}: remotefs.ErrPermission,
		{Status: 403}:                                          remotefs.ErrPermission,
		{Status: 409, Code: "ResourceAlreadyExists"}:           remotefs.ErrExists,
		{Status: 412, Code: "ConditionNotMet"}:                 remotefs.ErrExists,
		{Status: 409, Code: "DirectoryNotEmpty"}:               remotefs.ErrNotEmpty,
		{Status: 413, Code: "ShareSizeLimitReached"}:           remotefs.ErrQuota,
		{Status: 400, Code: "InvalidHeaderValue"}:              remotefs.ErrInvalid,
		{Status: 429}:                                          remotefs.ErrConnectionLost,
		{Status: 503, Code: "ServerBusy"}:                      remotefs.ErrConnectionLost,
	} {
		if err := azure.Wrap("list", "/", &answer); !errors.Is(err, want) {
			t.Errorf("Wrap(%d %s) = %v, want %v", answer.Status, answer.Code, err, want)
		}
	}
	if err := azure.Wrap("list", "/", io.ErrUnexpectedEOF); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Wrap(network failure) = %v, want ErrConnectionLost", err)
	}
}

// TestSign verifies the Shared Key signature against one computed apart, from the string to sign the documentation of Azure
// Storage gives: the standard headers in their order, the x-ms headers sorted, the resource and its query sorted.
//
// Use cases:
//   - a PUT with a body, a content type, If-None-Match, x-ms headers out of order and a query with two values signs as expected
func TestSign(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "https://acct.blob.core.windows.net/c/a%20b.txt?id=y&comp=block&id=x", strings.NewReader("abc"))
	req.Header.Set("x-ms-version", "2023-11-03")
	req.Header.Set("x-ms-date", "Sun, 04 Oct 2026 12:00:00 GMT")
	req.Header.Set("X-Ms-Blob-Type", "BlockBlob")
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("If-None-Match", "*")
	if got := azure.Sign(req, "acct", []byte("transferly-azure-test-account-key")); got != "6spsrW8XuwEzS0IXDjKWPZ9r/BzDLSGHHggMf4cr5Ck=" {
		t.Errorf("Sign() = %s", got)
	}
}
