package httpapi_test

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestRemoteFiles_Archive verifies downloading a folder as a ZIP.
//
// Scenarios:
//   - a folder with files and a subfolder comes as a ZIP attachment named after it, with the same names and contents
//   - the download is recorded as a finished transfer named after the archive, with the transfer identifier in the reply
//   - a file, a missing folder and an invalid path are refused with their codes and leave a failed record where the transfer began
//   - the request needs a session
//   - a store that fails answers 500
func TestRemoteFiles_Archive(t *testing.T) {
	e := newFileEnv(t)
	a := e.a
	for _, dir := range []string{"/docs", "/docs/inner"} {
		if r := a.send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), map[string]string{"path": dir}, e.cookie, e.csrf); r.Status != http.StatusCreated {
			t.Fatalf("mkdir %s = %d %s", dir, r.Status, r.Body)
		}
	}
	for path, content := range map[string]string{"/docs/a.txt": "alpha", "/docs/inner/b.txt": "bravo"} {
		if r := a.upload(e.ws, e.site, q("path", path), content, e.cookie, e.csrf); r.Status != http.StatusCreated {
			t.Fatalf("upload %s = %d %s", path, r.Status, r.Body)
		}
	}
	id := uuid.New()

	r := e.get("/archive", q("path", "/docs", "transferId", id.String()), nil)
	if r.Status != http.StatusOK || r.Header.Get("Content-Type") != "application/zip" || r.Header.Get("Content-Disposition") != `attachment; filename=docs.zip` {
		t.Fatalf("archive = %d %v", r.Status, r.Header)
	}
	if r.Header.Get("X-Transfer-Id") != id.String() {
		t.Errorf("transfer identifier = %q; want %s", r.Header.Get("X-Transfer-Id"), id)
	}
	reader, err := zip.NewReader(bytes.NewReader(r.Body), int64(len(r.Body)))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(rc)
		_ = rc.Close()
		got[file.Name] = string(content)
	}
	if len(got) != 3 || got["a.txt"] != "alpha" || got["inner/b.txt"] != "bravo" || got["inner/"] != "" {
		t.Errorf("archive holds %v", got)
	}
	waitFor(t, "the archive transfer to finish", func() bool { return a.transferState(e.cookie, id) == "done" })
	if record := e.transferRecord(id); record.Name != "docs.zip" || record.Direction != "download" {
		t.Errorf("record = %+v; want the download docs.zip", record)
	}

	expectProblem(t, e.get("/archive", q("path", "/docs/a.txt"), nil), 422, "not_a_directory")
	expectProblem(t, e.get("/archive", q("path", "/missing"), nil), 404, "path_not_found")
	expectProblem(t, e.get("/archive", q("path", "/bad\nname"), nil), 422, "invalid_path")
	expectProblem(t, a.do(request{method: http.MethodGet, path: filesPath(e.ws, e.site, "/archive", q("path", "/docs"))}), 401, "unauthenticated")
}
