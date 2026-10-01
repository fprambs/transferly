package files_test

import (
	"context"
	"errors"
	"io"
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

// tamperFS corrupts what is written to some paths and refuses to read others back.
type tamperFS struct{ *flakyFS }

// OpenWrite turns the bytes written below /tampered into other bytes.
func (t tamperFS) OpenWrite(ctx context.Context, p string, offset int64, mode remotefs.WriteMode) (io.WriteCloser, error) {
	w, err := t.flakyFS.OpenWrite(ctx, p, offset, mode)
	if err != nil || !strings.HasPrefix(p, "/tampered") {
		return w, err
	}
	return tamperWriter{w}, nil
}

// OpenRead fails for paths below /unreadable.
func (t tamperFS) OpenRead(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	if strings.HasPrefix(p, "/unreadable") {
		return nil, remotefs.ErrConnectionLost
	}
	return t.flakyFS.OpenRead(ctx, p, offset)
}

// tamperWriter changes every byte it is given.
type tamperWriter struct{ io.WriteCloser }

// Write writes each byte plus one.
func (w tamperWriter) Write(p []byte) (int, error) {
	changed := make([]byte, len(p))
	for i, b := range p {
		changed[i] = b + 1
	}
	return w.WriteCloser.Write(changed)
}

// tamperService returns a service over the tampering file system.
func tamperService(t *testing.T, f *fixture) *files.Service {
	t.Helper()
	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	pool := connections.New(tamperConnector{tamperFS{f.fs}}, clock, connections.Config{Attempts: 1})
	t.Cleanup(pool.Close)
	return files.NewService(roles{f.editor.Actor: workspace.RoleEditor, f.viewer.Actor: workspace.RoleViewer}, pool)
}

// tamperConnector hands out the tampering file system.
type tamperConnector struct{ fsys tamperFS }

// Connect returns the tampering file system.
func (c tamperConnector) Connect(context.Context, connections.Key, string) (remotefs.RemoteFS, error) {
	return c.fsys, nil
}

// TestService_Checksum verifies the digests of remote files.
//
// Scenarios:
//   - md5, sha1, sha256 and sha512 of a file are the known digests of its content, in lower case, with its size
//   - a viewer may compute them, and an unknown algorithm, a folder, a missing file, an empty path and an unknown user are refused
//   - a file that breaks midway fails the digest, and a canceled context stops it
func TestService_Checksum(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.put(t, "/hello", "hello")

	for algorithm, want := range map[string]string{
		"md5":    "5d41402abc4b2a76b9719d911017c592",
		"sha1":   "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d",
		"sha256": "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		"sha512": "9b71d224bd62f3785d96d46ad3ea3d73319bfbc2890caadae2dff72519673ca72323c3d99ba5c11d7c7acc6e14b8c5da0c4663475c2e5c3adef46f73bcdec043",
	} {
		got, err := f.svc.Checksum(ctx, f.viewer, "/hello", algorithm)
		if err != nil || got.Sum != want || got.Size != 5 || got.Algorithm != algorithm {
			t.Errorf("Checksum(%s) = %+v, %v; want %s", algorithm, got, err, want)
		}
	}

	if _, err := f.svc.Checksum(ctx, f.editor, "/hello", "crc32"); !errors.Is(err, files.ErrUnsupportedAlgorithm) {
		t.Errorf("Checksum() with an unknown algorithm = %v; want ErrUnsupportedAlgorithm", err)
	}
	if _, err := f.svc.Mkdir(ctx, f.editor, "/dir"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Checksum(ctx, f.editor, "/dir", "sha256"); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Checksum() of a folder = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.Checksum(ctx, f.editor, "/missing", "sha256"); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Checksum() of a missing file = %v; want ErrNotFound", err)
	}
	if _, err := f.svc.Checksum(ctx, f.editor, "", "sha256"); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Checksum() of an empty path = %v; want ErrInvalid", err)
	}
	stranger := files.Target{Actor: uuid.New(), Workspace: f.editor.Workspace, Site: f.editor.Site, Session: "s"}
	if _, err := f.svc.Checksum(ctx, stranger, "/hello", "sha256"); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("Checksum() by an unknown user = %v; want ErrNotFound", err)
	}

	f.fs.failOpen = true
	if _, err := f.svc.Checksum(ctx, f.editor, "/hello", "sha256"); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Checksum() of a file that cannot be opened = %v; want ErrConnectionLost", err)
	}
	f.fs.failOpen, f.fs.failRead = false, true
	if _, err := f.svc.Checksum(ctx, f.editor, "/hello", "sha256"); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Checksum() of a file that breaks = %v; want ErrConnectionLost", err)
	}
	f.fs.failRead = false
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.svc.Checksum(canceled, f.editor, "/hello", "sha256"); !errors.Is(err, context.Canceled) {
		t.Errorf("Checksum() with a canceled context = %v; want context.Canceled", err)
	}
}

// TestService_CopyVerify verifies the check of copies.
//
// Scenarios:
//   - a verified copy of a file, and of a tree, succeeds when the destination holds the bytes that were sent
//   - a destination that holds other bytes fails the copy with ErrChecksumMismatch
//   - a destination that cannot be read back fails the copy with that error
//   - a copy that is not verified does not read the destination back
func TestService_CopyVerify(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	svc := tamperService(t, f)
	f.put(t, "/src.txt", "payload")
	for _, dir := range []string{"/tree", "/tampered-dir", "/unreadable-dir"} {
		if _, err := f.svc.Mkdir(ctx, f.editor, dir); err != nil {
			t.Fatal(err)
		}
	}
	f.put(t, "/tree/a.txt", "A")
	other := otherSite(f.editor)

	if _, err := svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: other, DestinationPath: "/ok.txt", Verify: true}, files.PolicyFail); err != nil {
		t.Errorf("verified copy = %v", err)
	}
	if _, err := svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/tree", Destination: other, DestinationPath: "/tree-ok", Verify: true}, files.PolicyFail); err != nil {
		t.Errorf("verified copy of a tree = %v", err)
	}
	if _, err := svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: other, DestinationPath: "/tampered.txt", Verify: true}, files.PolicyFail); !errors.Is(err, files.ErrChecksumMismatch) {
		t.Errorf("copy of tampered bytes = %v; want ErrChecksumMismatch", err)
	}
	if _, err := svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: other, DestinationPath: "/unreadable.txt", Verify: true}, files.PolicyFail); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("copy that cannot be read back = %v; want ErrConnectionLost", err)
	}
	if _, err := svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: other, DestinationPath: "/tampered-plain.txt"}, files.PolicyFail); err != nil {
		t.Errorf("unverified copy of tampered bytes = %v; want it to pass unchecked", err)
	}
}
