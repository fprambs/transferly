package events_test

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/events"
	"github.com/fprambs/transferly/internal/core/ports"
)

// receive returns the next event, or false when none arrives soon.
func receive(s *events.Subscription) (ports.Event, bool) {
	select {
	case e := <-s.Events():
		return e, true
	case <-time.After(100 * time.Millisecond):
		return ports.Event{}, false
	}
}

// ended reports whether the subscription was ended.
func ended(s *events.Subscription) bool {
	select {
	case <-s.Done():
		return true
	default:
		return false
	}
}

// TestHub_Delivery verifies who receives which event.
//
// Scenarios:
//   - an event reaches every client of its user, in order, and nobody else
//   - clients that left receive nothing, and leaving twice is harmless
//   - Count follows the connected clients
func TestHub_Delivery(t *testing.T) {
	h := events.New(8)
	alice, bob := uuid.New(), uuid.New()
	a1, a2, b := h.Subscribe(alice, "s1"), h.Subscribe(alice, "s2"), h.Subscribe(bob, "s3")
	if h.Count() != 3 {
		t.Fatalf("Count() = %d; want 3", h.Count())
	}

	h.Publish(ports.Event{Type: "one", UserID: alice})
	h.Publish(ports.Event{Type: "two", UserID: alice})
	for _, s := range []*events.Subscription{a1, a2} {
		first, _ := receive(s)
		second, _ := receive(s)
		if first.Type != "one" || second.Type != "two" {
			t.Errorf("a client of alice got %q then %q", first.Type, second.Type)
		}
	}
	if e, ok := receive(b); ok {
		t.Errorf("bob received %+v", e)
	}

	h.Unsubscribe(a1)
	h.Unsubscribe(a1)
	if !ended(a1) || h.Count() != 2 {
		t.Errorf("after leaving: ended=%v count=%d", ended(a1), h.Count())
	}
	h.Publish(ports.Event{Type: "three", UserID: alice})
	if e, ok := receive(a1); ok {
		t.Errorf("a client that left received %+v", e)
	}
	if e, _ := receive(a2); e.Type != "three" {
		t.Errorf("the remaining client got %+v", e)
	}
}

// TestHub_SlowClients verifies that a client that cannot keep up does not hold anyone back.
//
// Scenarios:
//   - publishing never blocks, even to a client that does not read
//   - a client whose buffer is full is dropped and announced as done, and others keep receiving
//   - a buffer of zero or less still works as one event
func TestHub_SlowClients(t *testing.T) {
	h := events.New(2)
	user := uuid.New()
	slow, fast := h.Subscribe(user, "slow"), h.Subscribe(user, "fast")

	published := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			h.Publish(ports.Event{Type: "tick", UserID: user})
			if e, ok := receive(fast); !ok || e.Type != "tick" {
				t.Errorf("the fast client missed event %d", i)
			}
		}
		close(published)
	}()
	select {
	case <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a client that does not read")
	}
	if !ended(slow) || ended(fast) || h.Count() != 1 {
		t.Errorf("slow ended=%v fast ended=%v count=%d; want only the slow client dropped", ended(slow), ended(fast), h.Count())
	}

	tiny := events.New(0)
	s := tiny.Subscribe(user, "s")
	tiny.Publish(ports.Event{Type: "only", UserID: user})
	if e, ok := receive(s); !ok || e.Type != "only" {
		t.Error("a hub with no buffer delivered nothing")
	}
}

// TestHub_CloseSession verifies ending the clients of a session.
//
// Scenarios:
//   - closing a session ends its clients and leaves those of other sessions of the same user
func TestHub_CloseSession(t *testing.T) {
	h := events.New(4)
	user := uuid.New()
	closing, other := h.Subscribe(user, "closing"), h.Subscribe(user, "other")
	h.CloseSession("closing")
	if !ended(closing) || ended(other) || h.Count() != 1 {
		t.Errorf("closing ended=%v other ended=%v count=%d", ended(closing), ended(other), h.Count())
	}
	h.CloseSession("unknown")
}

// TestHub_Concurrency hammers the hub, to be run with the race detector.
//
// Scenarios:
//   - concurrent subscribing, publishing, unsubscribing and closing sessions is safe
func TestHub_Concurrency(t *testing.T) {
	h := events.New(4)
	user := uuid.New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				s := h.Subscribe(user, "s")
				h.Publish(ports.Event{Type: "x", UserID: user})
				if j%3 == 0 {
					h.CloseSession("s")
				}
				h.Unsubscribe(s)
			}
		}()
	}
	wg.Wait()
	if h.Count() != 0 {
		t.Errorf("Count() = %d after everyone left", h.Count())
	}
}
