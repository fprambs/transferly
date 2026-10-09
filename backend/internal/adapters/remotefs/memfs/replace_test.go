package memfs_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/memfs"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/testutil"
)

// TestReplace checks missing sources, invalid directories, cancellation and a missing parent without losing the destination.
func TestReplace(t *testing.T) {
	f := memfs.New(testutil.NewFakeClock(time.Now()))
	w, _ := f.OpenWrite(t.Context(), "/source", 0, remotefs.WriteCreate)
	_, _ = io.WriteString(w, "new")
	_ = w.Close()
	_ = f.Mkdir(t.Context(), "/dir")
	for _, tc := range []struct {
		from, to string
		want     error
	}{
		{"/missing", "/target", remotefs.ErrNotFound},
		{"/dir", "/target", remotefs.ErrInvalid},
		{"/source", "/dir", remotefs.ErrInvalid},
		{"/source", "/absent/target", remotefs.ErrNotFound},
	} {
		if err := f.Replace(t.Context(), tc.from, tc.to); !errors.Is(err, tc.want) {
			t.Fatalf("replace %s: %v", tc.from, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.Replace(ctx, "/source", "/target"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := f.Replace(t.Context(), "/source", "/target"); err != nil {
		t.Fatal(err)
	}
	r, _ := f.OpenRead(t.Context(), "/target", 0)
	defer func() { _ = r.Close() }()
	data, _ := io.ReadAll(r)
	if string(data) != "new" {
		t.Fatalf("committed %q", data)
	}
}
