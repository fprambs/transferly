package mcpaccess

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// ErrReadFailed reports a remote failure without disclosing connector diagnostics.
var ErrReadFailed = errors.New("MCP metadata read failed")

// Metadata contains the only remote file fields allowed in a tool result.
type Metadata struct {
	// Name is an untrusted remote name.
	Name string `json:"name"`
	// Path is an untrusted remote path.
	Path string `json:"path"`
	// Kind identifies a directory, file, symlink, or other entry.
	Kind string `json:"kind"`
	// Size is the size in bytes reported by the server.
	Size int64 `json:"size"`
	// Modified is the server's reported modification time.
	Modified time.Time `json:"modified"`
}

// Listing is a bounded page of untrusted file metadata.
type Listing struct {
	// Entries holds the projected file metadata.
	Entries []Metadata `json:"entries"`
	// Partial reports whether the remote listing contained more entries.
	Partial bool `json:"partial"`
}

// ReadListing checks admission around the remote read so a revoked grant cannot return pending output.
// The caller must bind admit to the same unlock generation for both checks.
func ReadListing(ctx context.Context, admit func(context.Context) error, read func(context.Context) ([]remotefs.Entry, error)) (Listing, error) {
	if err := admit(ctx); err != nil {
		return Listing{}, ErrUnavailable
	}
	entries, err := read(ctx)
	if admissionErr := admit(ctx); admissionErr != nil {
		return Listing{}, ErrUnavailable
	}
	if err != nil {
		return Listing{}, ErrReadFailed
	}
	const limit = 100
	result := Listing{Entries: make([]Metadata, 0, min(len(entries), limit)), Partial: len(entries) > limit}
	for _, entry := range entries[:min(len(entries), limit)] {
		result.Entries = append(result.Entries, metadata(entry))
	}
	return result, nil
}

// metadata projects one entry without exposing permission bits or connector-specific fields.
func metadata(entry remotefs.Entry) Metadata {
	kind := "other"
	switch {
	case entry.Mode.IsRegular():
		kind = "file"
	case entry.Mode.IsDir():
		kind = "directory"
	case entry.Mode&fs.ModeSymlink != 0:
		kind = "symlink"
	}
	return Metadata{Name: entry.Name, Path: entry.Path, Kind: kind, Size: entry.Size, Modified: entry.ModTime.UTC()}
}
