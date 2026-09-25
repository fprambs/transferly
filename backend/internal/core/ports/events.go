package ports

import "github.com/google/uuid"

// Event is something that happened that a user's open clients should hear about.
type Event struct {
	// Type names the event, such as transfer.progress.
	Type string
	// UserID is the user who receives the event.
	UserID uuid.UUID
	// Data is the payload, serialized by the transport that delivers the event.
	Data any
}

// EventSink delivers events to the clients of a user. Publishing never blocks: a client that cannot keep up
// misses events and is expected to reload its state.
type EventSink interface {
	// Publish hands an event to the transport.
	Publish(e Event)
}
