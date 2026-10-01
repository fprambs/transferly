package files_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/testutil"
)

// oddFS lists what real servers can hand out: links, devices, names with separators and folders that never end.
type oddFS struct{ *flakyFS }

// List adds strange entries to the folders that the tests set up for them.
func (o oddFS) List(ctx context.Context, dir string) ([]remotefs.Entry, error) {
	switch {
	case dir == "/odd":
		entries, err := o.flakyFS.List(ctx, dir)
		for _, name := range []string{"../evil", "a/b", "back\\slash", "", ".", ".."} {
			entries = append(entries, remotefs.Entry{Name: name, Mode: 0o644})
		}
		entries = append(entries,
			remotefs.Entry{Name: "link", Mode: fs.ModeSymlink | 0o777},
			remotefs.Entry{Name: "device", Mode: fs.ModeDevice | 0o600},
		)
		return entries, err
	case dir == "/deep" || strings.HasPrefix(dir, "/deep/"):
		return []remotefs.Entry{{Name: "d", Mode: fs.ModeDir | 0o755}}, nil
	case dir == "/listfail":
		return nil, fmt.Errorf("%w: dropped", remotefs.ErrConnectionLost)
	case dir == "/many":
		entries := make([]remotefs.Entry, 200)
		for i := range entries {
			entries[i] = remotefs.Entry{Name: fmt.Sprintf("d%d", i), Mode: fs.ModeDir | 0o755}
		}
		return entries, nil
	case strings.HasPrefix(dir, "/many/"):
		return nil, nil
	}
	return o.flakyFS.List(ctx, dir)
}

// Stat describes the folders of the odd tree as folders.
func (o oddFS) Stat(ctx context.Context, p string) (remotefs.Entry, error) {
	switch {
	case p == "/deep" || p == "/many" || p == "/listfail":
		return remotefs.Entry{Name: p[1:], Path: p, Mode: fs.ModeDir | 0o755}, nil
	case strings.HasPrefix(p, "/statfail"), p == "/renamefail/a (1).txt":
		return remotefs.Entry{}, fmt.Errorf("%w: dropped", remotefs.ErrConnectionLost)
	case p == "/renamefail/a.txt":
		return remotefs.Entry{Name: "a.txt", Path: p, Mode: 0o644}, nil
	}
	return o.flakyFS.Stat(ctx, p)
}

// oddConnector hands out the odd file system.
type oddConnector struct{ fsys oddFS }

// Connect returns the odd file system.
func (c oddConnector) Connect(context.Context, connections.Key, string) (remotefs.RemoteFS, error) {
	return c.fsys, nil
}

// oddService returns a service over the odd file system, with the base fixture's files behind it.
func oddService(t *testing.T, f *fixture) *files.Service {
	t.Helper()
	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	pool := connections.New(oddConnector{oddFS{f.fs}}, clock, connections.Config{Attempts: 1})
	t.Cleanup(pool.Close)
	return files.NewService(roles{f.editor.Actor: workspace.RoleEditor, f.viewer.Actor: workspace.RoleViewer}, pool)
}

// unzip reads a ZIP held in memory into a map from name to content; folders map to an empty marker.
func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("the archive is not a ZIP: %v", err)
	}
	out := map[string]string{}
	for _, file := range reader.File {
		if strings.HasSuffix(file.Name, "/") {
			out[file.Name] = "<dir>"
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[file.Name] = string(content)
	}
	return out
}

// TestService_Archive verifies streaming a folder as a ZIP.
//
// Scenarios:
//   - a tree of files, subfolders and an empty folder becomes a ZIP with the same names and contents, and the observer learns
//     that the size is unknown and counts every byte read
//   - the root of a site is named archive and any other folder by its own name
//   - links, devices and names that could climb out of the folder or hide in another one are left out
//   - a viewer may archive, a file, a missing folder, an empty path and an unknown user are refused
//   - a tree that goes deeper or holds more entries than the limits of the service fails with ErrTreeTooLarge
//   - a folder that cannot be listed, a file that cannot be opened or fails to read fails the archive, and closing early or with a canceled context stops it and returns the connection
func TestService_Archive(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	for _, dir := range []string{"/site", "/site/sub", "/site/empty", "/odd", "/big"} {
		if _, err := f.svc.Mkdir(ctx, f.editor, dir); err != nil {
			t.Fatal(err)
		}
	}
	f.put(t, "/site/a.txt", "alpha")
	f.put(t, "/site/sub/b.txt", "bravo bravo")
	obs := &recorder{}
	target := f.viewer
	target.Observer = obs

	archive, err := f.svc.Archive(ctx, target, "/site")
	if err != nil || archive.Name != "site" {
		t.Fatalf("Archive() = %+v, %v", archive, err)
	}
	data, err := io.ReadAll(archive.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := unzip(t, data), map[string]string{"a.txt": "alpha", "sub/": "<dir>", "sub/b.txt": "bravo bravo", "empty/": "<dir>"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("archive holds %v; want %v", got, want)
	}
	if len(obs.started) != 1 || obs.started[0] != -1 || obs.bytes != 16 {
		t.Errorf("observer saw %v started and %d bytes; want an unknown size and 16 bytes", obs.started, obs.bytes)
	}
	if err := archive.Body.Close(); err != nil {
		t.Errorf("closing twice = %v", err)
	}
	if f.idle(f.viewer) == 0 {
		t.Error("the connection was not given back")
	}

	root, err := f.svc.Archive(ctx, f.editor, "/")
	if err != nil || root.Name != "archive" {
		t.Fatalf("Archive() of the root = %+v, %v", root, err)
	}
	_, _ = io.Copy(io.Discard, root.Body)
	_ = root.Body.Close()

	odd := oddService(t, f).WithTreeLimits(5, 4)
	f.put(t, "/odd/fine.txt", "ok")
	fromOdd, err := odd.Archive(ctx, f.editor, "/odd")
	if err != nil {
		t.Fatal(err)
	}
	data, err = io.ReadAll(fromOdd.Body)
	_ = fromOdd.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := unzip(t, data); len(got) != 1 || got["fine.txt"] != "ok" {
		t.Errorf("archive of the odd folder holds %v; want only fine.txt", got)
	}

	if _, err := f.svc.Archive(ctx, f.editor, "/site/a.txt"); !errors.Is(err, remotefs.ErrNotDirectory) {
		t.Errorf("Archive() of a file = %v; want ErrNotDirectory", err)
	}
	if _, err := f.svc.Archive(ctx, f.editor, "/missing"); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Archive() of a missing folder = %v; want ErrNotFound", err)
	}
	if _, err := f.svc.Archive(ctx, f.editor, ""); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Archive() of an empty path = %v; want ErrInvalid", err)
	}
	stranger := files.Target{Actor: uuid.New(), Workspace: f.editor.Workspace, Site: f.editor.Site, Session: "s"}
	if _, err := f.svc.Archive(ctx, stranger, "/site"); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("Archive() by an unknown user = %v; want ErrNotFound", err)
	}

	shallow := oddService(t, f).WithTreeLimits(3, 100)
	for dir, service := range map[string]*files.Service{"/deep": shallow, "/many": odd} {
		big, err := service.Archive(ctx, f.editor, dir)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(big.Body)
		_ = big.Body.Close()
		if !errors.Is(err, files.ErrTreeTooLarge) {
			t.Errorf("Archive() of %s = %v; want ErrTreeTooLarge", dir, err)
		}
	}

	unlisted, err := odd.Archive(ctx, f.editor, "/listfail")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(unlisted.Body); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("reading an archive whose folder cannot be listed = %v; want ErrConnectionLost", err)
	}
	_ = unlisted.Body.Close()

	f.fs.failOpen = true
	unopened, err := f.svc.Archive(ctx, f.editor, "/site")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(unopened.Body); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("reading an archive whose file cannot be opened = %v; want ErrConnectionLost", err)
	}
	_ = unopened.Body.Close()
	f.fs.failOpen = false

	folders, err := oddService(t, f).Archive(ctx, f.editor, "/many")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = folders.Body.Read(make([]byte, 16))
	_ = folders.Body.Close()

	f.fs.failRead = true
	broken, err := f.svc.Archive(ctx, f.editor, "/site")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(broken.Body); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("reading an archive whose file breaks = %v; want ErrConnectionLost", err)
	}
	_ = broken.Body.Close()
	f.fs.failRead = false

	big := strings.Repeat("x", 1<<20)
	f.put(t, "/big/one.bin", big)
	early, err := f.svc.Archive(ctx, f.editor, "/big")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = early.Body.Read(make([]byte, 16))
	if err := early.Body.Close(); err != nil {
		t.Errorf("closing early = %v", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	late, err := f.svc.Archive(canceled, f.editor, "/big")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := io.ReadAll(late.Body); !errors.Is(err, context.Canceled) {
		t.Errorf("reading after the cancellation = %v; want context.Canceled", err)
	}
	_ = late.Body.Close()
}
