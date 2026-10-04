package s3fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/minio/minio-go/v7"

	"github.com/fprambs/transferly/internal/adapters/remotefs/s3fs/s3test"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// dialTest connects to the bucket of a test server.
func dialTest(t *testing.T, s *s3test.Server) *FS {
	t.Helper()
	fsys, err := Dial(t.Context(), Config{Endpoint: s.Addr, Secure: true, RootCAs: s.RootCAs, Bucket: s3test.Bucket, AccessKeyID: "key", SecretAccessKey: "secret"})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	t.Cleanup(func() { _ = fsys.Close() })
	return fsys
}

// TestWrap verifies how failures of the service read as the errors of the port.
//
// Use cases:
//   - cancellations stay as they are, and destinations the network guard refused stay refused, not to be tried again
//   - missing keys and buckets are not found, refused keys and signatures are refused credentials, refused access is a permission error
//   - other refusals of the request are invalid, and failures of the service or the network are a lost connection, which may be retried
func TestWrap(t *testing.T) {
	if err := wrap("list", "/", context.Canceled); !errors.Is(err, context.Canceled) || errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("wrap(canceled) = %v, want the cancellation itself", err)
	}
	for code, want := range map[minio.ErrorResponse]error{
		{Code: "NoSuchKey", StatusCode: 404}:             remotefs.ErrNotFound,
		{Code: "NoSuchBucket", StatusCode: 404}:          remotefs.ErrNotFound,
		{Code: "InvalidAccessKeyId", StatusCode: 403}:    remotefs.ErrAuth,
		{Code: "SignatureDoesNotMatch", StatusCode: 403}: remotefs.ErrAuth,
		{Code: "AccessDenied", StatusCode: 403}:          remotefs.ErrPermission,
		{Code: "InvalidRequest", StatusCode: 400}:        remotefs.ErrInvalid,
		{Code: "InternalError", StatusCode: 500}:         remotefs.ErrConnectionLost,
	} {
		if err := wrap("list", "/", code); !errors.Is(err, want) {
			t.Errorf("wrap(%s) = %v, want %v", code.Code, err, want)
		}
	}
	if err := wrap("list", "/", io.ErrUnexpectedEOF); !errors.Is(err, remotefs.ErrConnectionLost) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("wrap(network failure) = %v, want ErrConnectionLost with its cause", err)
	}
	if err := wrap("list", "/", fmt.Errorf("dial: %w", netguard.ErrBlocked)); !errors.Is(err, netguard.ErrBlocked) || remotefs.Retryable(err) {
		t.Errorf("wrap(blocked) = %v, want netguard.ErrBlocked, not retryable", err)
	}
}

// TestCanceledMidway verifies that work cut short by a context that ends between requests reports the cancellation
// instead of a short answer.
//
// Use cases:
//   - a listing whose context ends yields the cancellation instead of looking empty
//   - deleting keys with an ended context reports the cancellation
func TestCanceledMidway(t *testing.T) {
	fsys := dialTest(t, s3test.Start(t, s3test.Options{}))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var last error
	for object := range fsys.objects(ctx, "", false) {
		last = object.Err
	}
	if !errors.Is(last, context.Canceled) {
		t.Errorf("listing with an ended context ended with %v, want context.Canceled", last)
	}
	if err := fsys.remove(ctx, "remove", "/f", []minio.ObjectInfo{{Key: "f"}}); !errors.Is(err, context.Canceled) {
		t.Errorf("remove() with an ended context = %v, want context.Canceled", err)
	}
}

// TestRename_CopiesLargeObjectsInParts verifies that objects too large for one copy request are copied in parts.
//
// Use cases:
//   - with the limit of one copy lowered, a rename copies the object in parts and keeps its content
func TestRename_CopiesLargeObjectsInParts(t *testing.T) {
	saved := copyLimit
	copyLimit = 4
	t.Cleanup(func() { copyLimit = saved })
	var parts int
	server := s3test.Start(t, s3test.Options{Fault: func(r *http.Request) *s3test.Fault {
		if r.URL.Query().Has("partNumber") {
			parts++
		}
		return nil
	}})
	fsys := dialTest(t, server)

	w, err := fsys.OpenWrite(t.Context(), "/big", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "larger than the limit")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fsys.Rename(t.Context(), "/big", "/moved"); err != nil {
		t.Fatalf("Rename() = %v", err)
	}
	r, err := fsys.OpenRead(t.Context(), "/moved", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if got, _ := io.ReadAll(r); string(got) != "larger than the limit" || parts == 0 {
		t.Errorf("moved object = %q after %d part copies, want the content copied in parts", got, parts)
	}
}
