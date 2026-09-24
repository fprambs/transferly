package clock_test

import (
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/adapters/clock"
	"github.com/fprambs/sentinel/internal/core/ports"
)

// System must satisfy ports.Clock.
var _ ports.Clock = clock.System{}

// TestSystem_NowIsUTC verifies that the system clock returns the current instant in UTC,
// bounded by the wall clock before and after the call.
func TestSystem_NowIsUTC(t *testing.T) {
	before := time.Now()
	got := clock.System{}.Now()
	after := time.Now()

	if got.Location() != time.UTC {
		t.Errorf("Location() = %v, want UTC", got.Location())
	}
	if got.Before(before) || got.After(after) {
		t.Errorf("Now() = %v, want between %v and %v", got, before, after)
	}
}
