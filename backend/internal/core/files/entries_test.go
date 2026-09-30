package files_test

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/archives"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// zipOf builds a ZIP in memory with the given files, stored uncompressed so their bytes stay as large as they are.
func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	for name, data := range files {
		f, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write(data)
	}
	_ = w.Close()
	return buffer.Bytes()
}

// gzipOf compresses bytes with gzip.
func gzipOf(data []byte) []byte {
	var buffer bytes.Buffer
	w := gzip.NewWriter(&buffer)
	_, _ = w.Write(data)
	_ = w.Close()
	return buffer.Bytes()
}

// readEntryBody reads an entry and closes it twice, as a handler that closes on every path does.
func readEntryBody(t *testing.T, entry io.ReadCloser) []byte {
	t.Helper()
	data, err := io.ReadAll(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := entry.Close(); err != nil {
		t.Fatal(err)
	}
	_ = entry.Close()
	return data
}

// TestService_ArchiveEntries verifies that archives on the server are listed and their entries read, nested ones included.
//
// Use cases:
//   - a viewer lists a remote ZIP and reads an entry whole, larger than the cache, and the connection goes back to the pool
//   - an archive nested in another, and one nested in that, are listed and read through the chain of their names
//   - reads past the end of the file end there, and a read that went back to the index reopens the file where it needs
//   - a folder, a missing file or entry, an unknown user and a chain deeper than the cap are refused
//   - a nested archive over the cap, declared or found only while reading, is too large, and a nested entry that fails to read fails
//   - a file that cannot be opened or read fails and its connection is discarded, and a canceled request stops reading
func TestService_ArchiveEntries(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	big := make([]byte, 5<<20)
	for i := range big {
		big[i] = byte(rand.IntN(256))
	}
	deepest := zipOf(t, map[string][]byte{"note.txt": []byte("deep inside")})
	middle := zipOf(t, map[string][]byte{"deepest.zip": deepest, "noise.bin": big[:1000]})
	outer := zipOf(t, map[string][]byte{"big.bin": big, "nested/middle.zip": middle, "hello.txt": []byte("hello")})
	f.put(t, "/outer.zip", string(outer))
	if _, err := f.svc.Mkdir(ctx, f.editor, "/folder"); err != nil {
		t.Fatal(err)
	}

	listing, err := f.svc.ArchiveEntries(ctx, f.viewer, "/outer.zip", nil)
	if err != nil || listing.Format != archives.Zip || len(listing.Entries) != 3 {
		t.Fatalf("ArchiveEntries() = %+v, %v", listing, err)
	}
	entry, err := f.svc.OpenArchiveEntry(ctx, f.viewer, "/outer.zip", nil, "big.bin")
	if err != nil || entry.Name != "big.bin" || entry.Size != int64(len(big)) {
		t.Fatalf("OpenArchiveEntry() = %+v, %v", entry, err)
	}
	if !bytes.Equal(readEntryBody(t, entry.Body), big) {
		t.Error("the entry read differs from the file stored")
	}
	if f.idle(f.viewer) != 1 {
		t.Errorf("idle connections = %d; want the connection back", f.idle(f.viewer))
	}
	chain := []string{"nested/middle.zip", "deepest.zip"}
	if listing, err := f.svc.ArchiveEntries(ctx, f.viewer, "/outer.zip", chain); err != nil || listing.Entries[0].Name != "note.txt" {
		t.Errorf("nested listing = %+v, %v", listing, err)
	}
	nested, err := f.svc.OpenArchiveEntry(ctx, f.viewer, "/outer.zip", chain, "note.txt")
	if err != nil || string(readEntryBody(t, nested.Body)) != "deep inside" {
		t.Errorf("nested entry: %v", err)
	}

	failures := map[string]struct {
		path  string
		inner []string
		entry string
		want  error
	}{
		"folder":      {"/folder", nil, "x", remotefs.ErrInvalid},
		"missing":     {"/none.zip", nil, "x", remotefs.ErrNotFound},
		"no entry":    {"/outer.zip", nil, "none", archives.ErrEntryNotFound},
		"no nested":   {"/outer.zip", []string{"none.zip"}, "x", archives.ErrEntryNotFound},
		"too deep":    {"/outer.zip", []string{"a", "b", "c", "d", "e"}, "x", archives.ErrTooLarge},
		"bad name":    {"/outer.zip", nil, "a\x00b", remotefs.ErrInvalid},
		"not archive": {"/outer.zip", []string{"hello.txt"}, "x", archives.ErrNotArchive},
	}
	for name, c := range failures {
		if _, err := f.svc.OpenArchiveEntry(ctx, f.viewer, c.path, c.inner, c.entry); !errors.Is(err, c.want) {
			t.Errorf("%s: OpenArchiveEntry() = %v; want %v", name, err, c.want)
		}
	}
	if _, err := f.svc.ArchiveEntries(ctx, f.viewer, "/none.zip", nil); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("ArchiveEntries() of a missing file = %v", err)
	}
	stranger := f.viewer
	stranger.Actor = uuid.New()
	if _, err := f.svc.ArchiveEntries(ctx, stranger, "/outer.zip", nil); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger: %v", err)
	}

	if _, err := oddService(t, f).WithNestedLimit(1024).ArchiveEntries(ctx, f.viewer, "/outer.zip", []string{"nested/middle.zip"}); !errors.Is(err, archives.ErrTooLarge) {
		t.Errorf("declared too large: %v", err)
	}
	f.put(t, "/middle.zip.gz", string(gzipOf(middle)))
	if _, err := oddService(t, f).WithNestedLimit(1024).ArchiveEntries(ctx, f.viewer, "/middle.zip.gz", []string{"middle.zip"}); !errors.Is(err, archives.ErrTooLarge) {
		t.Errorf("found too large while reading: %v", err)
	}
	secret, err := os.ReadFile("../../../../testenv/preview-samples/archives/secret-entries.7z")
	if err != nil {
		t.Fatal(err)
	}
	f.put(t, "/secret.7z", string(secret))
	if _, err := f.svc.ArchiveEntries(ctx, f.viewer, "/secret.7z", []string{"project/README.md"}); !errors.Is(err, archives.ErrEncrypted) {
		t.Errorf("encrypted nested: %v", err)
	}

	for _, broken := range []*bool{&f.fs.failOpen, &f.fs.failRead} {
		*broken = true
		if _, err := f.svc.ArchiveEntries(ctx, f.viewer, "/outer.zip", nil); !errors.Is(err, remotefs.ErrConnectionLost) {
			t.Errorf("failed storage: %v", err)
		}
		*broken = false
	}
	canceled, cancel := context.WithCancel(ctx)
	entry, err = f.svc.OpenArchiveEntry(canceled, f.viewer, "/outer.zip", nil, "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := io.ReadAll(entry.Body); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled read = %v", err)
	}
	_ = entry.Body.Close()
}
