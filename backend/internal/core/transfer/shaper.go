package transfer

import (
	"context"
	"sync"
	"time"
)

// shaper spaces out the bytes of one or more transfers so they do not move faster than a rate. Each request for n bytes waits until the
// bytes reserved before it have had their time, then reserves the time n bytes take; a shaper that sat idle starts again without owing
// anything, so a pause never builds up a burst.
type shaper struct {
	mu   sync.Mutex
	rate int64
	next time.Time
}

// setRate changes the bytes per second; zero removes the cap.
func (s *shaper) setRate(rate int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rate = rate
}

// reserve books the time n bytes take at now and returns how long the caller must wait before moving more.
func (s *shaper) reserve(now time.Time, n int64) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rate <= 0 {
		return 0
	}
	if s.next.Before(now) {
		s.next = now
	}
	wait := s.next.Sub(now)
	s.next = s.next.Add(time.Duration(float64(n) / float64(s.rate) * float64(time.Second)))
	return wait
}

// sleep waits for d or until ctx ends, whichever comes first, and returns the error of the context in the second case.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
