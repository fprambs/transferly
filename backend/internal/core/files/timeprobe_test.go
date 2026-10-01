package files_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/workspace"
)

// zoneFS lists like a server that prints local times without a zone: it shifts the times of every entry and gives them minute precision.
type zoneFS struct {
	*flakyFS
	shift     time.Duration
	minute    bool
	hide      bool
	listErr   error
	removeErr error
}

// List shifts the times, hides the probe file or fails, as configured.
func (z *zoneFS) List(ctx context.Context, dir string) ([]remotefs.Entry, error) {
	if z.listErr != nil {
		return nil, z.listErr
	}
	entries, err := z.flakyFS.List(ctx, dir)
	kept := entries[:0]
	for _, e := range entries {
		if z.hide {
			continue
		}
		e.ModTime = e.ModTime.Add(z.shift)
		if z.minute {
			e.ModTimePrecision = remotefs.PrecisionMinute
		}
		kept = append(kept, e)
	}
	return kept, err
}

// Remove fails when configured to.
func (z *zoneFS) Remove(ctx context.Context, p string) error {
	if z.removeErr != nil {
		return z.removeErr
	}
	return z.flakyFS.Remove(ctx, p)
}

// zoneConnector hands out the zone file system.
type zoneConnector struct{ fsys *zoneFS }

// Connect returns the zone file system.
func (c zoneConnector) Connect(context.Context, connections.Key, string) (remotefs.RemoteFS, error) {
	return c.fsys, nil
}

// TestService_ProbeTimeOffset verifies finding the time zone offset of a server.
//
// Scenarios:
//   - a server that lists exact times needs no zone and the probe file is gone afterwards
//   - times printed three hours behind the clock give an offset of minus three hours, the zone the site has now is added back, and odd offsets
//     are rounded to a quarter of an hour
//   - times that differ from the clock by more than any zone are inconclusive
//   - a probe file that is not in the listing, a listing that fails, a probe file that cannot be written and one that cannot be removed are reported, and
//     the probe file is removed whenever it was written
//   - a viewer, an unknown user and an empty path are refused
func TestService_ProbeTimeOffset(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	fake := &zoneFS{flakyFS: f.fs}
	pool := connections.New(zoneConnector{fake}, f.clock, connections.Config{Attempts: 1})
	t.Cleanup(pool.Close)
	svc := files.NewService(roles{f.editor.Actor: workspace.RoleEditor, f.viewer.Actor: workspace.RoleViewer}, pool).WithClock(f.clock.Now)
	if _, err := f.svc.Mkdir(ctx, f.editor, "/dir"); err != nil {
		t.Fatal(err)
	}
	f.put(t, "/dir/other.txt", "already here")
	left := func() int {
		entries, _ := f.svc.List(ctx, f.editor, "/dir")
		return len(entries)
	}

	if got, err := svc.ProbeTimeOffset(ctx, f.editor, "/dir", 0); err != nil || !got.Exact || left() != 1 {
		t.Errorf("exact probe = %+v, %v, %d files left", got, err, left())
	}

	fake.minute = true
	for name, tt := range map[string]struct {
		shift, current, want time.Duration
	}{
		"behind":        {-3 * time.Hour, 0, -3 * time.Hour},
		"with a zone":   {-3 * time.Hour, time.Hour, -2 * time.Hour},
		"odd":           {5*time.Hour + 20*time.Minute, 0, 5*time.Hour + 15*time.Minute},
		"ahead":         {2 * time.Hour, 0, 2 * time.Hour},
		"same as clock": {0, 0, 0},
	} {
		fake.shift = tt.shift
		got, err := svc.ProbeTimeOffset(ctx, f.editor, "/dir", tt.current)
		if err != nil || got.Exact || got.Offset != tt.want || left() != 1 {
			t.Errorf("%s = %+v, %v, %d files left (one other file is expected); want %v", name, got, err, left(), tt.want)
		}
	}

	fake.shift = 20 * time.Hour
	if _, err := svc.ProbeTimeOffset(ctx, f.editor, "/dir", 0); !errors.Is(err, files.ErrProbeInconclusive) || left() != 1 {
		t.Errorf("probe with clocks far apart = %v; want ErrProbeInconclusive and only the other file left", err)
	}
	fake.shift, fake.hide = 0, true
	if _, err := svc.ProbeTimeOffset(ctx, f.editor, "/dir", 0); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("probe with a hidden file = %v; want ErrNotFound", err)
	}
	fake.hide, fake.listErr = false, remotefs.ErrConnectionLost
	if _, err := svc.ProbeTimeOffset(ctx, f.editor, "/dir", 0); !errors.Is(err, remotefs.ErrConnectionLost) || left() != 1 {
		t.Errorf("probe with a failing listing = %v", err)
	}
	fake.listErr, fake.removeErr = nil, remotefs.ErrPermission
	if _, err := svc.ProbeTimeOffset(ctx, f.editor, "/dir", 0); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("probe that cannot clean up = %v; want ErrPermission", err)
	}
	fake.removeErr = nil
	f.fs.failCommit = true
	if _, err := svc.ProbeTimeOffset(ctx, f.editor, "/dir", 0); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("probe that cannot write = %v; want ErrConnectionLost", err)
	}
	f.fs.failCommit = false
	if _, err := svc.ProbeTimeOffset(ctx, f.editor, "/missing", 0); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("probe in a missing folder = %v; want ErrNotFound", err)
	}

	if _, err := svc.ProbeTimeOffset(ctx, f.viewer, "/dir", 0); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("probe by a viewer = %v; want ErrForbidden", err)
	}
	stranger := files.Target{Actor: uuid.New(), Workspace: f.editor.Workspace, Site: f.editor.Site, Session: "s"}
	if _, err := svc.ProbeTimeOffset(ctx, stranger, "/dir", 0); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("probe by an unknown user = %v; want ErrNotFound", err)
	}
	if _, err := svc.ProbeTimeOffset(ctx, f.editor, "", 0); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("probe with an empty path = %v; want ErrInvalid", err)
	}
}
