package testutil_test

import (
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/testutil"
)

// FakeClock must satisfy ports.Clock.
var _ ports.Clock = (*testutil.FakeClock)(nil)

// TestFakeClock_NormalizesToUTC verifies that a start time given in another zone
// is reported as the same instant in UTC.
func TestFakeClock_NormalizesToUTC(t *testing.T) {
	zone := time.FixedZone("UTC+9", 9*60*60)
	c := testutil.NewFakeClock(time.Date(2026, 3, 8, 9, 0, 0, 0, zone))

	got := c.Now()
	want := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("Now() = %v, want %v in UTC", got, want)
	}
}

// TestFakeClock_Advance verifies that Advance moves the clock forward by the given duration.
func TestFakeClock_Advance(t *testing.T) {
	c := testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	c.Advance(90 * time.Minute)

	want := time.Date(2026, 1, 1, 1, 30, 0, 0, time.UTC)
	if got := c.Now(); !got.Equal(want) {
		t.Errorf("Now() = %v, want %v", got, want)
	}
}
