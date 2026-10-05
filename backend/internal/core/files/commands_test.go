package files_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/remotefs/memfs"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/testutil"
)

// commanding is an in-memory file system whose "server" also runs the operations of [remotefs.Commands], recording them, with
// failures that can be injected per operation.
type commanding struct {
	shared
	mu    sync.Mutex
	tools remotefs.Tools
	fail  map[string]error
	stat  map[string]error
	ran   []string
}

// Stat answers the injected failure of a path, or describes it.
func (c *commanding) Stat(ctx context.Context, p string) (remotefs.Entry, error) {
	c.mu.Lock()
	err := c.stat[p]
	c.mu.Unlock()
	if err != nil {
		return remotefs.Entry{}, err
	}
	return c.shared.Stat(ctx, p)
}

// failure returns the injected failure of an operation and records that it ran.
func (c *commanding) failure(op string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ran = append(c.ran, op)
	return c.fail[op]
}

// count tells how many times an operation ran.
func (c *commanding) count(op string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, r := range c.ran {
		if r == op {
			n++
		}
	}
	return n
}

// read returns the content of a file.
func (c *commanding) read(ctx context.Context, p string) ([]byte, error) {
	r, err := c.OpenRead(ctx, p, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}

// write stores content in a file.
func (c *commanding) write(ctx context.Context, p string, data []byte, mode remotefs.WriteMode) error {
	w, err := c.OpenWrite(ctx, p, 0, mode)
	if err != nil {
		return err
	}
	_, _ = w.Write(data)
	return w.Close()
}

// Tools implements [remotefs.Commands].
func (c *commanding) Tools(context.Context) (remotefs.Tools, error) {
	return c.tools, c.failure("tools")
}

// ServerCopy implements [remotefs.Commands].
func (c *commanding) ServerCopy(ctx context.Context, from, to string, overwrite bool) error {
	if err := c.failure("copy"); err != nil {
		return err
	}
	data, err := c.read(ctx, from)
	if err != nil {
		return err
	}
	mode := remotefs.WriteCreate
	if overwrite {
		mode = remotefs.WriteOverwrite
	}
	return c.write(ctx, to, data, mode)
}

// ServerChecksum implements [remotefs.Commands] with the SHA-256 of the file; tests tell it ran by counting it.
func (c *commanding) ServerChecksum(ctx context.Context, p, _ string) (string, error) {
	if err := c.failure("checksum"); err != nil {
		return "", err
	}
	data, err := c.read(ctx, p)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), err
}

// ServerUsage implements [remotefs.Commands].
func (c *commanding) ServerUsage(context.Context, string) (int64, error) {
	return 4096, c.failure("usage")
}

// ServerArchive implements [remotefs.Commands]: the archive lists the names it packs.
func (c *commanding) ServerArchive(ctx context.Context, _ string, names []string, target, _ string) error {
	if err := c.failure("archive"); err != nil {
		return err
	}
	return c.write(ctx, target, []byte(strings.Join(names, ",")), remotefs.WriteCreate)
}

// ServerExtract implements [remotefs.Commands]: it writes one file into the folder.
func (c *commanding) ServerExtract(ctx context.Context, _, format, dir string) error {
	if err := c.failure("extract"); err != nil {
		return err
	}
	return c.write(ctx, path.Join(dir, "from-"+strings.ReplaceAll(format, ".", "-")), []byte("x"), remotefs.WriteCreate)
}

// everything is what a server with every tool offers.
var everything = remotefs.Tools{Copy: true, Usage: true, Checksums: []string{"sha256"}, Archive: []string{"zip", "tar.gz"}, Extract: []string{"zip", "tar.gz"}}

// commandFixture is a service over a file system that runs commands, with an editor and a viewer, and a switch that says
// whether the instance allows commands.
type commandFixture struct {
	svc     *files.Service
	fs      *commanding
	editor  files.Target
	viewer  files.Target
	allowed bool
}

// newCommandFixture returns a fixture that allows commands.
func newCommandFixture(t *testing.T) *commandFixture {
	t.Helper()
	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fsys := &commanding{shared: shared{memfs.New(clock)}, tools: everything, fail: map[string]error{}, stat: map[string]error{}}
	pool := connections.New(connector{fsys}, clock, connections.Config{Attempts: 1})
	t.Cleanup(pool.Close)
	ws, site, editor, viewer := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f := &commandFixture{fs: fsys, allowed: true, editor: files.Target{Actor: editor, Workspace: ws, Site: site, Session: "s"}, viewer: files.Target{Actor: viewer, Workspace: ws, Site: site, Session: "s"}}
	f.svc = files.NewService(roles{editor: workspace.RoleEditor, viewer: workspace.RoleViewer}, pool).WithCommands(func() bool { return f.allowed })
	return f
}

// TestService_Tools verifies what is told about the server of a connection (ADR-0027).
//
// Use cases:
//   - a viewer learns what the server can do
//   - an instance that does not allow commands, and a connection that cannot run them, offer nothing
func TestService_Tools(t *testing.T) {
	f := newCommandFixture(t)
	if tools, err := f.svc.Tools(t.Context(), f.viewer); err != nil || !tools.Copy {
		t.Errorf("Tools() = %+v, %v", tools, err)
	}
	f.allowed = false
	if tools, err := f.svc.Tools(t.Context(), f.viewer); err != nil || tools.Copy {
		t.Errorf("Tools(not allowed) = %+v, %v, want none", tools, err)
	}
	plain := newFixture(t)
	if tools, err := plain.svc.Tools(t.Context(), plain.viewer); err != nil || tools.Copy {
		t.Errorf("Tools(no commands) = %+v, %v, want none", tools, err)
	}
}

// TestService_ServerChecksum verifies that checksums are computed by the server when it can.
//
// Use cases:
//   - the server answers the digest, with the size of the file
//   - a server that cannot leaves Transferly to read the file; a server that fails otherwise fails the checksum
//   - a folder is refused as before, without asking the server
func TestService_ServerChecksum(t *testing.T) {
	f := newCommandFixture(t)
	_ = f.fs.write(t.Context(), "/a.txt", []byte("hello"), remotefs.WriteCreate)
	want := sha256.Sum256([]byte("hello"))
	sum, err := f.svc.Checksum(t.Context(), f.viewer, "/a.txt", "sha256")
	if err != nil || sum.Sum != hex.EncodeToString(want[:]) || sum.Size != 5 || f.fs.count("checksum") != 1 {
		t.Errorf("Checksum() = %+v, %v, want the server's", sum, err)
	}
	f.fs.fail["checksum"] = remotefs.ErrUnsupported
	if sum, err := f.svc.Checksum(t.Context(), f.viewer, "/a.txt", "sha256"); err != nil || sum.Sum != hex.EncodeToString(want[:]) {
		t.Errorf("Checksum(unsupported) = %+v, %v, want Transferly's", sum, err)
	}
	f.fs.fail["checksum"] = remotefs.ErrPermission
	if _, err := f.svc.Checksum(t.Context(), f.viewer, "/a.txt", "sha256"); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("Checksum(refused) = %v", err)
	}
	f.allowed = false
	asked := f.fs.count("checksum")
	if _, err := f.svc.Checksum(t.Context(), f.viewer, "/a.txt", "sha256"); err != nil || f.fs.count("checksum") != asked {
		t.Errorf("Checksum(not allowed) = %v, asked the server", err)
	}
	_ = f.fs.Mkdir(t.Context(), "/d")
	if _, err := f.svc.Checksum(t.Context(), f.viewer, "/d", "sha256"); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Checksum(folder) = %v", err)
	}
}

// TestService_ServerCopy verifies that copies within one site run on the server when it can.
//
// Use cases:
//   - a file and the files of a folder are copied by the server, which reports the bytes to the observer
//   - an existing file is replaced or renamed as the policy says
//   - a server that cannot leaves Transferly to move the data; a failure of the server fails the copy
//   - a verified copy reads the bytes, so the server does not copy it
func TestService_ServerCopy(t *testing.T) {
	f := newCommandFixture(t)
	ctx := t.Context()
	_ = f.fs.write(ctx, "/a.txt", []byte("hello"), remotefs.WriteCreate)
	_ = f.fs.Mkdir(ctx, "/d")
	_ = f.fs.write(ctx, "/d/b.txt", []byte("bee"), remotefs.WriteCreate)
	obs := &recorder{}
	to := f.editor
	to.Observer = obs
	if e, err := f.svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/a.txt", Destination: to, DestinationPath: "/copy.txt"}, files.PolicyFail); err != nil || e.Size != 5 {
		t.Fatalf("Copy() = %+v, %v", e, err)
	}
	if f.fs.count("copy") != 1 || len(obs.started) != 1 || obs.started[0] != 5 || obs.bytes != 5 {
		t.Errorf("server copies = %d, observer started %v with %d bytes", f.fs.count("copy"), obs.started, obs.bytes)
	}
	if _, err := f.svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/d", Destination: f.editor, DestinationPath: "/e"}, files.PolicyFail); err != nil || f.fs.count("copy") != 2 {
		t.Errorf("Copy(folder) = %v after %d server copies", err, f.fs.count("copy"))
	}
	if _, err := f.svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/a.txt", Destination: f.editor, DestinationPath: "/copy.txt"}, files.PolicyOverwrite); err != nil {
		t.Errorf("Copy(overwrite) = %v", err)
	}
	if e, err := f.svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/a.txt", Destination: f.editor, DestinationPath: "/copy.txt"}, files.PolicyRename); err != nil || e.Name != "copy (1).txt" {
		t.Errorf("Copy(rename) = %+v, %v", e, err)
	}
	f.fs.fail["copy"] = remotefs.ErrUnsupported
	if _, err := f.svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/a.txt", Destination: f.editor, DestinationPath: "/streamed.txt"}, files.PolicyFail); err != nil {
		t.Errorf("Copy(unsupported) = %v, want it streamed", err)
	}
	f.fs.fail["copy"] = remotefs.ErrQuota
	if _, err := f.svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/a.txt", Destination: f.editor, DestinationPath: "/full.txt"}, files.PolicyFail); !errors.Is(err, remotefs.ErrQuota) {
		t.Errorf("Copy(full) = %v", err)
	}
	delete(f.fs.fail, "copy")
	before := f.fs.count("copy")
	if _, err := f.svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/a.txt", Destination: f.editor, DestinationPath: "/verified.txt", Verify: true}, files.PolicyFail); err != nil || f.fs.count("copy") != before {
		t.Errorf("Copy(verified) = %v, server copies %d → %d", err, before, f.fs.count("copy"))
	}
	f.allowed = false
	if _, err := f.svc.Copy(ctx, files.CopyLocation{Source: f.editor, SourcePath: "/a.txt", Destination: f.editor, DestinationPath: "/plain.txt"}, files.PolicyFail); err != nil || f.fs.count("copy") != before {
		t.Errorf("Copy(not allowed) = %v", err)
	}
}

// TestService_Compress verifies archives made on the server.
//
// Use cases:
//   - an editor packs entries of a folder into a new archive next to them
//   - unknown formats, names that do not match the format and names with a slash are invalid; a viewer may not
//   - a connection that cannot run commands, or an instance that does not allow them, is unsupported; a failure of the server
//     fails the archive
func TestService_Compress(t *testing.T) {
	f := newCommandFixture(t)
	ctx := t.Context()
	_ = f.fs.write(ctx, "/a.txt", []byte("a"), remotefs.WriteCreate)
	e, err := f.svc.Compress(ctx, f.editor, "/", []string{"a.txt"}, "bundle.zip", remotefs.FormatZip)
	if err != nil || e.Path != "/bundle.zip" {
		t.Fatalf("Compress() = %+v, %v", e, err)
	}
	for name, err := range map[string]error{
		"format":   second(f.svc.Compress(ctx, f.editor, "/", []string{"a.txt"}, "b.rar", "rar")),
		"mismatch": second(f.svc.Compress(ctx, f.editor, "/", []string{"a.txt"}, "b.zip", remotefs.FormatTarGz)),
		"bad name": second(f.svc.Compress(ctx, f.editor, "/", []string{"a/b"}, "b.zip", remotefs.FormatZip)),
		"target":   second(f.svc.Compress(ctx, f.editor, "/", []string{"a.txt"}, "../b.zip", remotefs.FormatZip)),
	} {
		if !errors.Is(err, remotefs.ErrInvalid) {
			t.Errorf("Compress(%s) = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := f.svc.Compress(ctx, f.viewer, "/", []string{"a.txt"}, "v.zip", remotefs.FormatZip); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("Compress(viewer) = %v", err)
	}
	f.fs.fail["archive"] = remotefs.ErrQuota
	if _, err := f.svc.Compress(ctx, f.editor, "/", []string{"a.txt"}, "q.tar.gz", remotefs.FormatTarGz); !errors.Is(err, remotefs.ErrQuota) {
		t.Errorf("Compress(full) = %v", err)
	}
	f.allowed = false
	if _, err := f.svc.Compress(ctx, f.editor, "/", []string{"a.txt"}, "n.zip", remotefs.FormatZip); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Errorf("Compress(not allowed) = %v", err)
	}
}

// TestService_Extract verifies archives unpacked on the server.
//
// Use cases:
//   - an archive is unpacked into a new folder named after it, or a free name when that one is taken; .tgz reads as tar.gz
//   - a file that is not an archive is invalid; a viewer may not
//   - a folder name that cannot be checked or made fails; a failure of the server removes the folder it made; an unsupported
//     connection is unsupported
func TestService_Extract(t *testing.T) {
	f := newCommandFixture(t)
	ctx := t.Context()
	_ = f.fs.write(ctx, "/photos.ZIP", []byte("z"), remotefs.WriteCreate)
	_ = f.fs.write(ctx, "/logs.tgz", []byte("t"), remotefs.WriteCreate)
	if e, err := f.svc.Extract(ctx, f.editor, "/photos.ZIP"); err != nil || e.Path != "/photos" || !e.IsDir() {
		t.Fatalf("Extract() = %+v, %v", e, err)
	}
	if e, err := f.svc.Extract(ctx, f.editor, "/photos.ZIP"); err != nil || e.Path != "/photos (1)" {
		t.Errorf("Extract(again) = %+v, %v", e, err)
	}
	if _, err := f.fs.Stat(ctx, "/logs"); !errors.Is(err, remotefs.ErrNotFound) {
		t.Fatal(err)
	}
	if e, err := f.svc.Extract(ctx, f.editor, "/logs.tgz"); err != nil || e.Path != "/logs" {
		t.Errorf("Extract(tgz) = %+v, %v", e, err)
	}
	if _, err := f.fs.Stat(ctx, "/logs/from-tar-gz"); err != nil {
		t.Errorf("tgz extracted as %v", err)
	}
	if _, err := f.svc.Extract(ctx, f.editor, "/notes.txt"); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Extract(not an archive) = %v", err)
	}
	if _, err := f.svc.Extract(ctx, f.editor, "/.zip"); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Extract(no stem) = %v", err)
	}
	if _, err := f.svc.Extract(ctx, f.viewer, "/photos.ZIP"); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("Extract(viewer) = %v", err)
	}
	f.fs.stat["/logs (1)"] = remotefs.ErrConnectionLost
	if _, err := f.svc.Extract(ctx, f.editor, "/logs.tgz"); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Extract(free name failing) = %v", err)
	}
	f.fs.stat["/logs"] = remotefs.ErrPermission
	if _, err := f.svc.Extract(ctx, f.editor, "/logs.tgz"); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("Extract(folder unreadable) = %v", err)
	}
	f.fs.stat = map[string]error{}
	if _, err := f.svc.Extract(ctx, f.editor, "/gone/a.zip"); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Extract(in a missing folder) = %v", err)
	}
	f.fs.fail["extract"] = remotefs.ErrPermission
	if _, err := f.svc.Extract(ctx, f.editor, "/logs.tgz"); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("Extract(refused) = %v", err)
	}
	if _, err := f.fs.Stat(ctx, "/logs (1)"); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("the folder of a failed extract stayed: %v", err)
	}
	f.allowed = false
	if _, err := f.svc.Extract(ctx, f.editor, "/logs.tgz"); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Errorf("Extract(not allowed) = %v", err)
	}
}

// TestService_Usage verifies the space a folder takes, measured by the server.
//
// Use cases:
//   - a viewer may ask; a connection that cannot run commands is unsupported
func TestService_Usage(t *testing.T) {
	f := newCommandFixture(t)
	if used, err := f.svc.Usage(t.Context(), f.viewer, "/"); err != nil || used != 4096 {
		t.Errorf("Usage() = %d, %v", used, err)
	}
	plain := newFixture(t)
	if _, err := plain.svc.Usage(t.Context(), plain.viewer, "/"); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Errorf("Usage(no commands) = %v", err)
	}
}

// TestFormatOf verifies which names are archives a server unpacks.
func TestFormatOf(t *testing.T) {
	for name, want := range map[string]string{"a.zip": "zip", "A.TAR.GZ": "tar.gz", "a.tgz": "tar.gz", "a.tar": "", ".zip": "", "a.zip.txt": ""} {
		if got := files.FormatOf(name); got != want {
			t.Errorf("FormatOf(%q) = %q, want %q", name, got, want)
		}
	}
}
