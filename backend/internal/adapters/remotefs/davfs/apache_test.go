package davfs_test

import (
	"bytes"
	"crypto/rand"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/remotefs/davfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// apache connects to the folder /webdav/{folder}/ of the Apache httpd named by TRANSFERLY_TEST_WEBDAV_ADDR, as the webdav service
// of the test stacks is, with its test user; it skips the test when the variable is not set.
func apache(t *testing.T, folder string) *davfs.FS {
	t.Helper()
	address := os.Getenv("TRANSFERLY_TEST_WEBDAV_ADDR")
	if address == "" {
		t.Skip("TRANSFERLY_TEST_WEBDAV_ADDR is not set; start the webdav profile of the test stack to run this test")
	}
	fsys, err := davfs.Dial(t.Context(), davfs.Config{
		Endpoint: "http://" + address + "/webdav/" + folder, Username: "transferly", Password: "transferly", Client: &http.Client{Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("Dial(%s) = %v", folder, err)
	}
	t.Cleanup(func() { _ = fsys.Close() })
	return fsys
}

// isolated makes a folder of its own on the shared server, removed when the test ends, and connects to it as the root of a site.
func isolated(t *testing.T) *davfs.FS {
	t.Helper()
	root := apache(t, "")
	folder := uuid.NewString()
	if err := root.Mkdir(t.Context(), "/"+folder); err != nil {
		t.Fatalf("Mkdir(%s) = %v", folder, err)
	}
	t.Cleanup(func() { _ = root.RemoveAll(t.Context(), "/"+folder) })
	return apache(t, folder)
}

// TestConformance_Apache runs the shared suite against Apache httpd with mod_dav, a WebDAV server written apart from the one of
// davtest.
//
// Scenarios:
//   - every test runs in a folder of its own, which is the root of the site, so the server can be shared
func TestConformance_Apache(t *testing.T) {
	remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS { return isolated(t) })
}

// TestApache_LargeFile verifies that a file of several megabytes streams to Apache in one PUT and reads back whole and from an
// offset, through a range.
func TestApache_LargeFile(t *testing.T) {
	fsys := isolated(t)
	data := make([]byte, 5<<20+123)
	_, _ = rand.Read(data)
	w, err := fsys.OpenWrite(t.Context(), "/big.bin", 0, remotefs.WriteCreate)
	if err != nil {
		t.Fatalf("OpenWrite() = %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("Write() = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	r, err := fsys.OpenRead(t.Context(), "/big.bin", 4<<20)
	if err != nil {
		t.Fatalf("OpenRead() = %v", err)
	}
	got, _ := io.ReadAll(r)
	_ = r.Close()
	if !bytes.Equal(got, data[4<<20:]) {
		t.Errorf("read from 4 MiB gave %d bytes, want the %d after the offset", len(got), len(data)-4<<20)
	}
}
