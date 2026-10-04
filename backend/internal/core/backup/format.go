// Package backup exports the sites of a workspace, with their secrets, to a file sealed with a password of its own, and reads
// that file back (ADR-0017). The password derives a key with Argon2id and the key seals the content with AES-256-GCM; the
// workspace key never leaves the server.
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/fprambs/transferly/internal/core/vault"
)

const (
	// Format names the files of this package.
	Format = "transferly-export"
	// Version is the version of the format this package writes and reads. Version 2 keeps every site as its connector and configuration
	// (ADR-0022); files of version 1 are refused.
	Version = 2
	// MaxBytes is the largest file that is opened.
	MaxBytes = 4 << 20
	// kdfName is the only key derivation the format uses.
	kdfName = "argon2id"
	// MinPasswordRunes is the shortest export password, the length the master password needs too.
	MinPasswordRunes = 12
	// maxPasswordBytes bounds the password handed to the key derivation.
	maxPasswordBytes = 1024
	// maxMemoryKiB, maxTime and maxThreads cap the cost a file may ask for, so a crafted file cannot exhaust the server.
	maxMemoryKiB = 256 * 1024
	maxTime      = 10
	maxThreads   = 8
)

var (
	// ErrWeakPassword means the export password is shorter than [MinPasswordRunes] or too long.
	ErrWeakPassword = errors.New("the export password does not meet the policy")
	// ErrWrongPassword means the password does not open the file, or the file was changed.
	ErrWrongPassword = errors.New("wrong export password or tampered file")
	// ErrTooLarge means the file is larger than [MaxBytes].
	ErrTooLarge = errors.New("the export file is too large")
	// ErrInvalidFile means the file is not an export this version can read.
	ErrInvalidFile = errors.New("not a Transferly export")
)

// kdf is the key derivation recorded in a file.
type kdf struct {
	Name      string `json:"name"`
	MemoryKiB uint32 `json:"memoryKiB"`
	Time      uint32 `json:"time"`
	Threads   uint8  `json:"threads"`
	Salt      []byte `json:"salt"`
}

// header is the clear part of a file, bound to the sealed content as its additional data.
type header struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	KDF     kdf    `json:"kdf"`
}

// envelope is a whole file: its header and the sealed content.
type envelope struct {
	header
	Data []byte `json:"data"`
}

// aad returns the additional data a header binds to the content.
func (h header) aad() []byte {
	data, _ := json.Marshal(h)
	return data
}

// params returns the Argon2id cost of the header, refusing one above the caps.
func (h header) params() (vault.Params, error) {
	k := h.KDF
	if k.Name != kdfName || k.MemoryKiB > maxMemoryKiB || k.Time > maxTime || k.Threads > maxThreads || len(k.Salt) != vault.SaltLength {
		return vault.Params{}, ErrInvalidFile
	}
	return vault.Params{MemoryKiB: k.MemoryKiB, Time: k.Time, Threads: k.Threads}, nil
}

// Sealer seals and opens export files.
type Sealer struct {
	kdf    *vault.KDF
	params vault.Params
	random io.Reader
}

// NewSealer returns a sealer that derives keys with kdf at the cost params and draws salts and nonces from random.
func NewSealer(kdf *vault.KDF, params vault.Params, random io.Reader) *Sealer {
	return &Sealer{kdf: kdf, params: params, random: random}
}

// Seal writes content as a file sealed with password.
func (s *Sealer) Seal(ctx context.Context, password string, content Content) ([]byte, error) {
	if err := checkPassword(password); err != nil {
		return nil, err
	}
	salt := make([]byte, vault.SaltLength)
	if _, err := io.ReadFull(s.random, salt); err != nil {
		return nil, fmt.Errorf("reading salt: %w", err)
	}
	h := header{Format: Format, Version: Version, KDF: kdf{Name: kdfName, MemoryKiB: s.params.MemoryKiB, Time: s.params.Time, Threads: s.params.Threads, Salt: salt}}
	key, err := s.kdf.Derive(ctx, password, salt, s.params)
	if err != nil {
		return nil, err
	}
	defer vault.Wipe(key)
	plain, _ := json.Marshal(content)
	defer vault.Wipe(plain)
	sealed, err := vault.Seal(s.random, key, plain, h.aad())
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(envelope{header: h, Data: sealed}, "", "  ")
}

// Open reads a file sealed with password. A file over [MaxBytes] is [ErrTooLarge]; one that is not an export, or asks for a cost above
// the caps, is [ErrInvalidFile] before anything is derived; a wrong password and a changed file are [ErrWrongPassword].
func (s *Sealer) Open(ctx context.Context, password string, file []byte) (Content, error) {
	if len(file) > MaxBytes {
		return Content{}, ErrTooLarge
	}
	var e envelope
	if err := json.Unmarshal(file, &e); err != nil || e.Format != Format || e.Version != Version || len(password) > maxPasswordBytes {
		return Content{}, ErrInvalidFile
	}
	params, err := e.params()
	if err != nil {
		return Content{}, err
	}
	key, err := s.kdf.Derive(ctx, password, e.KDF.Salt, params)
	if errors.Is(err, vault.ErrInvalidParams) {
		return Content{}, ErrInvalidFile
	}
	if err != nil {
		return Content{}, err
	}
	defer vault.Wipe(key)
	plain, err := vault.Open(key, e.Data, e.aad())
	if err != nil {
		return Content{}, ErrWrongPassword
	}
	defer vault.Wipe(plain)
	var content Content
	if err := json.Unmarshal(plain, &content); err != nil {
		return Content{}, ErrInvalidFile
	}
	return content, nil
}
