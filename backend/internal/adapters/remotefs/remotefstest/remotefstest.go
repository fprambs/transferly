// Package remotefstest is the conformance suite every [remotefs.RemoteFS] adapter must pass.
package remotefstest

import (
	"context"
	"errors"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// Factory returns a new, empty file system for one test. The suite closes it when the test ends.
type Factory func(t *testing.T) remotefs.RemoteFS

// Run executes the conformance suite against adapters built by newFS.
//
// Scenarios:
//   - listing and stat of files, directories and missing paths
//   - creating directories, with existing and missing parents
//   - writing and reading back, from offsets, in create, overwrite and resume modes
//   - removing files, empty directories and whole trees
//   - renaming files and directories, and refusing to overwrite
//   - permissions and modification times, when the adapter advertises them
//   - canceled contexts, ping and use after close
func Run(t *testing.T, newFS Factory) {
	t.Helper()
	tests := []struct {
		name string
		run  func(t *testing.T, fsys remotefs.RemoteFS)
	}{
		{"list an empty root", testListEmpty},
		{"list returns entries with sizes and types", testListEntries},
		{"list reports missing directories and files", testListErrors},
		{"stat describes files and reports missing paths", testStat},
		{"mkdir creates directories", testMkdir},
		{"write then read back", testWriteRead},
		{"create mode refuses to overwrite and overwrite truncates", testWriteModes},
		{"resume writes from an offset", testResume},
		{"writes need an existing directory", testWriteParent},
		{"remove deletes files and empty directories", testRemove},
		{"remove all deletes trees and ignores missing paths", testRemoveAll},
		{"rename moves files and directories", testRename},
		{"chmod and chtimes update the entry", testMetadata},
		{"modification times are UTC", testUTC},
		{"canceled contexts are honored", testCanceled},
		{"ping works and closed file systems report a lost connection", testLifecycle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := newFS(t)
			t.Cleanup(func() { _ = fsys.Close() })
			tt.run(t, fsys)
		})
	}
}

// put writes content to path in overwrite mode.
func put(t *testing.T, fsys remotefs.RemoteFS, path, content string) {
	t.Helper()
	w, err := fsys.OpenWrite(t.Context(), path, 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatalf("OpenWrite(%s) = %v", path, err)
	}
	if _, err := io.WriteString(w, content); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}

// get reads path from offset to the end.
func get(t *testing.T, fsys remotefs.RemoteFS, path string, offset int64) string {
	t.Helper()
	r, err := fsys.OpenRead(t.Context(), path, offset)
	if err != nil {
		t.Fatalf("OpenRead(%s, %d) = %v", path, offset, err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// mkdir creates a directory and fails the test on error.
func mkdir(t *testing.T, fsys remotefs.RemoteFS, path string) {
	t.Helper()
	if err := fsys.Mkdir(t.Context(), path); err != nil {
		t.Fatalf("Mkdir(%s) = %v", path, err)
	}
}

// wantErr fails the test unless err matches target.
func wantErr(t *testing.T, what string, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("%s = %v, want %v", what, err, target)
	}
}

// names returns the sorted entry names.
func names(entries []remotefs.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	sort.Strings(out)
	return out
}

// testListEmpty checks that a new file system has an empty root.
func testListEmpty(t *testing.T, fsys remotefs.RemoteFS) {
	entries, err := fsys.List(t.Context(), "/")
	if err != nil || len(entries) != 0 {
		t.Errorf("List(/) = %v, %v, want no entries", entries, err)
	}
}

// testListEntries checks names, sizes and types of listed entries.
func testListEntries(t *testing.T, fsys remotefs.RemoteFS) {
	mkdir(t, fsys, "/docs")
	put(t, fsys, "/a.txt", "hello")
	put(t, fsys, "/docs/b.txt", "x")

	entries, err := fsys.List(t.Context(), "/")
	if err != nil {
		t.Fatalf("List(/) = %v", err)
	}
	if got := names(entries); len(got) != 2 || got[0] != "a.txt" || got[1] != "docs" {
		t.Fatalf("names = %v, want [a.txt docs]", got)
	}
	for _, e := range entries {
		switch e.Name {
		case "a.txt":
			if e.IsDir() || e.Size != 5 || e.Path != "/a.txt" {
				t.Errorf("a.txt entry = %+v, want a 5 byte file at /a.txt", e)
			}
		case "docs":
			if !e.IsDir() {
				t.Errorf("docs entry = %+v, want a directory", e)
			}
		}
	}
}

// testListErrors checks the errors of listing.
func testListErrors(t *testing.T, fsys remotefs.RemoteFS) {
	put(t, fsys, "/file", "x")

	_, err := fsys.List(t.Context(), "/missing")
	wantErr(t, "List(/missing)", err, remotefs.ErrNotFound)
	_, err = fsys.List(t.Context(), "/file")
	wantErr(t, "List(/file)", err, remotefs.ErrNotDirectory)
}

// testStat checks Stat on files, directories and missing paths.
func testStat(t *testing.T, fsys remotefs.RemoteFS) {
	mkdir(t, fsys, "/d")
	put(t, fsys, "/d/f", "abc")

	file, err := fsys.Stat(t.Context(), "/d/f")
	if err != nil || file.Size != 3 || file.IsDir() || file.Name != "f" {
		t.Errorf("Stat(/d/f) = %+v, %v, want a 3 byte file named f", file, err)
	}
	dir, err := fsys.Stat(t.Context(), "/d")
	if err != nil || !dir.IsDir() {
		t.Errorf("Stat(/d) = %+v, %v, want a directory", dir, err)
	}
	_, err = fsys.Stat(t.Context(), "/nope")
	wantErr(t, "Stat(/nope)", err, remotefs.ErrNotFound)
}

// testMkdir checks Mkdir and its errors.
func testMkdir(t *testing.T, fsys remotefs.RemoteFS) {
	mkdir(t, fsys, "/a")
	mkdir(t, fsys, "/a/b")

	wantErr(t, "Mkdir(/a) again", fsys.Mkdir(t.Context(), "/a"), remotefs.ErrExists)
	wantErr(t, "Mkdir(/x/y) without parent", fsys.Mkdir(t.Context(), "/x/y"), remotefs.ErrNotFound)
	put(t, fsys, "/file", "x")
	wantErr(t, "Mkdir under a file", fsys.Mkdir(t.Context(), "/file/child"), remotefs.ErrNotDirectory)
}

// testWriteRead checks a write and read round trip, including offsets.
func testWriteRead(t *testing.T, fsys remotefs.RemoteFS) {
	put(t, fsys, "/f", "0123456789")

	if got := get(t, fsys, "/f", 0); got != "0123456789" {
		t.Errorf("read = %q, want the full content", got)
	}
	if got := get(t, fsys, "/f", 4); got != "456789" {
		t.Errorf("read from 4 = %q, want 456789", got)
	}
	if got := get(t, fsys, "/f", 10); got != "" {
		t.Errorf("read at the end = %q, want empty", got)
	}
	if _, err := fsys.OpenRead(t.Context(), "/f", 11); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("OpenRead past the end = %v, want ErrInvalid", err)
	}
	if _, err := fsys.OpenRead(t.Context(), "/missing", 0); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("OpenRead(/missing) = %v, want ErrNotFound", err)
	}
	mkdir(t, fsys, "/dir")
	if _, err := fsys.OpenRead(t.Context(), "/dir", 0); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("OpenRead on a directory = %v, want ErrInvalid", err)
	}
}

// testWriteModes checks the create and overwrite modes.
func testWriteModes(t *testing.T, fsys remotefs.RemoteFS) {
	w, err := fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteCreate)
	if err != nil {
		t.Fatalf("OpenWrite create = %v", err)
	}
	_, _ = io.WriteString(w, "first content")
	_ = w.Close()

	_, err = fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteCreate)
	wantErr(t, "OpenWrite create on an existing file", err, remotefs.ErrExists)

	put(t, fsys, "/f", "new")
	if got := get(t, fsys, "/f", 0); got != "new" {
		t.Errorf("after overwrite = %q, want new", got)
	}
	mkdir(t, fsys, "/dir")
	_, err = fsys.OpenWrite(t.Context(), "/dir", 0, remotefs.WriteOverwrite)
	wantErr(t, "OpenWrite on a directory", err, remotefs.ErrInvalid)
	_, err = fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteMode(99))
	wantErr(t, "OpenWrite with an unknown mode", err, remotefs.ErrInvalid)
}

// testResume checks resuming a partial upload.
func testResume(t *testing.T, fsys remotefs.RemoteFS) {
	if !fsys.Capabilities().Resume {
		t.Skip("adapter does not support resume")
	}
	put(t, fsys, "/f", "hello ")

	w, err := fsys.OpenWrite(t.Context(), "/f", 6, remotefs.WriteResume)
	if err != nil {
		t.Fatalf("OpenWrite resume = %v", err)
	}
	_, _ = io.WriteString(w, "world")
	_ = w.Close()
	if got := get(t, fsys, "/f", 0); got != "hello world" {
		t.Errorf("after resume = %q, want hello world", got)
	}

	if fsys.Capabilities().Rewrite {
		w, err = fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteResume)
		if err != nil {
			t.Fatalf("OpenWrite resume at 0 = %v", err)
		}
		_, _ = io.WriteString(w, "J")
		_ = w.Close()
		if got := get(t, fsys, "/f", 0); got != "Jello world" {
			t.Errorf("after partial rewrite = %q, want Jello world", got)
		}
	} else {
		_, err = fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteResume)
		wantErr(t, "resume before the end of the file without Rewrite", err, remotefs.ErrInvalid)
	}

	_, err = fsys.OpenWrite(t.Context(), "/missing", 0, remotefs.WriteResume)
	wantErr(t, "resume of a missing file", err, remotefs.ErrNotFound)
	_, err = fsys.OpenWrite(t.Context(), "/f", 999, remotefs.WriteResume)
	wantErr(t, "resume past the end", err, remotefs.ErrInvalid)
}

// testWriteParent checks that writes need an existing directory parent.
func testWriteParent(t *testing.T, fsys remotefs.RemoteFS) {
	put(t, fsys, "/file", "x")

	_, err := fsys.OpenWrite(t.Context(), "/no/such/dir/f", 0, remotefs.WriteOverwrite)
	wantErr(t, "OpenWrite in a missing directory", err, remotefs.ErrNotFound)
	_, err = fsys.OpenWrite(t.Context(), "/file/f", 0, remotefs.WriteOverwrite)
	wantErr(t, "OpenWrite below a file", err, remotefs.ErrNotDirectory)
}

// testRemove checks Remove on files, directories and missing paths.
func testRemove(t *testing.T, fsys remotefs.RemoteFS) {
	mkdir(t, fsys, "/d")
	put(t, fsys, "/d/f", "x")

	wantErr(t, "Remove of a non-empty directory", fsys.Remove(t.Context(), "/d"), remotefs.ErrNotEmpty)
	if err := fsys.Remove(t.Context(), "/d/f"); err != nil {
		t.Errorf("Remove(/d/f) = %v", err)
	}
	if err := fsys.Remove(t.Context(), "/d"); err != nil {
		t.Errorf("Remove(/d) = %v", err)
	}
	wantErr(t, "Remove of a missing path", fsys.Remove(t.Context(), "/d"), remotefs.ErrNotFound)
	wantErr(t, "Remove of the root", fsys.Remove(t.Context(), "/"), remotefs.ErrPermission)
}

// testRemoveAll checks recursive removal.
func testRemoveAll(t *testing.T, fsys remotefs.RemoteFS) {
	mkdir(t, fsys, "/tree")
	mkdir(t, fsys, "/tree/sub")
	put(t, fsys, "/tree/sub/f", "x")
	put(t, fsys, "/keep", "y")

	if err := fsys.RemoveAll(t.Context(), "/tree"); err != nil {
		t.Fatalf("RemoveAll(/tree) = %v", err)
	}
	if _, err := fsys.Stat(t.Context(), "/tree/sub/f"); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Stat below a removed tree = %v, want ErrNotFound", err)
	}
	if got := get(t, fsys, "/keep", 0); got != "y" {
		t.Errorf("unrelated file = %q, want y", got)
	}
	if err := fsys.RemoveAll(t.Context(), "/tree"); err != nil {
		t.Errorf("RemoveAll of a missing path = %v, want nil", err)
	}
	wantErr(t, "RemoveAll of the root", fsys.RemoveAll(t.Context(), "/"), remotefs.ErrPermission)
}

// testRename checks Rename for files, directories and its errors.
func testRename(t *testing.T, fsys remotefs.RemoteFS) {
	mkdir(t, fsys, "/d")
	put(t, fsys, "/d/f", "content")
	put(t, fsys, "/other", "o")

	if err := fsys.Rename(t.Context(), "/d/f", "/g"); err != nil {
		t.Fatalf("Rename file = %v", err)
	}
	if got := get(t, fsys, "/g", 0); got != "content" {
		t.Errorf("renamed file = %q, want content", got)
	}
	if err := fsys.Rename(t.Context(), "/d", "/e"); err != nil {
		t.Fatalf("Rename directory = %v", err)
	}
	put(t, fsys, "/e/inner", "i")
	if err := fsys.Rename(t.Context(), "/e", "/moved"); err != nil {
		t.Fatalf("Rename directory with children = %v", err)
	}
	if got := get(t, fsys, "/moved/inner", 0); got != "i" {
		t.Errorf("child of a renamed directory = %q, want i", got)
	}

	wantErr(t, "Rename onto an existing path", fsys.Rename(t.Context(), "/g", "/other"), remotefs.ErrExists)
	wantErr(t, "Rename of a missing path", fsys.Rename(t.Context(), "/nope", "/x"), remotefs.ErrNotFound)
	wantErr(t, "Rename into a missing directory", fsys.Rename(t.Context(), "/g", "/no/dir/g"), remotefs.ErrNotFound)
	wantErr(t, "Rename into itself", fsys.Rename(t.Context(), "/moved", "/moved/sub"), remotefs.ErrInvalid)
}

// testMetadata checks Chmod and Chtimes on adapters that advertise them.
func testMetadata(t *testing.T, fsys remotefs.RemoteFS) {
	caps := fsys.Capabilities()
	put(t, fsys, "/f", "x")

	if caps.Chmod {
		if err := fsys.Chmod(t.Context(), "/f", 0o600); err != nil {
			t.Fatalf("Chmod = %v", err)
		}
		if e, _ := fsys.Stat(t.Context(), "/f"); e.Mode.Perm() != 0o600 || e.IsDir() {
			t.Errorf("mode after Chmod = %v, want 0600 regular file", e.Mode)
		}
		wantErr(t, "Chmod of a missing path", fsys.Chmod(t.Context(), "/nope", 0o600), remotefs.ErrNotFound)
	}
	if caps.Chtimes {
		want := time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)
		if err := fsys.Chtimes(t.Context(), "/f", want.In(time.FixedZone("UTC-3", -3*3600))); err != nil {
			t.Fatalf("Chtimes = %v", err)
		}
		e, _ := fsys.Stat(t.Context(), "/f")
		if !e.ModTime.Equal(truncate(want, e.ModTimePrecision)) || e.ModTime.Location() != time.UTC {
			t.Errorf("ModTime after Chtimes = %v, want %v in UTC (precision %v)", e.ModTime, truncate(want, e.ModTimePrecision), e.ModTimePrecision)
		}
		wantErr(t, "Chtimes of a missing path", fsys.Chtimes(t.Context(), "/nope", want), remotefs.ErrNotFound)
	}
}

// truncate rounds a time down to the precision an entry reports, since some protocols only list minutes or days.
func truncate(t time.Time, precision remotefs.Precision) time.Time {
	switch precision {
	case remotefs.PrecisionMinute:
		return t.Truncate(time.Minute)
	case remotefs.PrecisionDay:
		return t.Truncate(24 * time.Hour)
	}
	return t
}

// testUTC checks that modification times are reported in UTC.
func testUTC(t *testing.T, fsys remotefs.RemoteFS) {
	put(t, fsys, "/f", "x")

	e, err := fsys.Stat(t.Context(), "/f")
	if err != nil || e.ModTime.IsZero() || e.ModTime.Location() != time.UTC {
		t.Errorf("Stat = %+v, %v, want a non-zero UTC modification time", e, err)
	}
}

// testCanceled checks that every operation honors a canceled context.
func testCanceled(t *testing.T, fsys remotefs.RemoteFS) {
	put(t, fsys, "/f", "x")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	operations := map[string]error{
		"List":      second(fsys.List(ctx, "/")),
		"Stat":      second(fsys.Stat(ctx, "/f")),
		"OpenRead":  second(fsys.OpenRead(ctx, "/f", 0)),
		"OpenWrite": second(fsys.OpenWrite(ctx, "/g", 0, remotefs.WriteOverwrite)),
		"Mkdir":     fsys.Mkdir(ctx, "/d"),
		"Remove":    fsys.Remove(ctx, "/f"),
		"RemoveAll": fsys.RemoveAll(ctx, "/f"),
		"Rename":    fsys.Rename(ctx, "/f", "/h"),
		"Chmod":     fsys.Chmod(ctx, "/f", 0o600),
		"Chtimes":   fsys.Chtimes(ctx, "/f", time.Now()),
		"Ping":      fsys.Ping(ctx),
	}
	for name, err := range operations {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s with a canceled context = %v, want context.Canceled", name, err)
		}
	}
}

// second returns the error of a call that also returns a value.
func second[T any](_ T, err error) error { return err }

// testLifecycle checks Ping and the behavior after Close.
func testLifecycle(t *testing.T, fsys remotefs.RemoteFS) {
	if err := fsys.Ping(t.Context()); err != nil {
		t.Fatalf("Ping = %v", err)
	}
	w, err := fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatalf("OpenWrite = %v", err)
	}

	if err := fsys.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if err := fsys.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
	wantErr(t, "Ping after Close", fsys.Ping(t.Context()), remotefs.ErrConnectionLost)
	_, err = fsys.List(t.Context(), "/")
	wantErr(t, "List after Close", err, remotefs.ErrConnectionLost)
	_, err = w.Write([]byte("late"))
	wantErr(t, "Write after Close", err, remotefs.ErrConnectionLost)
}
