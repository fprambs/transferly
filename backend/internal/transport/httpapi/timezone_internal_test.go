package httpapi

import (
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/core/files"
)

// TestProbeResponse verifies how what a probe learned is reported.
//
// Scenarios:
//   - a whole number of hours suggests the Etc zone with the inverted sign, zero suggests UTC and a half hour suggests nothing
//   - an exact listing suggests no zone
func TestProbeResponse(t *testing.T) {
	for offset, want := range map[time.Duration]string{
		0:                               "UTC",
		-3 * time.Hour:                  "Etc/GMT+3",
		2 * time.Hour:                   "Etc/GMT-2",
		5*time.Hour + 30*time.Minute:    "",
		-(9*time.Hour + 45*time.Minute): "",
	} {
		got := probeResponse(files.TimeProbe{Offset: offset})
		if want == "" && got.SuggestedZone != nil || want != "" && (got.SuggestedZone == nil || *got.SuggestedZone != want) || got.OffsetMinutes != int(offset/time.Minute) {
			t.Errorf("probeResponse(%v) = %+v; want %q", offset, got, want)
		}
	}
	if got := probeResponse(files.TimeProbe{Exact: true}); got.SuggestedZone != nil || !got.Exact {
		t.Errorf("exact probe = %+v", got)
	}
}
