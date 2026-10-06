package connect_test

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/testutil"
)

// TestStrictGateway_EndToEnd verifies, against a real SFTP server that reports every file 300 bytes larger than it is, as the
// enterprise gateways that motivated it do, what a person sees when downloading and previewing large files.
//
// Scenarios:
//   - the first download of a file of 1.1 MB, with nothing learned yet, is not announced with the wrong size: it comes whole, with
//     no length, and the server is from then on known as strict, also in the store
//   - a ZIP of 1 MB on it lists its entries
//   - after a restart, with a new connector over the same store, the server is strict from the first connection
func TestStrictGateway_EndToEnd(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}, SizeOff: 300})
	e.trust(server)
	payload := make([]byte, 1_100_000)
	_, _ = rand.Read(payload)
	if err := os.WriteFile(filepath.Join(server.Root, "report.zip"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	entry, _ := w.CreateHeader(&zip.FileHeader{Name: "data.bin", Method: zip.Store})
	_, _ = entry.Write(payload[:1_000_000])
	_ = w.Close()
	if err := os.WriteFile(filepath.Join(server.Root, "bundle.zip"), archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	s := e.addSite(server, spec{Name: "Gateway"}, changes{Password: text(sshPassword)})
	target := files.Target{Actor: e.user, Workspace: e.workspace, Site: s.ID, Session: session}

	service := func() (*files.Service, *connections.Manager) {
		c := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.StrictServers(e.store))
		pool := connections.New(c, testutil.NewFakeClock(time.Now()), connections.Config{})
		t.Cleanup(pool.Close)
		return files.NewService(e.workspaces, pool), pool
	}
	svc, _ := service()
	d, err := svc.Download(t.Context(), target, "/report.zip", files.Range{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(d.Body)
	_ = d.Body.Close()
	if err != nil || !d.Unsized || !bytes.Equal(got, payload) {
		t.Fatalf("first download = unsized %v, %d bytes, %v; want the whole file with no length", d.Unsized, len(got), err)
	}
	if stored, err := e.store.StrictServers(t.Context()); err != nil || len(stored) != 1 || stored[0] != server.Addr {
		t.Errorf("stored strict servers = %v, %v; want %s", stored, err, server.Addr)
	}

	listing, err := svc.ArchiveEntries(t.Context(), target, "/bundle.zip", nil)
	if err != nil || len(listing.Entries) != 1 || listing.Entries[0].Name != "data.bin" {
		t.Errorf("ArchiveEntries() = %+v, %v; want data.bin", listing, err)
	}

	_, restarted := service()
	lease, err := restarted.Acquire(t.Context(), connections.Key{UserID: e.user, WorkspaceID: e.workspace, SiteID: s.ID}, session)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release(nil)
	if remotefs.SizesTrusted(lease.FS()) {
		t.Error("after a restart the strict server was trusted again")
	}
}
