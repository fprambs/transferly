package mcpaccess

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// unlockClock advances deterministically without sleeping.
type unlockClock struct{ now time.Time }

// Now returns the time set by the test.
func (c *unlockClock) Now() time.Time { return c.now }

// TestUnlocks verifies idle and absolute expiration, explicit locking, and generation replacement.
func TestUnlocks(t *testing.T) {
	clock := &unlockClock{}
	registry := NewUnlocks(clock, 0, 0)
	link := uuid.New()
	first := registry.Open(t.Context(), link)
	clock.now = clock.now.Add(time.Hour)
	if err := registry.Admit(link, first, false); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Hour)
	if registry.Admit(link, first, true) != ErrUnavailable || first.Context().Err() == nil {
		t.Fatal("passive check extended the idle lifetime")
	}

	second := registry.Open(t.Context(), link)
	for range 7 {
		clock.now = clock.now.Add(time.Hour)
		if err := registry.Admit(link, second, true); err != nil {
			t.Fatal(err)
		}
	}
	clock.now = clock.now.Add(time.Hour)
	registry.Sweep()
	if second.Context().Err() == nil {
		t.Fatal("activity extended the absolute lifetime")
	}

	third := registry.Open(t.Context(), link)
	fourth := registry.Open(t.Context(), link)
	if third.ID() == uuid.Nil || third.ID() == fourth.ID() {
		t.Fatal("generation did not receive its own pool identity")
	}
	registry.Sweep()
	if registry.Admit(link, third, true) != ErrUnavailable || third.Context().Err() == nil {
		t.Fatal("old generation kept access")
	}
	if err := registry.Admit(link, fourth, false); err != nil {
		t.Fatal(err)
	}
	registry.Lock(link)
	if fourth.Context().Err() == nil || registry.Admit(link, fourth, false) != ErrUnavailable {
		t.Fatal("locked generation kept access")
	}

	service, stop := context.WithCancel(t.Context())
	fifth := registry.Open(service, link)
	stop()
	registry.Sweep()
	if registry.Admit(link, fifth, false) != ErrUnavailable {
		t.Fatal("stopped service kept access")
	}
}
