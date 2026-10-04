package azfilefs

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fprambs/transferly/internal/adapters/remotefs/azure"
	"github.com/fprambs/transferly/internal/adapters/remotefs/azuretest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// fixed returns a file system whose service answers every request with handler.
func fixed(t *testing.T, handler http.HandlerFunc) *FS {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := azure.New(azure.Config{Endpoint: server.URL + "/acct", Account: "acct", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return &FS{client: client, share: "s", idle: func() {}}
}

// TestRangesAndPages verifies uploads of many ranges, listings of many pages, unreadable listings and reads cut short.
//
// Use cases:
//   - with ranges of 4 bytes, a file of 10 bytes is written in three ranges and reads back whole; a closed file system writes no more,
//     and a range the service refuses fails the write
//   - the root stats as a directory
//   - a directory listed in pages of one entry lists every entry
//   - a listing that is not XML is a lost connection, and so is content cut short
func TestRangesAndPages(t *testing.T) {
	savedRange, savedPage := rangeSize, pageSize
	rangeSize, pageSize = 4, 1
	t.Cleanup(func() { rangeSize, pageSize = savedRange, savedPage })
	s := azuretest.StartFiles(t, azuretest.Options{})
	fsys, err := Dial(t.Context(), Config{Service: azure.Config{Endpoint: s.Endpoint(), Account: azuretest.Account, Key: azuretest.Key, Client: &http.Client{}}, Share: azuretest.Share})
	if err != nil {
		t.Fatal(err)
	}
	w, _ := fsys.OpenWrite(t.Context(), "/ranges", 0, remotefs.WriteCreate)
	_, _ = io.WriteString(w, "0123456789")
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	r, _ := fsys.OpenRead(t.Context(), "/ranges", 0)
	if got, _ := io.ReadAll(r); string(got) != "0123456789" {
		t.Errorf("read back %q", got)
	}
	_ = r.Close()
	for _, name := range []string{"/a", "/b"} {
		w, _ := fsys.OpenWrite(t.Context(), name, 0, remotefs.WriteCreate)
		_ = w.Close()
	}
	if entries, err := fsys.List(t.Context(), "/"); err != nil || len(entries) != 3 {
		t.Errorf("List(/) in pages = %+v, %v, want three entries", entries, err)
	}
	w, _ = fsys.OpenWrite(t.Context(), "/late", 0, remotefs.WriteCreate)
	_ = fsys.Close()
	if _, err := w.Write([]byte("late")); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Write() after Close() of the file system = %v, want ErrConnectionLost", err)
	}

	refusing := azuretest.StartFiles(t, azuretest.Options{Fault: func(r *http.Request) *azuretest.Fault {
		if r.URL.Query().Get("comp") == "range" {
			return &azuretest.Fault{Status: http.StatusForbidden, Code: "AuthorizationPermissionMismatch"}
		}
		return nil
	}})
	refused, err := Dial(t.Context(), Config{Service: azure.Config{Endpoint: refusing.Endpoint(), Account: azuretest.Account, Key: azuretest.Key, Client: &http.Client{}}, Share: azuretest.Share})
	if err != nil {
		t.Fatal(err)
	}
	if root, err := refused.Stat(t.Context(), "/"); err != nil || !root.IsDir() {
		t.Errorf("Stat(/) = %+v, %v, want the root directory", root, err)
	}
	w, _ = refused.OpenWrite(t.Context(), "/f", 0, remotefs.WriteCreate)
	if _, err := io.WriteString(w, "0123456789"); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("Write() of a range the service refuses = %v, want ErrPermission", err)
	}

	garbled := fixed(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "not xml") })
	if _, _, err := garbled.children(t.Context(), "/"); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("children() unreadable = %v, want ErrConnectionLost", err)
	}
	cut := fixed(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, strings.Repeat("x", 3))
		}
	})
	r, err = cut.OpenRead(t.Context(), "/f", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if _, err := io.ReadAll(r); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("reading content cut short = %v, want ErrConnectionLost", err)
	}
}
