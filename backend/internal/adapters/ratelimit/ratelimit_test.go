package ratelimit_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/ratelimit"
	"github.com/fprambs/transferly/internal/testutil"
)

// newLimiter returns a limiter of 3 actions per burst, one earned every 10 seconds, with a fake clock.
func newLimiter(maxKeys int) (*ratelimit.Limiter, *testutil.FakeClock) {
	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return ratelimit.New(clock, ratelimit.Rule{Burst: 3, Every: 10 * time.Second}, maxKeys), clock
}

// TestLimiter_BurstRefillAndIsolation verifies the token bucket behavior.
//
// Scenarios:
//   - a burst of actions is allowed, then refused with the time to wait
//   - waiting earns actions back gradually, never above the burst
//   - keys are independent
func TestLimiter_BurstRefillAndIsolation(t *testing.T) {
	limiter, clock := newLimiter(100)

	for i := range 3 {
		if ok, _ := limiter.Allow("a"); !ok {
			t.Fatalf("action %d of the burst was refused", i+1)
		}
	}
	ok, retry := limiter.Allow("a")
	if ok || retry != 10*time.Second {
		t.Errorf("Allow() after the burst = %v, %v, want refusal with a 10s wait", ok, retry)
	}
	if ok, _ := limiter.Allow("b"); !ok {
		t.Error("another key was affected by a")
	}

	clock.Advance(4 * time.Second)
	if ok, retry := limiter.Allow("a"); ok || retry != 6*time.Second {
		t.Errorf("after 4s = %v, %v, want refusal with a 6s wait", ok, retry)
	}
	clock.Advance(6 * time.Second)
	if ok, _ := limiter.Allow("a"); !ok {
		t.Error("an action earned back was refused")
	}
	if ok, _ := limiter.Allow("a"); ok {
		t.Error("more actions than earned were allowed")
	}

	clock.Advance(time.Hour)
	for i := range 3 {
		if ok, _ := limiter.Allow("a"); !ok {
			t.Fatalf("action %d after a long wait was refused", i+1)
		}
	}
	if ok, _ := limiter.Allow("a"); ok {
		t.Error("the burst was exceeded after a long wait")
	}
}

// TestLimiter_BoundedMemory verifies that memory cannot be exhausted by inventing keys.
//
// Scenarios:
//   - the number of remembered keys never exceeds the maximum
//   - recovered (idle) buckets are dropped before busy ones
//   - when every bucket is busy the least recently used one is evicted
func TestLimiter_BoundedMemory(t *testing.T) {
	limiter, clock := newLimiter(3)

	for i := range 50 {
		limiter.Allow(fmt.Sprintf("key-%d", i))
		clock.Advance(time.Millisecond)
	}
	if limiter.Len() > 3 {
		t.Errorf("Len() = %d, want at most 3", limiter.Len())
	}

	limiter, clock = newLimiter(3)
	for _, key := range []string{"busy1", "busy2", "idle"} {
		limiter.Allow(key)
	}
	clock.Advance(time.Minute)
	limiter.Allow("busy1")
	limiter.Allow("busy1")
	limiter.Allow("busy1")
	limiter.Allow("busy2")
	limiter.Allow("busy2")
	limiter.Allow("busy2")
	limiter.Allow("new")
	if ok, _ := limiter.Allow("busy1"); ok {
		t.Error("a busy bucket was dropped when idle ones existed")
	}

	limiter, _ = newLimiter(2)
	limiter.Allow("old")
	limiter.Allow("newer")
	limiter.Allow("newer")
	limiter.Allow("newest")
	if limiter.Len() != 2 {
		t.Errorf("Len() = %d, want 2", limiter.Len())
	}
}

// TestLimiter_IsSafeForConcurrentUse verifies exact accounting under contention: with a burst of 3,
// exactly 3 of many simultaneous requests for one key are allowed.
func TestLimiter_IsSafeForConcurrentUse(t *testing.T) {
	limiter, _ := newLimiter(100)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := limiter.Allow("shared"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed != 3 {
		t.Errorf("allowed = %d, want exactly the burst of 3", allowed)
	}
}
