package instancekey_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fprambs/transferly/internal/adapters/instancekey"
)

// fixedRandom returns a reader that yields the same bytes on every read.
func fixedRandom(b byte) io.Reader {
	return bytes.NewReader(bytes.Repeat([]byte{b}, instancekey.Length))
}

// errReader is a reader that always fails.
type errReader struct{}

// Read fails with a fixed error.
func (errReader) Read([]byte) (int, error) { return 0, errors.New("entropy exhausted") }

// racingReader creates the key file while the key is being generated, simulating a concurrent instance.
type racingReader struct {
	path    string
	content string
}

// Read writes the competing key file, then returns random bytes.
func (r racingReader) Read(p []byte) (int, error) {
	if err := os.WriteFile(r.path, []byte(r.content), 0o600); err != nil {
		return 0, err
	}
	for i := range p {
		p[i] = 0xaa
	}
	return len(p), nil
}

// TestResolve_CreatesAndReusesTheKey verifies the lifecycle of the stored key.
//
// Scenarios:
//   - the first call generates a key, stores it privately and reports it as created
//   - the second call returns the same key from the file
//   - the data directory is created with owner-only access when missing
func TestResolve_CreatesAndReusesTheKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")

	first, source, err := instancekey.Resolve(dir, nil, fixedRandom(0x11))
	if err != nil || source != instancekey.Created {
		t.Fatalf("first Resolve() = %v, %q, want created", err, source)
	}
	second, source, err := instancekey.Resolve(dir, nil, errReader{})
	if err != nil || source != instancekey.FromFile || !bytes.Equal(first, second) {
		t.Fatalf("second Resolve() = %v, %q, equal=%v, want the stored key", err, source, bytes.Equal(first, second))
	}

	info, err := os.Stat(filepath.Join(dir, "instance.key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %v (%v), want 0600", info.Mode().Perm(), err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("data directory mode = %v (%v), want 0700", dirInfo.Mode().Perm(), err)
	}
}

// TestResolve_ConfiguredKeyWins verifies that a configured key is returned untouched and nothing is written.
func TestResolve_ConfiguredKeyWins(t *testing.T) {
	dir := t.TempDir()
	configured := bytes.Repeat([]byte{0x22}, instancekey.Length)

	key, source, err := instancekey.Resolve(dir, configured, errReader{})

	if err != nil || source != instancekey.FromConfig || !bytes.Equal(key, configured) {
		t.Fatalf("Resolve() = %v, %q, want the configured key", err, source)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("data directory has %d entries, want none", len(entries))
	}
}

// TestResolve_Failures verifies the error paths.
//
// Scenarios:
//   - a stored key that is not valid hexadecimal
//   - a stored key of the wrong length
//   - a key path that cannot be read as a file
//   - the random source failing
//   - a data directory located under a regular file
//   - a data directory that cannot be created because its parent is read-only
//   - a key file that cannot be created in an unwritable directory
func TestResolve_Failures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) (dir string, random io.Reader)
		want  string
	}{
		{"not hexadecimal", func(t *testing.T) (string, io.Reader) {
			return writeKey(t, "not-hex\n"), fixedRandom(1)
		}, "corrupt"},
		{"wrong length", func(t *testing.T) (string, io.Reader) {
			return writeKey(t, "abcd\n"), fixedRandom(1)
		}, "corrupt"},
		{"key path is a directory", func(t *testing.T) (string, io.Reader) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "instance.key"), 0o700); err != nil {
				t.Fatal(err)
			}
			return dir, fixedRandom(1)
		}, "is a directory"},
		{"random source fails", func(t *testing.T) (string, io.Reader) {
			return t.TempDir(), errReader{}
		}, "entropy exhausted"},
		{"data directory under a file", func(t *testing.T) (string, io.Reader) {
			blocker := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(blocker, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(blocker, "data"), fixedRandom(1)
		}, "not a directory"},
		{"data directory cannot be created", func(t *testing.T) (string, io.Reader) {
			parent := t.TempDir()
			if err := os.Chmod(parent, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
			return filepath.Join(parent, "data"), fixedRandom(1)
		}, "creating data directory"},
		{"key file cannot be created", func(t *testing.T) (string, io.Reader) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
			return dir, fixedRandom(1)
		}, "creating instance key file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, random := tt.setup(t)
			if os.Geteuid() == 0 && strings.Contains(tt.name, "cannot be created") {
				t.Skip("root ignores directory permissions")
			}

			_, _, err := instancekey.Resolve(dir, nil, random)

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Resolve() = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

// writeKey creates a data directory whose key file has the given content.
func writeKey(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "instance.key"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestResolve_ConcurrentCreatorWins verifies that when another instance stores a key first,
// Resolve returns that key instead of overwriting it, and reports an error if that key is corrupt.
func TestResolve_ConcurrentCreatorWins(t *testing.T) {
	dir := t.TempDir()
	winner := strings.Repeat("ab", instancekey.Length)

	key, source, err := instancekey.Resolve(dir, nil, racingReader{path: filepath.Join(dir, "instance.key"), content: winner + "\n"})

	if err != nil || source != instancekey.FromFile || len(key) != instancekey.Length || key[0] != 0xab {
		t.Fatalf("Resolve() = %x, %q, %v, want the winner's key", key, source, err)
	}

	corruptDir := t.TempDir()
	_, _, err = instancekey.Resolve(corruptDir, nil, racingReader{path: filepath.Join(corruptDir, "instance.key"), content: "junk\n"})
	if err == nil {
		t.Error("Resolve() with a corrupt concurrent key = nil error, want error")
	}
}
