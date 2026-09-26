package files

import (
	"context"
	"crypto/rand"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

const (
	// probeGranularity is the step offsets are rounded to, which absorbs the minute precision of listings and small clock differences.
	probeGranularity = 15 * time.Minute
	// maxProbeOffset is the largest offset a time zone can have; a bigger difference means the clocks disagree, not the zones.
	maxProbeOffset = 14 * time.Hour
)

// ErrProbeInconclusive means the times of the server and the clock differ by more than any time zone can.
var ErrProbeInconclusive = fmt.Errorf("the times of the server do not match the clock")

// TimeProbe is what a probe learned about the times a server prints in its listings.
type TimeProbe struct {
	// Exact is true when the listing gives times to the second and in UTC, so no zone is needed.
	Exact bool
	// Offset is how far the times printed by the server are from UTC, rounded to a quarter of an hour, as the site is
	// configured; it is meaningful when Exact is false.
	Offset time.Duration
}

// WithClock returns the service with another clock, which tests and simulations use to read the time.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// ProbeTimeOffset finds the time zone offset of a server that lists files without a zone: it writes a small file of its own in dir, reads
// the time the listing prints for it, compares it with the clock and removes it again. The file is removed even when the probe fails.
// The listing is parsed with the zone the site has now, so `current` is added back to get the offset of the server itself.
func (s *Service) ProbeTimeOffset(ctx context.Context, t Target, dir string, current time.Duration) (TimeProbe, error) {
	lease, err := s.begin(ctx, t, workspace.ActionUpload, dir)
	if err != nil {
		return TimeProbe{}, err
	}
	probe, err := probeOnce(ctx, lease.FS(), dir, s.now, current)
	lease.Release(err)
	return probe, err
}

// probeOnce writes the probe file, measures it and removes it.
func probeOnce(ctx context.Context, fsys remotefs.RemoteFS, dir string, now func() time.Time, current time.Duration) (TimeProbe, error) {
	name := ".sentinel-probe-" + strings.ToLower(rand.Text()[:12])
	file := path.Join(dir, name)
	w, err := fsys.OpenWrite(ctx, file, 0, remotefs.WriteCreate)
	if err != nil {
		return TimeProbe{}, err
	}
	_, err = w.Write([]byte("sentinel time probe\n"))
	if closeErr := w.Close(); err == nil {
		err = closeErr
	}
	written := now()
	if err != nil {
		_ = fsys.Remove(context.WithoutCancel(ctx), file)
		return TimeProbe{}, err
	}
	probe, err := measure(ctx, fsys, dir, name, written, current)
	if removeErr := fsys.Remove(context.WithoutCancel(ctx), file); err == nil {
		err = removeErr
	}
	return probe, err
}

// measure lists the folder and compares the time printed for the probe file with the time it was written.
func measure(ctx context.Context, fsys remotefs.RemoteFS, dir, name string, written time.Time, current time.Duration) (TimeProbe, error) {
	entries, err := fsys.List(ctx, dir)
	if err != nil {
		return TimeProbe{}, err
	}
	for _, e := range entries {
		if e.Name != name {
			continue
		}
		if e.ModTimePrecision == remotefs.PrecisionSecond {
			return TimeProbe{Exact: true}, nil
		}
		offset := e.ModTime.Sub(written) + current
		offset = offset.Round(probeGranularity)
		if offset > maxProbeOffset || offset < -maxProbeOffset {
			return TimeProbe{}, ErrProbeInconclusive
		}
		return TimeProbe{Offset: offset}, nil
	}
	return TimeProbe{}, fmt.Errorf("%w: %s is not in the listing", remotefs.ErrNotFound, name)
}
