package postgres

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3/lock"
)

// TestNewProvider_ReportsLockFailures verifies that a failure to create the migration lock is reported.
func TestNewProvider_ReportsLockFailures(t *testing.T) {
	original := newLocker
	newLocker = func(...lock.SessionLockerOption) (lock.SessionLocker, error) {
		return nil, errors.New("lock unavailable")
	}
	t.Cleanup(func() { newLocker = original })

	_, _, err := newProvider("postgres://u:p@127.0.0.1:1/db", fstest.MapFS{})

	if err == nil || !strings.Contains(err.Error(), "migration lock") {
		t.Errorf("newProvider() = %v, want a migration lock error", err)
	}
}

// TestNewProvider_RejectsBadInputs verifies the errors of newProvider.
//
// Scenarios:
//   - a malformed database URL
//   - a migrations file system without any migration
func TestNewProvider_RejectsBadInputs(t *testing.T) {
	tests := []struct {
		name string
		url  string
		fsys fstest.MapFS
	}{
		{"malformed database url", "not a url", fstest.MapFS{"1_x.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")}}},
		{"no migrations", "postgres://u:p@127.0.0.1:1/db", fstest.MapFS{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := newProvider(tt.url, tt.fsys); err == nil {
				t.Error("newProvider() = nil error, want error")
			}
		})
	}
}
