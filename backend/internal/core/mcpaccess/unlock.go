package mcpaccess

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/ports"
)

// Unlocks tracks process-local access windows for already verified MCP links.
// Its owner must call Sweep periodically to cancel operations on expired windows.
type Unlocks struct {
	mu      sync.Mutex
	clock   ports.Clock
	maximum time.Duration
	idle    time.Duration
	windows map[uuid.UUID]*Window
}

// Window identifies one unlock generation and carries cancellation for its operations.
type Window struct {
	context context.Context
	cancel  context.CancelFunc
	maximum time.Time
	idle    time.Time
}

// NewUnlocks creates a registry with the configured absolute and idle lifetimes.
// Zero lifetimes select eight hours and two hours respectively.
func NewUnlocks(clock ports.Clock, maximum, idle time.Duration) *Unlocks {
	if maximum == 0 {
		maximum = 8 * time.Hour
	}
	if idle == 0 {
		idle = 2 * time.Hour
	}
	return &Unlocks{clock: clock, maximum: maximum, idle: idle, windows: make(map[uuid.UUID]*Window)}
}

// Open starts a new generation after vault verification and invalidates any prior window.
// lifetime is the service lifetime and must outlive the approving HTTP request.
func (u *Unlocks) Open(lifetime context.Context, link uuid.UUID) *Window {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.remove(link)
	now := u.clock.Now()
	ctx, cancel := context.WithCancel(lifetime)
	window := &Window{context: ctx, cancel: cancel, maximum: now.Add(u.maximum), idle: now.Add(u.idle)}
	u.windows[link] = window
	return window
}

// Context returns the cancellation signal for this generation.
func (w *Window) Context() context.Context { return w.context }

// Admit checks that the same generation is still active.
// Only a successfully authorized file call may pass activity=true.
func (u *Unlocks) Admit(link uuid.UUID, window *Window, activity bool) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	current := u.windows[link]
	if current == nil || current != window {
		return ErrUnavailable
	}
	now := u.clock.Now()
	if window.expired(now) {
		u.remove(link)
		return ErrUnavailable
	}
	if activity {
		window.idle = now.Add(u.idle)
	}
	return nil
}

// Lock cancels and removes the current generation for a link.
func (u *Unlocks) Lock(link uuid.UUID) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.remove(link)
}

// Sweep cancels and removes all expired generations.
func (u *Unlocks) Sweep() {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := u.clock.Now()
	for link, window := range u.windows {
		if window.expired(now) {
			u.remove(link)
		}
	}
}

// expired reports whether the service or either deadline ended the window.
func (w *Window) expired(now time.Time) bool {
	return w.context.Err() != nil || !now.Before(w.maximum) || !now.Before(w.idle)
}

// remove cancels an existing window while the caller holds the lock.
func (u *Unlocks) remove(link uuid.UUID) {
	if window := u.windows[link]; window != nil {
		delete(u.windows, link)
		window.cancel()
	}
}
