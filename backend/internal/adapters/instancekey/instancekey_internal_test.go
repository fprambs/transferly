package instancekey

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// brokenFile is a file whose write or close can be made to fail.
type brokenFile struct {
	writeErr error
	closeErr error
}

// Write fails with the configured error.
func (f brokenFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

// Close fails with the configured error.
func (f brokenFile) Close() error { return f.closeErr }

// TestResolve_FileWriteFailures verifies that a failure while writing or closing the key file is reported.
//
// Scenarios:
//   - the write fails, for example because the disk is full
//   - the close fails after a successful write
func TestResolve_FileWriteFailures(t *testing.T) {
	tests := []struct {
		name string
		file brokenFile
		want string
	}{
		{"write fails", brokenFile{writeErr: errors.New("disk full")}, "disk full"},
		{"close fails", brokenFile{closeErr: errors.New("flush failed")}, "flush failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			create := func(string) (io.WriteCloser, error) { return tt.file, nil }

			_, _, err := resolve(t.TempDir(), nil, bytes.NewReader(bytes.Repeat([]byte{1}, Length)), create)

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("resolve() = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}
