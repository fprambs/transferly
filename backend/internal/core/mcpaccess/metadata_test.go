package mcpaccess

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// TestReadListing checks bounded output and suppression of data after revocation or remote failure.
func TestReadListing(t *testing.T) {
	private := errors.New("confidential remote diagnostic")
	for _, test := range []struct {
		name   string
		denyAt int
		remote error
		want   error
		reads  int
	}{
		{"approved", 0, nil, nil, 1},
		{"denied", 1, nil, ErrUnavailable, 0},
		{"revoked while reading", 2, nil, ErrUnavailable, 1},
		{"remote failure", 0, private, ErrReadFailed, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			checks, reads := 0, 0
			admit := func(context.Context) error {
				checks++
				if checks == test.denyAt {
					return private
				}
				return nil
			}
			read := func(context.Context) ([]remotefs.Entry, error) {
				reads++
				entries := make([]remotefs.Entry, 50_000)
				entries[0] = remotefs.Entry{Name: "Untrusted text in a filename", Mode: 0o600}
				entries[1].Mode = fs.ModeDir
				entries[2].Mode = fs.ModeSymlink
				entries[3].Mode = fs.ModeDevice
				return entries, test.remote
			}
			result, err := ReadListing(t.Context(), admit, read)
			if err != test.want || reads != test.reads {
				t.Fatalf("read count %d, error %v", reads, err)
			}
			if err != nil {
				if len(result.Entries) != 0 {
					t.Fatal("rejected response exposed entries")
				}
				return
			}
			if !result.Partial || len(result.Entries) != 100 ||
				result.Entries[0].Kind != "file" || result.Entries[1].Kind != "directory" ||
				result.Entries[2].Kind != "symlink" || result.Entries[3].Kind != "other" {
				t.Fatal("incorrect bounded metadata")
			}
			body, err := json.Marshal(result)
			if err != nil || !strings.Contains(string(body), `"name":"Untrusted text in a filename"`) ||
				strings.Contains(string(body), `"mode"`) {
				t.Fatalf("incorrect structured result: %v", err)
			}
		})
	}
}
