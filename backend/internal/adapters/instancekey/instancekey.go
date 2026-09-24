// Package instancekey provides the random secret that identifies an instance.
package instancekey

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// Length is the size of an instance key in bytes.
	Length   = 32
	fileName = "instance.key"
)

// Source tells where a resolved key came from.
type Source string

const (
	// FromConfig means the key was supplied through configuration.
	FromConfig Source = "config"
	// FromFile means the key was read from the data directory.
	FromFile Source = "file"
	// Created means the key was generated and stored during this call.
	Created Source = "created"
)

// Resolve returns the instance key.
//
// A configured key wins. Otherwise the key stored in dataDir is used, and when there is none a
// new key is read from random, stored with mode 0600 and returned. Concurrent creators agree on
// the first key written.
func Resolve(dataDir string, configured []byte, random io.Reader) ([]byte, Source, error) {
	return resolve(dataDir, configured, random, createExclusive)
}

// creator creates a new file that must not exist yet and returns it for writing.
type creator func(path string) (io.WriteCloser, error)

// createExclusive creates path with owner-only access, failing if it already exists.
func createExclusive(path string) (io.WriteCloser, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

// resolve implements [Resolve] with an injectable file creator.
func resolve(dataDir string, configured []byte, random io.Reader, create creator) ([]byte, Source, error) {
	if configured != nil {
		return configured, FromConfig, nil
	}

	path := filepath.Join(dataDir, fileName)
	key, err := read(path)
	if err == nil {
		return key, FromFile, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, "", err
	}

	key = make([]byte, Length)
	if _, err := io.ReadFull(random, key); err != nil {
		return nil, "", fmt.Errorf("generating instance key: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("creating data directory: %w", err)
	}

	file, err := create(path)
	if errors.Is(err, fs.ErrExist) {
		winner, readErr := read(path)
		return winner, FromFile, readErr
	}
	if err != nil {
		return nil, "", fmt.Errorf("creating instance key file: %w", err)
	}
	_, writeErr := io.WriteString(file, hex.EncodeToString(key)+"\n")
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return nil, "", fmt.Errorf("writing instance key file: %w", err)
	}
	return key, Created, nil
}

// read loads and validates the key stored at path.
func read(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != Length {
		return nil, fmt.Errorf("instance key file %s is corrupt: want %d hex-encoded bytes", path, Length)
	}
	return key, nil
}
