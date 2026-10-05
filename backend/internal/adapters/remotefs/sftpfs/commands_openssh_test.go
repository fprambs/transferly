package sftpfs_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// hostile are names that would run something, split into several words or read as options if a command did not quote them.
var hostile = []string{"plain.txt", "with space.txt", "it's.txt", `$(touch pwned).txt`, "`touch pwned`.txt", "a;touch pwned", "new\nline.txt", "-rf", "*", `back\slash`, `"double".txt`}

// shell connects to the OpenSSH server with a shell named by TRANSFERLY_TEST_SSH_ADDR, as the ssh service of the test stacks is,
// and makes a folder of its own in the home of transferly, removed when the test ends; it skips the test when the variable is
// not set.
func shell(t *testing.T) (*sftpfs.FS, string) {
	t.Helper()
	address := os.Getenv("TRANSFERLY_TEST_SSH_ADDR")
	if address == "" {
		t.Skip("TRANSFERLY_TEST_SSH_ADDR is not set; start the ssh profile of the test stack to run this test")
	}
	fsys, err := sftpfs.Dial(t.Context(), sftpfs.Config{Address: address, Username: "transferly", Password: "transferly", VerifyHostKey: trustAll})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	root := "/home/transferly/" + uuid.NewString()
	if err := fsys.Mkdir(t.Context(), root); err != nil {
		t.Fatalf("Mkdir(%s) = %v", root, err)
	}
	t.Cleanup(func() {
		_ = fsys.RemoveAll(t.Context(), root)
		_ = fsys.Close()
	})
	return fsys, root
}

// store writes content to a file over SFTP.
func store(t *testing.T, fsys remotefs.RemoteFS, p, content string) {
	t.Helper()
	w, err := fsys.OpenWrite(t.Context(), p, 0, remotefs.WriteCreate)
	if err != nil {
		t.Fatalf("OpenWrite(%q) = %v", p, err)
	}
	_, _ = io.WriteString(w, content)
	if err := w.Close(); err != nil {
		t.Fatalf("Close(%q) = %v", p, err)
	}
}

// load reads a file over SFTP.
func load(t *testing.T, fsys remotefs.RemoteFS, p string) string {
	t.Helper()
	r, err := fsys.OpenRead(t.Context(), p, 0)
	if err != nil {
		t.Fatalf("OpenRead(%q) = %v", p, err)
	}
	defer func() { _ = r.Close() }()
	data, _ := io.ReadAll(r)
	return string(data)
}

// noPwned fails the test when a command ran something a name smuggled in.
func noPwned(t *testing.T, fsys remotefs.RemoteFS, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if _, err := fsys.Stat(t.Context(), path.Join(dir, "pwned")); !errors.Is(err, remotefs.ErrNotFound) {
			t.Errorf("a name ran a command in %s: %v", dir, err)
		}
	}
}

// TestCommands_OpenSSH runs the operations on the server against OpenSSH with bash, coreutils, tar, zip and unzip (ADR-0027).
//
// Scenarios:
//   - the session starts in the home folder of the account, as pwd tells it
//   - the probe finds every tool
//   - files with hostile names are copied, summed and archived as they are, and nothing they smuggle runs
//   - a copy that would replace a file is refused unless it may overwrite, and a missing source is not found
//   - archives in both formats extract back to the same files, keeping the files already in the folder; unzip writes control
//     characters of names as ^J and the like, so a name with a newline comes back renamed from a ZIP archive
//   - the usage of a folder is measured
func TestCommands_OpenSSH(t *testing.T) {
	fsys, root := shell(t)
	if home, err := fsys.Home(t.Context()); err != nil || home != "/home/transferly" {
		t.Errorf("Home() = %q, %v; want the home folder of the account", home, err)
	}
	tools, err := fsys.Tools(t.Context())
	want := remotefs.Tools{
		Copy: true, Usage: true, Checksums: []string{"md5", "sha1", "sha256", "sha512"},
		Archive: []string{remotefs.FormatZip, remotefs.FormatTarGz}, Extract: []string{remotefs.FormatZip, remotefs.FormatTarGz},
	}
	if err != nil || !slices.Equal(tools.Checksums, want.Checksums) || !slices.Equal(tools.Archive, want.Archive) || !slices.Equal(tools.Extract, want.Extract) || !tools.Copy || !tools.Usage {
		t.Fatalf("Tools() = %+v, %v; want %+v", tools, err, want)
	}

	src := path.Join(root, "src")
	if err := fsys.Mkdir(t.Context(), src); err != nil {
		t.Fatal(err)
	}
	copies := path.Join(root, "copies")
	if err := fsys.Mkdir(t.Context(), copies); err != nil {
		t.Fatal(err)
	}
	for _, name := range hostile {
		content := "content of " + name
		store(t, fsys, path.Join(src, name), content)
		if err := fsys.ServerCopy(t.Context(), path.Join(src, name), path.Join(copies, name), false); err != nil {
			t.Errorf("ServerCopy(%q) = %v", name, err)
			continue
		}
		if got := load(t, fsys, path.Join(copies, name)); got != content {
			t.Errorf("copy of %q = %q", name, got)
		}
		digest := sha256.Sum256([]byte(content))
		if sum, err := fsys.ServerChecksum(t.Context(), path.Join(src, name), "sha256"); err != nil || sum != hex.EncodeToString(digest[:]) {
			t.Errorf("ServerChecksum(%q) = %s, %v", name, sum, err)
		}
	}
	noPwned(t, fsys, root, src, copies, "/home/transferly")

	first := path.Join(src, hostile[0])
	if err := fsys.ServerCopy(t.Context(), first, path.Join(copies, hostile[1]), false); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("ServerCopy onto a file = %v, want ErrExists", err)
	}
	if err := fsys.ServerCopy(t.Context(), first, path.Join(copies, hostile[1]), true); err != nil || load(t, fsys, path.Join(copies, hostile[1])) != "content of "+hostile[0] {
		t.Errorf("ServerCopy with overwrite = %v", err)
	}
	if err := fsys.ServerCopy(t.Context(), path.Join(src, "missing"), path.Join(copies, "x"), false); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("ServerCopy of a missing file = %v, want ErrNotFound", err)
	}

	for _, format := range []string{remotefs.FormatZip, remotefs.FormatTarGz} {
		archive := path.Join(root, "bundle."+format)
		if err := fsys.ServerArchive(t.Context(), src, hostile, archive, format); err != nil {
			t.Fatalf("ServerArchive(%s) = %v", format, err)
		}
		if err := fsys.ServerArchive(t.Context(), src, hostile, archive, format); !errors.Is(err, remotefs.ErrExists) {
			t.Errorf("ServerArchive(%s) onto an archive = %v, want ErrExists", format, err)
		}
		out := path.Join(root, "out-"+format)
		if err := fsys.Mkdir(t.Context(), out); err != nil {
			t.Fatal(err)
		}
		store(t, fsys, path.Join(out, hostile[0]), "kept")
		if err := fsys.ServerExtract(t.Context(), archive, format, out); err != nil {
			t.Fatalf("ServerExtract(%s) = %v", format, err)
		}
		if got := load(t, fsys, path.Join(out, hostile[0])); got != "kept" {
			t.Errorf("extract (%s) replaced a file: %q", format, got)
		}
		for _, name := range hostile[1:] {
			if format == remotefs.FormatZip && strings.ContainsRune(name, '\n') {
				continue
			}
			if got := load(t, fsys, path.Join(out, name)); got != "content of "+name {
				t.Errorf("extracted (%s) %q = %q", format, name, got)
			}
		}
		noPwned(t, fsys, root, out)
	}

	if used, err := fsys.ServerUsage(t.Context(), src); err != nil || used <= 0 {
		t.Errorf("ServerUsage() = %d, %v", used, err)
	}
}

// TestCommands_SFTPOnly verifies that a server restricted to SFTP, as the sftp service of the test stacks is, offers no tool, and
// that every operation on it says so, for the caller to fall back. It is skipped unless TRANSFERLY_TEST_SFTP_ADDR is set.
func TestCommands_SFTPOnly(t *testing.T) {
	address := os.Getenv("TRANSFERLY_TEST_SFTP_ADDR")
	if address == "" {
		t.Skip("TRANSFERLY_TEST_SFTP_ADDR is not set; start the testenv profile to run this test")
	}
	fsys, err := sftpfs.Dial(t.Context(), sftpfs.Config{Address: address, Username: "transferly", Password: "transferly", VerifyHostKey: trustAll})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	defer func() { _ = fsys.Close() }()
	if tools, err := fsys.Tools(t.Context()); err != nil || tools.Copy || len(tools.Checksums) > 0 {
		t.Errorf("Tools() = %+v, %v; want none", tools, err)
	}
	if err := fsys.ServerCopy(t.Context(), "/upload/a", "/upload/b", false); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Errorf("ServerCopy() = %v, want ErrUnsupported", err)
	}
	if _, err := fsys.Stat(t.Context(), "/upload"); err != nil {
		t.Errorf("the connection broke after the probe: %v", err)
	}
}

// sudoed connects to the OpenSSH server named by TRANSFERLY_TEST_SSH_ADDR in sudo mode, as user with password, asking sudo for root
// with sudoPassword; it skips the test when the variable is not set.
func sudoed(t *testing.T, user, password, sudoPassword string) (*sftpfs.FS, error) {
	t.Helper()
	address := os.Getenv("TRANSFERLY_TEST_SSH_ADDR")
	if address == "" {
		t.Skip("TRANSFERLY_TEST_SSH_ADDR is not set; start the ssh profile of the test stack to run this test")
	}
	fsys, err := sftpfs.Dial(t.Context(), sftpfs.Config{
		Address: address, Username: user, Password: password, VerifyHostKey: trustAll,
		Sudo: &sftpfs.Sudo{User: "root", Password: sudoPassword},
	})
	if err == nil {
		t.Cleanup(func() { _ = fsys.Close() })
	}
	return fsys, err
}

// TestSudo_OpenSSH runs the sudo mode against OpenSSH with sudo (ADR-0027).
//
// Scenarios:
//   - an account that sudo asks for its password reads a file only root may read, once the password is given
//   - an account that may use sudo without a password needs none
//   - a wrong password and a missing one are refused, as elevation refused, without sending anything else
//   - a session in sudo mode offers no commands on the server
func TestSudo_OpenSSH(t *testing.T) {
	for _, account := range []struct{ user, password, sudo string }{{"transferly", "transferly", "transferly"}, {"deployer", "deployer", ""}} {
		fsys, err := sudoed(t, account.user, account.password, account.sudo)
		if err != nil {
			t.Fatalf("Dial(%s) in sudo mode = %v", account.user, err)
		}
		if got := load(t, fsys, "/root/only-root.txt"); got != "Only root reads this.\n" {
			t.Errorf("%s read /root/only-root.txt as %q", account.user, got)
		}
		if tools, err := fsys.Tools(t.Context()); err != nil || tools.Copy {
			t.Errorf("Tools() in sudo mode = %+v, %v; want none", tools, err)
		}
	}
	for name, sudo := range map[string]string{"wrong": "nope", "missing": ""} {
		if _, err := sudoed(t, "transferly", "transferly", sudo); !errors.Is(err, remotefs.ErrElevationRefused) {
			t.Errorf("Dial() with a %s sudo password = %v, want ErrElevationRefused", name, err)
		}
	}
}
