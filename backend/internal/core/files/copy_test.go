package files_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/files"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// otherSite returns the target moved to another site of the same workspace.
func otherSite(t files.Target) files.Target {
	t.Site = uuid.New()
	return t
}

// TestService_Copy verifies copying a file from one site to another through the server.
//
// Scenarios:
//   - the content arrives at the destination, the entry returned is the copy, and the source stays
//   - the destination observer learns the size of the source at the start and counts every byte
//   - the fail policy refuses an existing destination and overwrite replaces it
//   - copying a file onto itself, a missing source and an unusable path are refused
//   - a viewer cannot write the destination and an unknown user cannot read the source
//   - a source that cannot be opened, a source that breaks midway and a destination that fails to commit report the error
//   - a canceled context stops the copy
func TestService_Copy(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.put(t, "/src.txt", "0123456789")
	obs := &recorder{}
	l := files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: "/dst.txt"}
	l.Destination.Observer = obs

	entry, err := f.svc.Copy(ctx, l, files.PolicyFail)
	if err != nil || entry.Size != 10 || entry.Name != "dst.txt" {
		t.Fatalf("Copy() = %+v, %v", entry, err)
	}
	if len(obs.started) != 1 || obs.started[0] != 10 || obs.bytes != 10 {
		t.Errorf("observer saw %v started and %d bytes; want the total of 10 and 10 bytes", obs.started, obs.bytes)
	}
	d, err := f.svc.Download(ctx, f.editor, "/dst.txt", files.Range{})
	if err != nil || readAll(t, d) != "0123456789" {
		t.Errorf("the copy holds other data: %v", err)
	}
	if _, err := f.svc.Stat(ctx, f.editor, "/src.txt"); err != nil {
		t.Errorf("the source is gone: %v", err)
	}

	if _, err := f.svc.Copy(ctx, l, files.PolicyFail); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("Copy() onto an existing file = %v; want ErrExists", err)
	}
	f.put(t, "/src.txt", "new")
	if _, err := f.svc.Copy(ctx, l, files.PolicyOverwrite); err != nil {
		t.Errorf("Copy() overwriting = %v", err)
	}
	if got, _ := f.svc.Stat(ctx, f.editor, "/dst.txt"); got.Size != 3 {
		t.Errorf("the overwritten copy has %d bytes; want 3", got.Size)
	}

	same := files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: f.editor, DestinationPath: "/src.txt"}
	if _, err := f.svc.Copy(ctx, same, files.PolicyOverwrite); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Copy() onto itself = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.Mkdir(ctx, f.editor, "/dir"); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]files.CopyLocation{
		"missing":      {Source: f.editor, SourcePath: "/nothing", Destination: otherSite(f.editor), DestinationPath: "/x"},
		"empty source": {Source: f.editor, SourcePath: "", Destination: otherSite(f.editor), DestinationPath: "/x"},
		"empty target": {Source: f.editor, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: ""},
	} {
		if _, err := f.svc.Copy(ctx, bad, files.PolicyFail); err == nil {
			t.Errorf("Copy() of a %s did not fail", name)
		}
	}

	viewer := files.CopyLocation{Source: f.viewer, SourcePath: "/src.txt", Destination: otherSite(f.viewer), DestinationPath: "/v.txt"}
	if _, err := f.svc.Copy(ctx, viewer, files.PolicyFail); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("Copy() by a viewer = %v; want ErrForbidden", err)
	}
	stranger := files.CopyLocation{Source: files.Target{Actor: uuid.New(), Workspace: f.editor.Workspace, Site: f.editor.Site, Session: "s"}, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: "/s.txt"}
	if _, err := f.svc.Copy(ctx, stranger, files.PolicyFail); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("Copy() by an unknown user = %v; want ErrNotFound", err)
	}

	f.fs.failOpen = true
	unopened := files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: "/unopened.txt"}
	if _, err := f.svc.Copy(ctx, unopened, files.PolicyFail); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Copy() of a source that cannot be opened = %v; want ErrConnectionLost", err)
	}
	f.fs.failOpen = false
	f.fs.failRead = true
	broken := files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: "/broken.txt"}
	if _, err := f.svc.Copy(ctx, broken, files.PolicyFail); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Copy() of a source that breaks = %v; want ErrConnectionLost", err)
	}
	f.fs.failRead, f.fs.failCommit = false, true
	commit := files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: "/commit.txt"}
	if _, err := f.svc.Copy(ctx, commit, files.PolicyFail); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Copy() that fails to commit = %v; want ErrConnectionLost", err)
	}
	f.fs.failCommit = false

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	late := files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: "/late.txt"}
	if _, err := f.svc.Copy(canceled, late, files.PolicyFail); !errors.Is(err, context.Canceled) {
		t.Errorf("Copy() with a canceled context = %v; want context.Canceled", err)
	}
}

// content reads a file of the fixture as text.
func content(t *testing.T, f *fixture, path string) string {
	t.Helper()
	d, err := f.svc.Download(t.Context(), f.editor, path, files.Range{})
	if err != nil {
		t.Fatalf("Download(%s) = %v", path, err)
	}
	return readAll(t, d)
}

// mustMkdir creates folders of the fixture.
func mustMkdir(t *testing.T, f *fixture, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if _, err := f.svc.Mkdir(t.Context(), f.editor, dir); err != nil {
			t.Fatalf("Mkdir(%s) = %v", dir, err)
		}
	}
}

// TestService_CopyTree verifies copying folders and the policies for files that exist.
//
// Scenarios:
//   - a folder with files, subfolders and an empty folder is copied whole, the observer learns that the size is unknown once
//     and counts every byte, and the entry returned is the destination folder
//   - links, devices and unsafe names are left out of the copy
//   - folders that exist are merged into, and a file where a folder should go is refused
//   - the fail policy stops at the first file that exists, overwrite replaces, skip keeps, newer replaces only older files
//   - rename keeps both under a free name, also for names without an extension, and gives up when none is free
//   - a folder cannot be copied into itself, a destination whose parent is missing fails, and trees beyond the limits fail
//   - failing servers are reported wherever they fail: the destination, its listing, the source listing and the free names
func TestService_CopyTree(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	mustMkdir(t, f, "/src", "/src/sub", "/src/empty")
	f.put(t, "/src/a.txt", "A")
	f.put(t, "/src/sub/b.txt", "BB")
	obs := &recorder{}
	l := files.CopyLocation{Source: f.editor, SourcePath: "/src", Destination: otherSite(f.editor), DestinationPath: "/out"}
	l.Destination.Observer = obs

	entry, err := f.svc.Copy(ctx, l, files.PolicyFail)
	if err != nil || !entry.IsDir() || entry.Name != "out" {
		t.Fatalf("Copy() = %+v, %v", entry, err)
	}
	if content(t, f, "/out/a.txt") != "A" || content(t, f, "/out/sub/b.txt") != "BB" {
		t.Error("the copy holds other data")
	}
	if empty, err := f.svc.Stat(ctx, f.editor, "/out/empty"); err != nil || !empty.IsDir() {
		t.Errorf("the empty folder was not copied: %+v, %v", empty, err)
	}
	if len(obs.started) != 1 || obs.started[0] != -1 || obs.bytes != 3 {
		t.Errorf("observer saw %v started and %d bytes; want one unknown size and 3 bytes", obs.started, obs.bytes)
	}

	odd := oddService(t, f)
	mustMkdir(t, f, "/odd")
	f.put(t, "/odd/fine.txt", "ok")
	if _, err := odd.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/odd", Destination: f.editor, DestinationPath: "/odd-out"}, files.PolicyFail); err != nil {
		t.Fatal(err)
	}
	if listing, _ := f.svc.List(ctx, f.editor, "/odd-out"); len(listing) != 1 || listing[0].Name != "fine.txt" {
		t.Errorf("the copy of the odd folder holds %+v; want only fine.txt", listing)
	}

	if _, err := f.svc.Copy(ctx, l, files.PolicyFail); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("Copy() onto existing files = %v; want ErrExists", err)
	}
	f.put(t, "/src/a.txt", "A2")
	if _, err := f.svc.Copy(ctx, l, files.PolicySkip); err != nil || content(t, f, "/out/a.txt") != "A" {
		t.Errorf("skip left %q, %v; want the old file", content(t, f, "/out/a.txt"), err)
	}
	if _, err := f.svc.Copy(ctx, l, files.PolicyNewer); err != nil || content(t, f, "/out/a.txt") != "A" {
		t.Errorf("newer with equal times left %q, %v; want the old file", content(t, f, "/out/a.txt"), err)
	}
	f.clock.Advance(time.Hour)
	f.put(t, "/src/a.txt", "A3")
	if _, err := f.svc.Copy(ctx, l, files.PolicyNewer); err != nil || content(t, f, "/out/a.txt") != "A3" || content(t, f, "/out/sub/b.txt") != "BB" {
		t.Errorf("newer left %q, %v; want the newer file", content(t, f, "/out/a.txt"), err)
	}
	f.put(t, "/src/a.txt", "A4")
	if _, err := f.svc.Copy(ctx, l, files.PolicyOverwrite); err != nil || content(t, f, "/out/a.txt") != "A4" {
		t.Errorf("overwrite left %q, %v", content(t, f, "/out/a.txt"), err)
	}

	if _, err := f.svc.Copy(ctx, l, files.PolicyRename); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Copy(ctx, l, files.PolicyRename); err != nil {
		t.Fatal(err)
	}
	if content(t, f, "/out/a (1).txt") != "A4" || content(t, f, "/out/a (2).txt") != "A4" || content(t, f, "/out/sub/b (1).txt") != "BB" {
		t.Error("rename did not keep both files under free names")
	}
	f.put(t, "/plain", "P")
	mustMkdir(t, f, "/plains")
	single := files.CopyLocation{Source: f.editor, SourcePath: "/plain", Destination: otherSite(f.editor), DestinationPath: "/plains/plain"}
	for i := 0; i < 2; i++ {
		if _, err := f.svc.Copy(ctx, single, files.PolicyRename); err != nil {
			t.Fatal(err)
		}
	}
	if content(t, f, "/plains/plain") != "P" || content(t, f, "/plains/plain (1)") != "P" {
		t.Error("rename of a name without an extension failed")
	}
	mustMkdir(t, f, "/crowded")
	f.put(t, "/crowded/z.txt", "Z")
	for n := 1; n <= 1000; n++ {
		f.put(t, fmt.Sprintf("/crowded/z (%d).txt", n), "Z")
	}
	crowded := files.CopyLocation{Source: f.editor, SourcePath: "/crowded/z.txt", Destination: otherSite(f.editor), DestinationPath: "/crowded/z.txt"}
	crowded.Source.Site = uuid.New()
	if _, err := f.svc.Copy(ctx, crowded, files.PolicyRename); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("Copy() with no free name = %v; want ErrExists", err)
	}

	f.put(t, "/afile", "x")
	onFile := files.CopyLocation{Source: f.editor, SourcePath: "/src", Destination: otherSite(f.editor), DestinationPath: "/afile"}
	if _, err := f.svc.Copy(ctx, onFile, files.PolicyOverwrite); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("Copy() of a folder onto a file = %v; want ErrExists", err)
	}
	inside := files.CopyLocation{Source: f.editor, SourcePath: "/src", Destination: f.editor, DestinationPath: "/src/inner"}
	if _, err := f.svc.Copy(ctx, inside, files.PolicyFail); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Copy() of a folder into itself = %v; want ErrInvalid", err)
	}
	orphan := files.CopyLocation{Source: f.editor, SourcePath: "/src", Destination: otherSite(f.editor), DestinationPath: "/nope/dst"}
	if _, err := f.svc.Copy(ctx, orphan, files.PolicyFail); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Copy() below a missing folder = %v; want ErrNotFound", err)
	}

	limited := oddService(t, f).WithTreeLimits(1, 100)
	mustMkdir(t, f, "/n1", "/n1/n2", "/n1/n2/n3")
	f.put(t, "/n1/n2/n3/f.txt", "deep")
	if _, err := limited.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/n1", Destination: f.editor, DestinationPath: "/n1-out"}, files.PolicyFail); !errors.Is(err, files.ErrTreeTooLarge) {
		t.Errorf("Copy() of a tree that is too deep = %v; want ErrTreeTooLarge", err)
	}
	crowdedTree := oddService(t, f).WithTreeLimits(10, 2)
	if _, err := crowdedTree.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/src", Destination: f.editor, DestinationPath: "/src-out"}, files.PolicyFail); !errors.Is(err, files.ErrTreeTooLarge) {
		t.Errorf("Copy() of a tree with too many entries = %v; want ErrTreeTooLarge", err)
	}

	for name, bad := range map[string]struct {
		location files.CopyLocation
		policy   files.Policy
	}{
		"destination folder": {files.CopyLocation{Source: f.editor, SourcePath: "/src", Destination: f.editor, DestinationPath: "/statfail-folder"}, files.PolicyFail},
		"destination file":   {files.CopyLocation{Source: f.editor, SourcePath: "/src/a.txt", Destination: f.editor, DestinationPath: "/statfail-file"}, files.PolicyOverwrite},
		"source listing":     {files.CopyLocation{Source: f.editor, SourcePath: "/listfail", Destination: f.editor, DestinationPath: "/listed"}, files.PolicyFail},
		"free names":         {files.CopyLocation{Source: f.editor, SourcePath: "/src/a.txt", Destination: f.editor, DestinationPath: "/renamefail/a.txt"}, files.PolicyRename},
	} {
		if _, err := odd.Copy(ctx, bad.location, bad.policy); !errors.Is(err, remotefs.ErrConnectionLost) {
			t.Errorf("Copy() with a failing %s = %v; want ErrConnectionLost", name, err)
		}
	}
}
