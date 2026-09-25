// Package events delivers the events of the core to the clients that are connected right now.
package events

import (
	"sync"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/ports"
)

// Subscription is one connected client. Events arrive on [Subscription.Events] until the client leaves or the
// hub drops it, which [Subscription.Done] announces.
type Subscription struct {
	userID    uuid.UUID
	sessionID string
	events    chan ports.Event
	done      chan struct{}
	once      sync.Once
}

// Events returns the channel of events for the client.
func (s *Subscription) Events() <-chan ports.Event { return s.events }

// Done is closed when the subscription ended: the client unsubscribed, its session ended, or it could not keep up.
func (s *Subscription) Done() <-chan struct{} { return s.done }

// end closes the subscription once.
func (s *Subscription) end() { s.once.Do(func() { close(s.done) }) }

// Hub fans events out to the subscriptions of their user. It implements [ports.EventSink] and is safe for
// concurrent use.
type Hub struct {
	buffer int

	mu   sync.Mutex
	subs map[*Subscription]struct{}
}

var _ ports.EventSink = (*Hub)(nil)

// New returns a hub whose subscriptions buffer up to buffer events before they are dropped as too slow.
func New(buffer int) *Hub {
	return &Hub{buffer: max(buffer, 1), subs: map[*Subscription]struct{}{}}
}

// Subscribe registers a client of a user's session.
func (h *Hub) Subscribe(userID uuid.UUID, sessionID string) *Subscription {
	s := &Subscription{userID: userID, sessionID: sessionID, events: make(chan ports.Event, h.buffer), done: make(chan struct{})}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

// Unsubscribe removes a client. It is safe to call more than once.
func (h *Hub) Unsubscribe(s *Subscription) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
	s.end()
}

// Publish sends the event to every client of its user. It never blocks: a client whose buffer is full is
// dropped, and reloads its state when it reconnects.
func (h *Hub) Publish(e ports.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		if s.userID != e.UserID {
			continue
		}
		select {
		case s.events <- e:
		default:
			delete(h.subs, s)
			s.end()
		}
	}
}

// CloseSession drops the clients of a session, for example when it signs out.
func (h *Hub) CloseSession(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		if s.sessionID == sessionID {
			delete(h.subs, s)
			s.end()
		}
	}
}

// Count returns the number of connected clients.
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
