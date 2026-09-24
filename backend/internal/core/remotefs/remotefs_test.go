package remotefs_test

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// TestRetryable verifies which failures are worth retrying after a reconnect.
//
// Scenarios:
//   - a lost connection is retryable, also when wrapped
//   - authentication, host key, permission and lookup failures are not
//   - a nil error is not
func TestRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"connection lost", remotefs.ErrConnectionLost, true},
		{"wrapped connection lost", fmt.Errorf("list /a: %w", remotefs.ErrConnectionLost), true},
		{"authentication", remotefs.ErrAuth, false},
		{"host key mismatch", remotefs.ErrHostKeyMismatch, false},
		{"permission", remotefs.ErrPermission, false},
		{"not found", remotefs.ErrNotFound, false},
		{"unrelated", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := remotefs.Retryable(tt.err); got != tt.want {
				t.Errorf("Retryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestEntry_IsDir verifies that directories are recognized from the mode bits.
func TestEntry_IsDir(t *testing.T) {
	if !(remotefs.Entry{Mode: fs.ModeDir | 0o755}).IsDir() {
		t.Error("directory entry: IsDir() = false")
	}
	if (remotefs.Entry{Mode: 0o644}).IsDir() {
		t.Error("file entry: IsDir() = true")
	}
}
