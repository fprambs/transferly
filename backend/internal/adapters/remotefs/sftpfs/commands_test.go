package sftpfs_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// allTools is what the probe of a server with every tool prints.
const allTools = "cp\ntar\ngzip\nzip\nunzip\ndu\nmd5sum\nsha1sum\nsha256sum\nsha512sum\ntransferly-probe\n"

// answer is what a scripted command prints and how it exits.
type answer struct {
	stdout, stderr string
	status         int
}

// script is a server whose commands answer from a function, and that records the command lines it ran after the probe.
type script struct {
	mu    sync.Mutex
	ran   []string
	reply func(command string) answer
}

// last returns the last command line the server ran.
func (s *script) last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ran) == 0 {
		return ""
	}
	return s.ran[len(s.ran)-1]
}

// scripted starts a server whose probe prints probe and whose other commands answer with reply, and connects to it.
func scripted(t *testing.T, probe string, reply func(command string) answer) (*sftpfs.FS, *script, *sftptest.Server) {
	t.Helper()
	sc := &script{reply: reply}
	s := startServer(t, sftptest.Options{Exec: func(command string, stdout, stderr io.Writer) int {
		if strings.HasPrefix(command, "sh -c 'for t in ") {
			_, _ = io.WriteString(stdout, probe)
			return 0
		}
		sc.mu.Lock()
		sc.ran = append(sc.ran, command)
		sc.mu.Unlock()
		a := sc.reply(command)
		_, _ = io.WriteString(stdout, a.stdout)
		_, _ = io.WriteString(stderr, a.stderr)
		return a.status
	}})
	return dial(t, s), sc, s
}

// ok answers every command with success and the given output.
func ok(stdout string) func(string) answer {
	return func(string) answer { return answer{stdout: stdout} }
}

// failing answers every command with a failure.
func failing(stderr string, status int) func(string) answer {
	return func(string) answer { return answer{stderr: stderr, status: status} }
}

// TestCommands_Probe verifies how the tools of a server are learned.
//
// Use cases:
//   - every tool found is offered; tar without gzip offers no tar.gz, and zip and unzip are told apart
//   - a server that refuses commands, or answers without the marker as servers restricted to SFTP do, offers nothing
//   - a probe that exits with a failure, as when the login shell is not a POSIX one, offers nothing
//   - the probe runs once per connection, and an ended context or a closed connection fails it and the operations
func TestCommands_Probe(t *testing.T) {
	fsys, _, _ := scripted(t, allTools, ok(""))
	tools, err := fsys.Tools(t.Context())
	if err != nil || !tools.Copy || !tools.Usage || strings.Join(tools.Checksums, ",") != "md5,sha1,sha256,sha512" ||
		strings.Join(tools.Archive, ",") != "zip,tar.gz" || strings.Join(tools.Extract, ",") != "zip,tar.gz" {
		t.Errorf("Tools() = %+v, %v", tools, err)
	}
	partial, _, _ := scripted(t, "tar\nunzip\nsha256sum\ntransferly-probe\n", ok(""))
	if tools, _ := partial.Tools(t.Context()); tools.Copy || len(tools.Archive) != 0 || strings.Join(tools.Extract, ",") != "zip" || strings.Join(tools.Checksums, ",") != "sha256" {
		t.Errorf("Tools(partial) = %+v", tools)
	}
	foreign, _, _ := scripted(t, "cp\ntar\n", ok(""))
	if tools, _ := foreign.Tools(t.Context()); tools.Copy {
		t.Errorf("Tools(without marker) = %+v, want none", tools)
	}
	refusing := dial(t, startServer(t, sftptest.Options{}))
	if tools, err := refusing.Tools(t.Context()); err != nil || tools.Copy {
		t.Errorf("Tools(refusing) = %+v, %v, want none", tools, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fsys.Tools(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Tools(canceled) = %v", err)
	}
	_ = fsys.Close()
	if _, err := fsys.Tools(t.Context()); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Tools(closed) = %v", err)
	}
	if err := fsys.ServerCopy(t.Context(), "/a", "/b", true); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("ServerCopy(closed) = %v", err)
	}

	failed := dial(t, startServer(t, sftptest.Options{Exec: func(_ string, stdout, _ io.Writer) int {
		_, _ = io.WriteString(stdout, "cp\ntransferly-probe\n")
		return 1
	}}))
	if tools, err := failed.Tools(t.Context()); err != nil || tools.Copy {
		t.Errorf("Tools(probe exiting with 1) = %+v, %v, want none", tools, err)
	}

	hung := make(chan struct{})
	t.Cleanup(func() { close(hung) })
	closing := dial(t, startServer(t, sftptest.Options{Exec: func(string, io.Writer, io.Writer) int {
		<-hung
		return 0
	}}))
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = closing.Close()
	}()
	if _, err := closing.Tools(t.Context()); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Tools(closed during the probe) = %v, want ErrConnectionLost", err)
	}
}

// TestCommands_Lines verifies the command lines the operations send, with every path quoted.
//
// Use cases:
//   - a copy that may not overwrite refuses an existing target first; one that may does not
//   - checksums, usage, archives in both formats and extraction in both formats run their programs on quoted paths
func TestCommands_Lines(t *testing.T) {
	fsys, sc, _ := scripted(t, allTools, func(command string) answer {
		switch {
		case strings.HasPrefix(command, "sha256sum"):
			return answer{stdout: `\` + strings.Repeat("ab", 32) + "  /it's\n"}
		case strings.HasPrefix(command, "du"):
			return answer{stdout: "12\t/dir\n"}
		}
		return answer{}
	})
	ctx := t.Context()
	if err := fsys.ServerCopy(ctx, "/a b", "/c'd", false); err != nil {
		t.Fatal(err)
	}
	if want := `if [ -e '/c'\''d' ] || [ -L '/c'\''d' ]; then echo 'File exists' >&2; exit 1; fi; cp -p '/a b' '/c'\''d'`; sc.last() != want {
		t.Errorf("copy = %s", sc.last())
	}
	_ = fsys.ServerCopy(ctx, "/a", "/b", true)
	if sc.last() != "cp -p '/a' '/b'" {
		t.Errorf("copy with overwrite = %s", sc.last())
	}
	if sum, err := fsys.ServerChecksum(ctx, "/it's", "sha256"); err != nil || sum != strings.Repeat("ab", 32) || sc.last() != `sha256sum '/it'\''s'` {
		t.Errorf("checksum = %s, %v by %s", sum, err, sc.last())
	}
	if used, err := fsys.ServerUsage(ctx, "/dir/"); err != nil || used != 12*1024 || sc.last() != "du -sk '/dir'" {
		t.Errorf("usage = %d, %v by %s", used, err, sc.last())
	}
	_ = fsys.ServerArchive(ctx, "/d", []string{"-rf", "x y"}, "/out.zip", remotefs.FormatZip)
	if want := `if [ -e '/out.zip' ] || [ -L '/out.zip' ]; then echo 'File exists' >&2; exit 1; fi; cd '/d' && zip -q -r -y '/out.zip' './-rf' './x y'`; sc.last() != want {
		t.Errorf("zip = %s", sc.last())
	}
	_ = fsys.ServerArchive(ctx, "/d", []string{"a"}, "/out.tar.gz", remotefs.FormatTarGz)
	if !strings.HasSuffix(sc.last(), "tar -czf '/out.tar.gz' -C '/d' './a'") {
		t.Errorf("tar = %s", sc.last())
	}
	_ = fsys.ServerExtract(ctx, "/out.zip", remotefs.FormatZip, "/x")
	if sc.last() != "unzip -q -n '/out.zip' -d '/x'" {
		t.Errorf("unzip = %s", sc.last())
	}
	_ = fsys.ServerExtract(ctx, "/out.tar.gz", remotefs.FormatTarGz, "/x")
	if sc.last() != "tar -xzkf '/out.tar.gz' -C '/x'" {
		t.Errorf("untar = %s", sc.last())
	}
}

// TestCommands_Refusals verifies what the operations refuse before running anything, and how failures read.
//
// Use cases:
//   - relative paths, unknown algorithms, archives of nothing or of names with a slash are invalid
//   - what the server lacks is unsupported
//   - failures read by their standard error, a missing program is unsupported, and unreadable outputs are unsupported
//   - unzip warnings and files tar kept count as success
func TestCommands_Refusals(t *testing.T) {
	ctx := t.Context()
	fsys, _, _ := scripted(t, allTools, ok("garbage"))
	for name, err := range map[string]error{
		"copy from":      fsys.ServerCopy(ctx, "rel", "/b", false),
		"copy to":        fsys.ServerCopy(ctx, "/a", "rel", true),
		"copy to (new)":  fsys.ServerCopy(ctx, "/a", "rel\x00", false),
		"checksum path":  second(fsys.ServerChecksum(ctx, "rel", "md5")),
		"algorithm":      second(fsys.ServerChecksum(ctx, "/a", "crc32")),
		"usage path":     second(fsys.ServerUsage(ctx, "rel")),
		"archive none":   fsys.ServerArchive(ctx, "/d", nil, "/o.zip", remotefs.FormatZip),
		"archive slash":  fsys.ServerArchive(ctx, "/d", []string{"a/b"}, "/o.zip", remotefs.FormatZip),
		"archive dots":   fsys.ServerArchive(ctx, "/d", []string{".."}, "/o.zip", remotefs.FormatZip),
		"archive nul":    fsys.ServerArchive(ctx, "/d", []string{"a\x00"}, "/o.zip", remotefs.FormatZip),
		"archive dir":    fsys.ServerArchive(ctx, "rel", []string{"a"}, "/o.zip", remotefs.FormatZip),
		"archive target": fsys.ServerArchive(ctx, "/d", []string{"a"}, "rel", remotefs.FormatZip),
		"extract source": fsys.ServerExtract(ctx, "rel", remotefs.FormatZip, "/d"),
		"extract target": fsys.ServerExtract(ctx, "/a.zip", remotefs.FormatZip, "rel"),
	} {
		if !errors.Is(err, remotefs.ErrInvalid) {
			t.Errorf("%s = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := fsys.ServerChecksum(ctx, "/a", "sha1"); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Errorf("checksum with garbage output = %v, want ErrUnsupported", err)
	}
	if _, err := fsys.ServerUsage(ctx, "/a"); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Errorf("usage with garbage output = %v, want ErrUnsupported", err)
	}
	empty, _, _ := scripted(t, allTools, ok(""))
	if _, err := empty.ServerChecksum(ctx, "/a", "md5"); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Errorf("checksum with no output = %v, want ErrUnsupported", err)
	}
	if _, err := empty.ServerUsage(ctx, "/a"); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Errorf("usage with no output = %v, want ErrUnsupported", err)
	}

	bare, _, _ := scripted(t, "transferly-probe\n", ok(""))
	for name, err := range map[string]error{
		"copy":     bare.ServerCopy(ctx, "/a", "/b", false),
		"checksum": second(bare.ServerChecksum(ctx, "/a", "sha256")),
		"usage":    second(bare.ServerUsage(ctx, "/a")),
		"archive":  bare.ServerArchive(ctx, "/d", []string{"a"}, "/o.zip", remotefs.FormatZip),
		"extract":  bare.ServerExtract(ctx, "/a.tar.gz", remotefs.FormatTarGz, "/d"),
	} {
		if !errors.Is(err, remotefs.ErrUnsupported) {
			t.Errorf("%s without its tool = %v, want ErrUnsupported", name, err)
		}
	}

	for stderr, want := range map[string]error{
		"cp: cannot stat '/a': No such file or directory":   remotefs.ErrNotFound,
		"cp: cannot create regular file: Permission denied": remotefs.ErrPermission,
		"File exists": remotefs.ErrExists,
		"cp: error writing: No space left on device": remotefs.ErrQuota,
		"cp: target '/b/': Not a directory":          remotefs.ErrNotDirectory,
		"something else went wrong":                  remotefs.ErrInvalid,
	} {
		broken, _, _ := scripted(t, allTools, failing(stderr, 1))
		if err := broken.ServerCopy(ctx, "/a", "/b", true); !errors.Is(err, want) || !strings.Contains(err.Error(), "status 1") {
			t.Errorf("copy failing with %q = %v, want %v", stderr, err, want)
		}
	}
	missing, _, _ := scripted(t, allTools, failing("sh: zip: not found", 127))
	for name, err := range map[string]error{
		"checksum": second(missing.ServerChecksum(ctx, "/a", "md5")),
		"usage":    second(missing.ServerUsage(ctx, "/a")),
		"archive":  missing.ServerArchive(ctx, "/d", []string{"a"}, "/o.zip", remotefs.FormatZip),
		"extract":  missing.ServerExtract(ctx, "/a.zip", remotefs.FormatZip, "/d"),
	} {
		if !errors.Is(err, remotefs.ErrUnsupported) {
			t.Errorf("%s with a missing program = %v, want ErrUnsupported", name, err)
		}
	}

	warned, _, _ := scripted(t, allTools, failing("warning: skipped ../x", 1))
	if err := warned.ServerExtract(ctx, "/a.zip", remotefs.FormatZip, "/d"); err != nil {
		t.Errorf("unzip with a warning = %v, want success", err)
	}
	kept, _, _ := scripted(t, allTools, failing("tar: ./a: Cannot open: File exists\ntar: Exiting with failure status due to previous errors\n", 2))
	if err := kept.ServerExtract(ctx, "/a.tar.gz", remotefs.FormatTarGz, "/d"); err != nil {
		t.Errorf("tar that kept files = %v, want success", err)
	}
	for _, stderr := range []string{"tar: ./a: Cannot open: Permission denied\n", "tar: Exiting with failure status due to previous errors\n"} {
		broken, _, _ := scripted(t, allTools, failing(stderr, 2))
		if err := broken.ServerExtract(ctx, "/a.tar.gz", remotefs.FormatTarGz, "/d"); err == nil {
			t.Errorf("tar failing with %q = nil, want a failure", stderr)
		}
	}
}

// TestCommands_Lifecycle verifies commands whose context ends, whose connection closes or drops, and servers that refuse them.
//
// Use cases:
//   - an ended context kills a running command
//   - closing the connection while a command runs fails it as a lost connection
//   - a server that refuses commands makes every operation unsupported
//   - a connection the server dropped cannot open a session, which reads as unsupported for the caller to fall back
//   - output beyond the cap is dropped
func TestCommands_Lifecycle(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	slow, _, _ := scripted(t, allTools, func(string) answer {
		select {
		case <-release:
		case <-time.After(10 * time.Second):
		}
		return answer{}
	})
	if _, err := slow.Tools(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := slow.ServerCopy(ctx, "/a", "/b", true); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("copy past its deadline = %v", err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = slow.Close()
	}()
	if err := slow.ServerCopy(t.Context(), "/a", "/b", true); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("copy while closing = %v, want ErrConnectionLost", err)
	}

	big, _, s := scripted(t, allTools, ok(strings.Repeat("9", 1<<20)))
	if _, err := big.ServerUsage(t.Context(), "/a"); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Errorf("usage with a huge number = %v, want ErrUnsupported", err)
	}
	s.DropConnections()
	time.Sleep(50 * time.Millisecond)
	if err := big.ServerCopy(t.Context(), "/a", "/b", true); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Errorf("copy on a dropped connection = %v, want ErrUnsupported", err)
	}
}
