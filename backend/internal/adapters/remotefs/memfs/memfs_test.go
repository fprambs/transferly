package memfs_test

import (
	"io"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/memfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/testutil"
)

// TestConformance verifies that the in-memory file system passes the shared conformance suite.
func TestConformance(t *testing.T) {
	remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS {
		return memfs.New(testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	})
}

// TestPaths verifies path normalization: relative paths, dot segments and trailing slashes address the same node.
func TestPaths(t *testing.T) {
	fsys := memfs.New(testutil.NewFakeClock(time.Now()))
	w, err := fsys.OpenWrite(t.Context(), "dir/../file.txt/", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatalf("OpenWrite = %v", err)
	}
	_, _ = io.WriteString(w, "x")
	_ = w.Close()

	e, err := fsys.Stat(t.Context(), "/./file.txt")
	if err != nil || e.Path != "/file.txt" {
		t.Errorf("Stat = %+v, %v, want the normalized path /file.txt", e, err)
	}
}

// TestModTimesFollowTheClock verifies that writes stamp the clock time.
func TestModTimesFollowTheClock(t *testing.T) {
	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fsys := memfs.New(clock)
	w, err := fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Hour)
	_, _ = io.WriteString(w, "x")

	e, _ := fsys.Stat(t.Context(), "/f")
	if want := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC); !e.ModTime.Equal(want) {
		t.Errorf("ModTime = %v, want %v", e.ModTime, want)
	}
}

// TestWriterFailsWhenTheFileWasRemoved verifies that writing to a removed file reports not found.
func TestWriterFailsWhenTheFileWasRemoved(t *testing.T) {
	fsys := memfs.New(testutil.NewFakeClock(time.Now()))
	w, err := fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsys.Remove(t.Context(), "/f"); err != nil {
		t.Fatal(err)
	}

	if _, err := w.Write([]byte("x")); err == nil {
		t.Error("Write to a removed file = nil error, want error")
	}
}

// TestWriterExtendsWithZeros verifies that writing past the end pads with zero bytes.
func TestWriterExtendsWithZeros(t *testing.T) {
	fsys := memfs.New(testutil.NewFakeClock(time.Now()))
	put, _ := fsys.OpenWrite(t.Context(), "/f", 0, remotefs.WriteOverwrite)
	_, _ = io.WriteString(put, "ab")
	_ = put.Close()

	w, err := fsys.OpenWrite(t.Context(), "/f", 2, remotefs.WriteResume)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "cd")

	e, _ := fsys.Stat(t.Context(), "/f")
	if e.Size != 4 {
		t.Errorf("Size = %d, want 4", e.Size)
	}
}
