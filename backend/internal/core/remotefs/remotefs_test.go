package remotefs_test

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/fprambs/transferly/internal/core/remotefs"
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

// TestHostKeyError verifies how a rejected host key matches the sentinels.
//
// Scenarios:
//   - an unseen key matches ErrHostKeyUnknown and not ErrHostKeyMismatch
//   - a changed key matches ErrHostKeyMismatch and not ErrHostKeyUnknown
//   - both kinds survive wrapping, are not retryable, and their message carries the type and fingerprint
func TestHostKeyError(t *testing.T) {
	unseen := &remotefs.HostKeyError{Type: "ssh-ed25519", Fingerprint: "SHA256:abc"}
	changed := &remotefs.HostKeyError{Type: "ssh-ed25519", Fingerprint: "SHA256:abc", Changed: true}

	if !errors.Is(unseen, remotefs.ErrHostKeyUnknown) || errors.Is(unseen, remotefs.ErrHostKeyMismatch) {
		t.Error("an unseen key must match only ErrHostKeyUnknown")
	}
	if !errors.Is(changed, remotefs.ErrHostKeyMismatch) || errors.Is(changed, remotefs.ErrHostKeyUnknown) {
		t.Error("a changed key must match only ErrHostKeyMismatch")
	}
	wrapped := fmt.Errorf("dial: %w", changed)
	var got *remotefs.HostKeyError
	if !errors.Is(wrapped, remotefs.ErrHostKeyMismatch) || !errors.As(wrapped, &got) || got.Fingerprint != "SHA256:abc" {
		t.Error("a wrapped key error lost its identity")
	}
	if remotefs.Retryable(unseen) || remotefs.Retryable(changed) {
		t.Error("a rejected host key must not be retried")
	}
	for _, e := range []*remotefs.HostKeyError{unseen, changed} {
		if msg := e.Error(); !strings.Contains(msg, "ssh-ed25519") || !strings.Contains(msg, "SHA256:abc") {
			t.Errorf("Error() = %q; want the type and fingerprint", msg)
		}
	}
}
