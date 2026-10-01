package httpapi_test

import (
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// archiveSample reads an archive of the preview samples.
func archiveSample(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../../../testenv/preview-samples/archives", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestArchiveEntries verifies listing and reading the entries of remote archives over HTTP (D2.5).
//
// Use cases:
//   - a ZIP lists its entries with their types, sizes and times, and an entry downloads as an inert attachment with its length
//   - a compressed single file lists one entry with no time, which downloads without a length
//   - an archive nested in another is listed and read through the inner chain
//   - an encrypted 7z entry, found only once decoded, is a 422 problem instead of a broken download
//   - a missing entry is 404, a file that is no archive, a bomb and a chain deeper than four levels are 422
func TestArchiveEntries(t *testing.T) {
	e := newFileEnv(t)
	for _, name := range []string{"project.zip", "server.log.gz", "secret-entries.7z", "bomb.zip"} {
		if r := e.a.upload(e.ws, e.site, q("path", "/"+name), archiveSample(t, name), e.cookie, e.csrf); r.Status != http.StatusCreated {
			t.Fatalf("upload %s = %d", name, r.Status)
		}
	}
	e.a.upload(e.ws, e.site, q("path", "/plain.txt"), "not an archive", e.cookie, e.csrf)

	var listing httpapi.ArchiveListing
	r := e.get("/archive-entries", q("path", "/project.zip"), nil)
	mustDecode(t, r.Body, &listing)
	if r.Status != http.StatusOK || listing.Format != "zip" || listing.Truncated || len(listing.Entries) != 13 {
		t.Fatalf("listing = %d %+v", r.Status, listing)
	}
	if first := listing.Entries[0]; first.Name != "project/" || first.Type != "directory" || first.Modified == nil {
		t.Errorf("first entry = %+v", first)
	}
	readme := e.get("/archive-entry", q("path", "/project.zip", "entry", "project/README.md"), nil)
	disposition, params, _ := mime.ParseMediaType(readme.Header.Get("Content-Disposition"))
	if readme.Status != http.StatusOK || disposition != "attachment" || params["filename"] != "README.md" || readme.Header.Get("Content-Length") != "581" ||
		readme.Header.Get("Content-Type") != "application/octet-stream" || len(readme.Body) != 581 {
		t.Errorf("entry = %d %v %d bytes", readme.Status, readme.Header, len(readme.Body))
	}

	var single httpapi.ArchiveListing
	mustDecode(t, e.get("/archive-entries", q("path", "/server.log.gz"), nil).Body, &single)
	if single.Format != "gz" || len(single.Entries) != 1 || single.Entries[0].Modified != nil || single.Entries[0].Size != -1 {
		t.Errorf("single = %+v", single)
	}
	if log := e.get("/archive-entry", q("path", "/server.log.gz", "entry", "server.log"), nil); log.Status != http.StatusOK || log.Header.Get("Content-Length") != "" || len(log.Body) == 0 {
		t.Errorf("single entry = %d %v", log.Status, log.Header)
	}

	nested := url.Values{"path": {"/project.zip"}, "inner": {"project/vendor/inner.zip"}}
	var inner httpapi.ArchiveListing
	mustDecode(t, e.get("/archive-entries", nested, nil).Body, &inner)
	if len(inner.Entries) != 2 || inner.Entries[0].Name != "notes/plain.txt" {
		t.Errorf("nested = %+v", inner)
	}
	nested.Set("entry", "notes/plain.txt")
	if r := e.get("/archive-entry", nested, nil); r.Status != http.StatusOK || len(r.Body) == 0 {
		t.Errorf("nested entry = %d", r.Status)
	}

	expectProblem(t, e.get("/archive-entry", q("path", "/secret-entries.7z", "entry", "project/README.md"), nil), 422, "archive_encrypted")
	expectProblem(t, e.get("/archive-entry", q("path", "/project.zip", "entry", "none"), nil), 404, "archive_entry_not_found")
	expectProblem(t, e.get("/archive-entries", q("path", "/plain.txt"), nil), 422, "archive_unsupported")
	expectProblem(t, e.get("/archive-entry", q("path", "/bomb.zip", "entry", "zeros.bin"), nil), 422, "archive_bomb")
	deep := url.Values{"path": {"/project.zip"}, "inner": {"a", "b", "c", "d"}}
	expectProblem(t, e.get("/archive-entries", deep, nil), 404, "archive_entry_not_found")

}
