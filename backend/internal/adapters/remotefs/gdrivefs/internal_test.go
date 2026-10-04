package gdrivefs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"

	"github.com/fprambs/transferly/internal/adapters/remotefs/gdrivefs/googletest"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// TestWrap verifies how failures read as the errors of the port.
//
// Use cases:
//   - cancellations, and errors that already are of the port, stay as they are; destinations the guard refused stay refused
//   - a token Google refuses, and an answer of 401, are refused credentials, so the account signs in again
//   - missing files are not found, a full quota is a quota error, refusals are permission errors, rate limits and failures of Google
//     or of the network are a lost connection, which may be retried, and other refusals of the request are invalid
func TestWrap(t *testing.T) {
	for _, kept := range []error{context.Canceled, fmt.Errorf("%w: x", remotefs.ErrInvalid), fmt.Errorf("%w: x", remotefs.ErrConnectionLost)} {
		if got := wrap("list", "/", kept); !errors.Is(got, kept) || got.Error() != kept.Error() {
			t.Errorf("wrap(%v) = %v, want it unchanged", kept, got)
		}
	}
	if err := wrap("list", "/", fmt.Errorf("dial: %w", netguard.ErrBlocked)); !errors.Is(err, netguard.ErrBlocked) || remotefs.Retryable(err) {
		t.Errorf("wrap(blocked) = %v, want netguard.ErrBlocked, not retryable", err)
	}
	if err := wrap("list", "/", fmt.Errorf("get: %w", &oauth2.RetrieveError{ErrorCode: "invalid_grant"})); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("wrap(refused token) = %v, want ErrAuth", err)
	}
	for answer, want := range map[apiError]error{
		{Status: 401, Reason: "authError"}:               remotefs.ErrAuth,
		{Status: 404, Reason: "notFound"}:                remotefs.ErrNotFound,
		{Status: 403, Reason: "storageQuotaExceeded"}:    remotefs.ErrQuota,
		{Status: 403, Reason: "userRateLimitExceeded"}:   remotefs.ErrConnectionLost,
		{Status: 403, Reason: "rateLimitExceeded"}:       remotefs.ErrConnectionLost,
		{Status: 403, Reason: "insufficientPermissions"}: remotefs.ErrPermission,
		{Status: 400, Reason: "badRequest"}:              remotefs.ErrInvalid,
		{Status: 429, Reason: ""}:                        remotefs.ErrConnectionLost,
		{Status: 503, Reason: "backendError"}:            remotefs.ErrConnectionLost,
	} {
		if err := wrap("list", "/", &answer); !errors.Is(err, want) {
			t.Errorf("wrap(%s) = %v, want %v", answer.Error(), err, want)
		}
	}
	if err := wrap("list", "/", io.ErrUnexpectedEOF); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("wrap(network failure) = %v, want ErrConnectionLost", err)
	}
}

// fixed returns a file system whose API is a server that answers every request with handler.
func fixed(t *testing.T, handler http.HandlerFunc) *FS {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &FS{api: server.URL, client: server.Client(), cache: map[string]file{}}
}

// TestTransport verifies requests that cannot be sent and answers that cannot be read.
//
// Use cases:
//   - a request that cannot be built is invalid, a server that cannot be reached is a lost connection, and so is an answer that is not
//     JSON
//   - a chunk of an upload that Drive does not ask to continue is a lost connection
//   - an upload larger than a chunk is sent in chunks and arrives whole
func TestTransport(t *testing.T) {
	answering := fixed(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "not json") })
	if err := answering.call(t.Context(), "BAD METHOD", "/x", nil, nil, nil); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("call(bad method) = %v, want ErrInvalid", err)
	}
	var out struct{}
	if err := answering.call(t.Context(), http.MethodGet, "/x", nil, nil, &out); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("call(unreadable answer) = %v, want ErrConnectionLost", err)
	}
	gone := &FS{api: "http://127.0.0.1:1", client: http.DefaultClient, cache: map[string]file{}}
	if err := gone.Ping(t.Context()); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Ping(unreachable) = %v, want ErrConnectionLost", err)
	}

	saved := chunkSize
	chunkSize = 4
	t.Cleanup(func() { chunkSize = saved })
	done := fixed(t, func(w http.ResponseWriter, _ *http.Request) {})
	w := &writer{ctx: t.Context(), fs: done, session: done.api, path: "/f"}
	if _, err := w.Write([]byte("12345")); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Write() whose chunk Drive does not continue = %v, want ErrConnectionLost", err)
	}
}

// TestUpload_InChunks verifies that an upload larger than a chunk is sent in chunks and arrives whole.
//
// Use cases:
//   - with chunks of 4 bytes, a file of 10 bytes takes three chunks and reads back as written
func TestUpload_InChunks(t *testing.T) {
	saved := chunkSize
	chunkSize = 4
	t.Cleanup(func() { chunkSize = saved })
	s := googletest.Start(t, googletest.Options{})
	cfg := oauth2.Config{ClientID: googletest.ClientID, ClientSecret: googletest.ClientSecret, Endpoint: oauth2.Endpoint{TokenURL: s.URL + "/token"}}
	fsys, err := Dial(t.Context(), Config{APIURL: s.URL, Client: cfg.Client(context.Background(), &oauth2.Token{RefreshToken: s.RefreshToken()})})
	if err != nil {
		t.Fatal(err)
	}
	w, err := fsys.OpenWrite(t.Context(), "/chunked", 0, remotefs.WriteCreate)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "0123456789")
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	r, err := fsys.OpenRead(t.Context(), "/chunked", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if got, _ := io.ReadAll(r); string(got) != "0123456789" {
		t.Errorf("read back %q, want the bytes written", got)
	}
}

// TestReads_CutShort verifies reads whose answer stops before its end.
//
// Use cases:
//   - an export cut short while skipping to an offset, and content cut short while it is read, are a lost connection
func TestReads_CutShort(t *testing.T) {
	cut := fixed(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "abc")
	})
	cut.cache["/Plan.docx"] = file{ID: "doc", Name: "Plan", MimeType: "application/vnd.google-apps.document"}
	cut.cache["/f"] = file{ID: "bin", Name: "f", MimeType: "text/plain", Size: "100"}
	cut.root = file{ID: "root", MimeType: folderMime}
	if _, err := cut.OpenRead(t.Context(), "/Plan.docx", 10); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("OpenRead() of an export cut short = %v, want ErrConnectionLost", err)
	}
	r, err := cut.OpenRead(t.Context(), "/f", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if _, err := io.ReadAll(r); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("reading content cut short = %v, want ErrConnectionLost", err)
	}
}
