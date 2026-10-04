package s3fs_test

import (
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/s3fs"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// gateway connects to the S3 server named by TRANSFERLY_TEST_S3_ADDR, as the s3 service of the test stack (profile s3) is:
// plain HTTP, the bucket transferly and its root access key. It skips the test when the variable is not set.
func gateway(t *testing.T) *s3fs.FS {
	t.Helper()
	address := os.Getenv("TRANSFERLY_TEST_S3_ADDR")
	if address == "" {
		t.Skip("TRANSFERLY_TEST_S3_ADDR is not set; start the s3 profile of the test stack to run this test")
	}
	fsys, err := s3fs.Dial(t.Context(), s3fs.Config{
		Endpoint: address, Bucket: "transferly", AccessKeyID: "transferly", SecretAccessKey: "transferly-secret", Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	return fsys
}

// TestConformance_Gateway runs the shared suite against a real S3 server, to catch answers gofakes3 does not imitate, in
// particular uploads over plain HTTP, which are signed in chunks.
//
// Scenarios:
//   - every test runs in a folder of its own, removed afterwards, so the server can be shared
func TestConformance_Gateway(t *testing.T) {
	remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS {
		fsys := gateway(t)
		root := "/" + uuid.NewString()
		if err := fsys.Mkdir(t.Context(), root); err != nil {
			t.Fatalf("Mkdir(%s) = %v", root, err)
		}
		return remotefstest.Prefixed{Inner: fsys, Prefix: root}
	})
}

// TestGateway_MultipartUpload verifies that a file larger than one part reaches a real server whole.
//
// Scenarios:
//   - a file of 40 MiB, three parts, is uploaded, read back byte for byte and removed
func TestGateway_MultipartUpload(t *testing.T) {
	fsys := gateway(t)
	defer func() { _ = fsys.Close() }()
	content := make([]byte, 40<<20)
	_, _ = rand.Read(content)
	name := "/" + uuid.NewString() + ".bin"
	defer func() { _ = fsys.Remove(t.Context(), name) }()

	w, err := fsys.OpenWrite(t.Context(), name, 0, remotefs.WriteCreate)
	if err != nil {
		t.Fatalf("OpenWrite() = %v", err)
	}
	if _, err := io.Copy(w, bytes.NewReader(content)); err != nil {
		t.Fatalf("write = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	r, err := fsys.OpenRead(t.Context(), name, 0)
	if err != nil {
		t.Fatalf("OpenRead() = %v", err)
	}
	defer func() { _ = r.Close() }()
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, content) {
		t.Errorf("read back %d bytes, %v; want the %d bytes uploaded", len(got), err, len(content))
	}
}
