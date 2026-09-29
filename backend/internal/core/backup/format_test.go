package backup_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/fprambs/sentinel/internal/core/backup"
	"github.com/fprambs/sentinel/internal/core/vault"
)

const password = "a long export password"

// cheap is an Argon2id cost fast enough for tests.
var cheap = vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}

// newSealer returns a sealer with the cheap cost.
func newSealer(random io.Reader) *backup.Sealer {
	return backup.NewSealer(vault.NewKDF(2), cheap, random)
}

// sample is a content with one secret.
func sample() backup.Content {
	return backup.Content{Sites: []backup.Site{{Name: "Prod", Host: "h", Secrets: map[string]string{"password": "s3cret-value"}}}}
}

// rawFile is a file as its JSON reads, to change a part of it.
type rawFile struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	KDF     struct {
		Name      string `json:"name"`
		MemoryKiB uint32 `json:"memoryKiB"`
		Time      uint32 `json:"time"`
		Threads   uint8  `json:"threads"`
		Salt      []byte `json:"salt"`
	} `json:"kdf"`
	Data []byte `json:"data"`
}

// sealed returns a sealed sample, decoded to change it.
func sealed(t *testing.T) rawFile {
	t.Helper()
	file, err := newSealer(rand.Reader).Seal(t.Context(), password, sample())
	if err != nil {
		t.Fatal(err)
	}
	var raw rawFile
	if err := json.Unmarshal(file, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

// encode writes a changed file back.
func encode(t *testing.T, raw rawFile) []byte {
	t.Helper()
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestSealer covers sealing and opening export files (ADR-0017).
//
// Use cases:
//   - a sealed file opens with its password into the same content, and its clear part holds neither the password nor a secret
//   - the file records the format, version, Argon2id cost and salt in clear text
//   - a wrong password, changed sealed data and a changed cost or salt are all ErrWrongPassword
//   - a password shorter than 12 characters or longer than 1 KiB is refused when sealing
func TestSealer(t *testing.T) {
	s := newSealer(rand.Reader)
	file, err := s.Seal(t.Context(), password, sample())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(file, []byte("s3cret-value")) || bytes.Contains(file, []byte(password)) || bytes.Contains(file, []byte("Prod")) {
		t.Error("the file shows its content in clear text")
	}
	raw := sealed(t)
	if raw.Format != backup.Format || raw.Version != backup.Version || raw.KDF.Name != "argon2id" || raw.KDF.MemoryKiB != 64 || len(raw.KDF.Salt) != vault.SaltLength {
		t.Errorf("header = %+v", raw)
	}
	content, err := s.Open(t.Context(), password, file)
	if err != nil || len(content.Sites) != 1 || content.Sites[0].Secrets["password"] != "s3cret-value" {
		t.Fatalf("Open() = %+v, %v", content, err)
	}

	if _, err := s.Open(t.Context(), "not the password at all", file); !errors.Is(err, backup.ErrWrongPassword) {
		t.Errorf("wrong password = %v", err)
	}
	flipped := sealed(t)
	flipped.Data[len(flipped.Data)-1] ^= 1
	slower := sealed(t)
	slower.KDF.Time = 2
	resalted := sealed(t)
	resalted.KDF.Salt[0] ^= 1
	for name, raw := range map[string]rawFile{"data": flipped, "cost": slower, "salt": resalted} {
		if _, err := s.Open(t.Context(), password, encode(t, raw)); !errors.Is(err, backup.ErrWrongPassword) {
			t.Errorf("changed %s = %v; want ErrWrongPassword", name, err)
		}
	}

	for _, weak := range []string{"short", strings.Repeat("x", 1025)} {
		if _, err := s.Seal(t.Context(), weak, sample()); !errors.Is(err, backup.ErrWeakPassword) {
			t.Errorf("Seal(%d chars) = %v; want ErrWeakPassword", len(weak), err)
		}
	}
}

// TestSealer_InvalidFiles covers the files that are refused before anything is derived (ADR-0017).
//
// Use cases:
//   - a file over 4 MiB is ErrTooLarge
//   - text that is not JSON, another format or version, and a password longer than 1 KiB are ErrInvalidFile
//   - a key derivation other than Argon2id, a cost above 256 MiB, 10 passes or 8 lanes, a salt of the wrong size and a cost
//     Argon2id cannot run are ErrInvalidFile
//   - sealed content that is not an export is ErrInvalidFile
func TestSealer_InvalidFiles(t *testing.T) {
	s := newSealer(rand.Reader)
	changes := map[string]func(*rawFile){
		"format":     func(r *rawFile) { r.Format = "other" },
		"version":    func(r *rawFile) { r.Version = 2 },
		"kdf":        func(r *rawFile) { r.KDF.Name = "scrypt" },
		"memory":     func(r *rawFile) { r.KDF.MemoryKiB = 256*1024 + 1 },
		"passes":     func(r *rawFile) { r.KDF.Time = 11 },
		"lanes":      func(r *rawFile) { r.KDF.Threads = 9 },
		"salt":       func(r *rawFile) { r.KDF.Salt = r.KDF.Salt[:8] },
		"no threads": func(r *rawFile) { r.KDF.Threads = 0 },
	}
	for name, change := range changes {
		raw := sealed(t)
		change(&raw)
		if _, err := s.Open(t.Context(), password, encode(t, raw)); !errors.Is(err, backup.ErrInvalidFile) {
			t.Errorf("%s: Open() = %v; want ErrInvalidFile", name, err)
		}
	}
	if _, err := s.Open(t.Context(), password, make([]byte, backup.MaxBytes+1)); !errors.Is(err, backup.ErrTooLarge) {
		t.Errorf("too large = %v", err)
	}
	if _, err := s.Open(t.Context(), password, []byte("<xml/>")); !errors.Is(err, backup.ErrInvalidFile) {
		t.Errorf("not JSON = %v", err)
	}
	if _, err := s.Open(t.Context(), strings.Repeat("x", 1025), encode(t, sealed(t))); !errors.Is(err, backup.ErrInvalidFile) {
		t.Errorf("long password = %v", err)
	}

	raw := sealed(t)
	key, err := vault.NewKDF(1).Derive(t.Context(), password, raw.KDF.Salt, cheap)
	if err != nil {
		t.Fatal(err)
	}
	header := encode(t, rawFile{Format: raw.Format, Version: raw.Version, KDF: raw.KDF})
	aad := header[:bytes.LastIndex(header, []byte(`,"data"`))]
	if raw.Data, err = vault.Seal(rand.Reader, key, []byte("not an export"), append(aad, '}')); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(t.Context(), password, encode(t, raw)); !errors.Is(err, backup.ErrInvalidFile) {
		t.Errorf("sealed content that is not an export = %v", err)
	}
}

// failingReader gives n bytes and then fails.
type failingReader struct{ n int }

// Read implements [io.Reader].
func (f *failingReader) Read(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, errors.New("no randomness")
	}
	n := min(len(p), f.n)
	f.n -= n
	return n, nil
}

// TestSealer_Failures covers the failures of the machinery around a file.
//
// Use cases:
//   - no randomness for the salt or for the nonce fails sealing
//   - a context that ended fails sealing and opening with its error, not as a wrong password
func TestSealer_Failures(t *testing.T) {
	for _, n := range []int{0, vault.SaltLength} {
		if _, err := newSealer(&failingReader{n: n}).Seal(t.Context(), password, sample()); err == nil {
			t.Errorf("Seal() with %d random bytes did not fail", n)
		}
	}
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	s := newSealer(rand.Reader)
	if _, err := s.Seal(ended, password, sample()); !errors.Is(err, context.Canceled) {
		t.Errorf("Seal() canceled = %v", err)
	}
	if _, err := s.Open(ended, password, encode(t, sealed(t))); !errors.Is(err, context.Canceled) {
		t.Errorf("Open() canceled = %v", err)
	}
}
