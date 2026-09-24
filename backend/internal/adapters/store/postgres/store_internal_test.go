package postgres

import (
	"testing"
	"testing/fstest"
)

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
