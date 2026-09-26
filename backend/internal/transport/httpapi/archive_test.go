package httpapi_test

import (
	"archive/zip"
	"bytes"
	"io"
	"mime"
	"net/http"
	"strings"
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

// TestRemoteFiles_Checksum verifies computing the digest of a remote file.
//
// Scenarios:
//   - a file has its SHA-256 by default and the digest of the algorithm asked for, with the size, in lower case
//   - an unknown algorithm is 422 unsupported_algorithm, a folder is 422 invalid_path, a missing file is 404 and anonymous callers 401
func TestRemoteFiles_Checksum(t *testing.T) {
	e := newFileEnv(t)
	a := e.a
	if r := a.upload(e.ws, e.site, q("path", "/hello.txt"), "hello", e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("upload = %d %s", r.Status, r.Body)
	}
	if r := a.send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), map[string]string{"path": "/dir"}, e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("mkdir = %d %s", r.Status, r.Body)
	}
	var sum struct {
		Algorithm string `json:"algorithm"`
		Checksum  string `json:"checksum"`
		Size      int64  `json:"size"`
	}

	r := e.get("/checksum", q("path", "/hello.txt"), nil)
	mustDecode(t, r.Body, &sum)
	if r.Status != http.StatusOK || sum.Algorithm != "sha256" || sum.Checksum != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" || sum.Size != 5 {
		t.Errorf("default checksum = %d %+v", r.Status, sum)
	}
	r = e.get("/checksum", q("path", "/hello.txt", "algorithm", "md5"), nil)
	mustDecode(t, r.Body, &sum)
	if r.Status != http.StatusOK || sum.Algorithm != "md5" || sum.Checksum != "5d41402abc4b2a76b9719d911017c592" {
		t.Errorf("md5 checksum = %d %+v", r.Status, sum)
	}

	expectProblem(t, e.get("/checksum", q("path", "/hello.txt", "algorithm", "crc32"), nil), 422, "unsupported_algorithm")
	expectProblem(t, e.get("/checksum", q("path", "/dir"), nil), 422, "invalid_path")
	expectProblem(t, e.get("/checksum", q("path", "/missing"), nil), 404, "path_not_found")
	expectProblem(t, a.do(request{method: http.MethodGet, path: filesPath(e.ws, e.site, "/checksum", q("path", "/hello.txt"))}), 401, "unauthenticated")
}

// TestRemoteFiles_ArchiveHostileNames verifies that names chosen by a remote server cannot attack the client through a ZIP.
//
// Scenarios:
//   - a folder whose name holds quotes, a semicolon, markup and non-ASCII text comes as an inert attachment whose file name is the folder name and nothing else
//   - the entries inside the ZIP never hold an absolute path or a `..` part, whatever the names below the folder
func TestRemoteFiles_ArchiveHostileNames(t *testing.T) {
	e := newFileEnv(t)
	a := e.a
	folder := `/we"ird;<b>é`
	for _, dir := range []string{folder, folder + "/..dots", folder + "/sub"} {
		if r := a.send(http.MethodPost, filesPath(e.ws, e.site, "/directories", nil), map[string]string{"path": dir}, e.cookie, e.csrf); r.Status != http.StatusCreated {
			t.Fatalf("mkdir %s = %d %s", dir, r.Status, r.Body)
		}
	}
	if r := a.upload(e.ws, e.site, q("path", folder+"/sub/<script>.txt"), "x", e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("upload = %d %s", r.Status, r.Body)
	}

	r := e.get("/archive", q("path", folder, "transferId", uuid.NewString()), nil)
	disposition, params, err := mime.ParseMediaType(r.Header.Get("Content-Disposition"))
	if r.Status != http.StatusOK || err != nil || disposition != "attachment" || params["filename"] != `we"ird;<b>é.zip` {
		t.Fatalf("archive = %d %v %v", r.Status, r.Header, err)
	}
	if r.Header.Get("Content-Type") != "application/zip" || r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers = %v; want an inert ZIP", r.Header)
	}
	reader, err := zip.NewReader(bytes.NewReader(r.Body), int64(len(r.Body)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		for _, part := range strings.Split(file.Name, "/") {
			if part == ".." || strings.HasPrefix(file.Name, "/") || strings.Contains(file.Name, `\`) {
				t.Errorf("entry %q can leave the folder it is extracted to", file.Name)
			}
		}
	}
}
