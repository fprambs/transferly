package testutil

import (
	"sync"
	"time"
)

// FakeClock is a controllable [ports.Clock] for tests.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock returns a clock fixed at now, normalized to UTC.
func NewFakeClock(now time.Time) *FakeClock {
	return &FakeClock{now: now.UTC()}
}

// Now returns the current fake instant in UTC.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
