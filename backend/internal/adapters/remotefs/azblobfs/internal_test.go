package azblobfs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/azure"
	"github.com/fprambs/transferly/internal/adapters/remotefs/azuretest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// dialTest opens the container of a fake.
func dialTest(t *testing.T, s *azuretest.Server) *FS {
	t.Helper()
	fsys, err := Dial(t.Context(), Config{Service: azure.Config{Endpoint: s.Endpoint(), Account: azuretest.Account, Key: azuretest.Key, Client: &http.Client{}}, Container: azuretest.Container})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	t.Cleanup(func() { _ = fsys.Close() })
	return fsys
}

// fixed returns a file system whose service answers every request with handler.
func fixed(t *testing.T, handler http.HandlerFunc) *FS {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := azure.New(azure.Config{Endpoint: server.URL + "/acct", Account: "acct", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return &FS{client: client, container: "c", idle: func() {}}
}

// TestBlocks verifies uploads larger than a block.
//
// Use cases:
//   - with blocks of 4 bytes, a blob of 10 bytes is staged in blocks, committed and reads back whole
//   - a block the service refuses fails the write, and so does the last block or the commit
func TestBlocks(t *testing.T) {
	saved := blockSize
	blockSize = 4
	t.Cleanup(func() { blockSize = saved })
	fsys := dialTest(t, azuretest.StartBlob(t, azuretest.Options{}))
	w, err := fsys.OpenWrite(t.Context(), "/blocks", 0, remotefs.WriteCreate)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "0123456789")
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	r, _ := fsys.OpenRead(t.Context(), "/blocks", 0)
	if got, _ := io.ReadAll(r); string(got) != "0123456789" {
		t.Errorf("read back %q", got)
	}
	_ = r.Close()

	for name, tc := range map[string]struct {
		comp  string
		skip  int
		write string
	}{
		"a block":        {"block", 0, "01234"},
		"the last block": {"block", 1, "012345"},
		"the block list": {"blocklist", 0, "012345"},
	} {
		skip := tc.skip
		refusing := dialTest(t, azuretest.StartBlob(t, azuretest.Options{Fault: func(r *http.Request) *azuretest.Fault {
			if r.URL.Query().Get("comp") != tc.comp {
				return nil
			}
			if skip > 0 {
				skip--
				return nil
			}
			return &azuretest.Fault{Status: http.StatusForbidden, Code: "AuthorizationPermissionMismatch"}
		}}))
		w, _ := refusing.OpenWrite(t.Context(), "/f", 0, remotefs.WriteOverwrite)
		_, writeErr := io.WriteString(w, tc.write)
		closeErr := w.Close()
		if !errors.Is(writeErr, remotefs.ErrPermission) && !errors.Is(closeErr, remotefs.ErrPermission) {
			t.Errorf("refusing %s: write %v, close %v; want ErrPermission", name, writeErr, closeErr)
		}
	}
	closed := dialTest(t, azuretest.StartBlob(t, azuretest.Options{}))
	w, _ = closed.OpenWrite(t.Context(), "/f", 0, remotefs.WriteOverwrite)
	_ = closed.Close()
	if _, err := w.Write([]byte("late")); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Write() after Close() of the file system = %v, want ErrConnectionLost", err)
	}
}

// TestCopies verifies renames whose copy the service reports as pending, or as failed.
//
// Use cases:
//   - a copy reported pending twice is waited for, and the rename completes
//   - a rename whose context ends while it waits fails with the cancellation
//   - a copy that ends other than in success fails the rename
func TestCopies(t *testing.T) {
	saved := copyWait
	copyWait = time.Millisecond
	t.Cleanup(func() { copyWait = saved })
	server := azuretest.StartBlob(t, azuretest.Options{PendingCopies: 2})
	fsys := dialTest(t, server)
	server.Put("f", []byte("content"))
	if err := fsys.Rename(t.Context(), "/f", "/g"); err != nil {
		t.Fatalf("Rename() with pending copies = %v", err)
	}
	copyWait = time.Hour
	server.Put("h", []byte("content"))
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := fsys.Rename(ctx, "/h", "/i"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Rename() that waits past its context = %v, want the deadline", err)
	}
	aborted := fixed(t, func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("x-ms-copy-status", "aborted") })
	if err := aborted.copy(t.Context(), "a", "b"); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("copy() aborted = %v, want ErrConnectionLost", err)
	}
}

// TestListings verifies listings of many pages, blobs that are also folders, unreadable listings and reads cut short.
//
// Use cases:
//   - a listing of pages of one blob lists every blob
//   - a name that is a blob and a folder at once is listed once
//   - a listing that is not XML is a lost connection, and so is content cut short
func TestListings(t *testing.T) {
	saved := pageSize
	pageSize = 1
	t.Cleanup(func() { pageSize = saved })
	server := azuretest.StartBlob(t, azuretest.Options{})
	fsys := dialTest(t, server)
	for _, name := range []string{"a", "a/b", "c", "d"} {
		server.Put(name, []byte("x"))
	}
	entries, err := fsys.List(t.Context(), "/")
	if err != nil || len(entries) != 3 {
		t.Errorf("List(/) = %+v, %v, want a, c and d once each", entries, err)
	}

	garbled := fixed(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "not xml") })
	if _, _, err := garbled.list(t.Context(), "", false, 0); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("list() unreadable = %v, want ErrConnectionLost", err)
	}
	cut := fixed(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, strings.Repeat("x", 3))
		}
	})
	r, err := cut.OpenRead(t.Context(), "/f", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if _, err := io.ReadAll(r); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("reading content cut short = %v, want ErrConnectionLost", err)
	}
}
