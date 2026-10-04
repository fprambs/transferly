package azblobfs_test

import (
	"bytes"
	"crypto/rand"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/remotefs/azblobfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/azure"
	"github.com/fprambs/transferly/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// The well-known account of Azurite, the same in every installation of the emulator.
const (
	azuriteAccount = "devstoreaccount1"
	azuriteKey     = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
)

// azurite connects to the container transferly of the Azurite named by TRANSFERLY_TEST_AZURITE_ADDR, as the azurite service of the
// test stack (profile azure) is, with the well-known account; it skips the test when the variable is not set.
func azurite(t *testing.T) *azblobfs.FS {
	t.Helper()
	address := os.Getenv("TRANSFERLY_TEST_AZURITE_ADDR")
	if address == "" {
		t.Skip("TRANSFERLY_TEST_AZURITE_ADDR is not set; start the azure profile of the test stack to run this test")
	}
	fsys, err := azblobfs.Dial(t.Context(), azblobfs.Config{
		Service:   azure.Config{Endpoint: "http://" + address + "/" + azuriteAccount, Account: azuriteAccount, Key: azuriteKey, Client: &http.Client{Timeout: 30 * time.Second}},
		Container: "transferly",
	})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	return fsys
}

// TestConformance_Azurite runs the shared suite against Azurite, which checks the Shared Key signatures independently of the fake.
//
// Scenarios:
//   - every test runs in a folder of its own, removed afterwards, so the emulator can be shared
func TestConformance_Azurite(t *testing.T) {
	remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS {
		fsys := azurite(t)
		root := "/" + uuid.NewString()
		if err := fsys.Mkdir(t.Context(), root); err != nil {
			t.Fatalf("Mkdir(%s) = %v", root, err)
		}
		return remotefstest.Prefixed{Inner: fsys, Prefix: root}
	})
}

// TestAzurite_BlockUpload verifies that a blob larger than one block reaches Azurite whole.
//
// Scenarios:
//   - a blob of 20 MiB, three blocks, is uploaded, read back byte for byte and removed
func TestAzurite_BlockUpload(t *testing.T) {
	fsys := azurite(t)
	defer func() { _ = fsys.Close() }()
	content := make([]byte, 20<<20)
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
	if got, err := io.ReadAll(r); err != nil || !bytes.Equal(got, content) {
		t.Errorf("read back %d bytes, %v; want the %d bytes uploaded", len(got), err, len(content))
	}
}
