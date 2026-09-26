// Package scheduler decides which transfers may move data now. Transfers wait in a queue and are admitted while four
// limits hold: the total number of running transfers, and the number per workspace, per user and per site. Waiting
// transfers are served by priority and then by arrival, but one that cannot run because its site is full never blocks
// a later one that can.
package scheduler

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/google/uuid"
)

// Priority orders waiting transfers: a higher one is admitted first.
type Priority int

const (
	// Low waits behind everything else.
	Low Priority = -1
	// Normal is the default.
	Normal Priority = 0
	// High is admitted before the others.
	High Priority = 1
)

// Valid reports whether p is one of the priorities.
func (p Priority) Valid() bool { return p >= Low && p <= High }

// Errors returned by the scheduler.
var (
	// ErrNotQueued means the transfer is not waiting: it never queued, was admitted or has left.
	ErrNotQueued = errors.New("transfer is not waiting in the queue")
	// ErrInvalidPriority means the priority is not one of the known ones.
	ErrInvalidPriority = errors.New("invalid priority")
)

// Limits are the most transfers that may run at once. Zero means no limit.
type Limits struct {
	// Global limits the whole server.
	Global int
	// PerWorkspace limits each workspace.
	PerWorkspace int
	// PerUser limits each user.
	PerUser int
	// PerSite limits each site (or quick connection).
	PerSite int
}

// Request describes a transfer that wants to run.
type Request struct {
	// ID identifies the transfer.
	ID uuid.UUID
	// UserID is the user who transfers.
	UserID uuid.UUID
	// WorkspaceID is the workspace of the site.
	WorkspaceID uuid.UUID
	// SiteID is the site, or the quick connection.
	SiteID uuid.UUID
	// Priority orders the request in the queue.
	Priority Priority
}

// State describes a transfer that waits.
type State struct {
	// Paused is true while the transfer is held back by its user.
	Paused bool
	// Priority is its priority in the queue.
	Priority Priority
}

// waiter is a queued request.
type waiter struct {
	req      Request
	seq      uint64
	paused   bool
	admitted bool
	ready    chan struct{}
}

// counts are the running transfers of one dimension.
type counts map[uuid.UUID]int

// Scheduler admits transfers within limits. It is safe for concurrent use.
type Scheduler struct {
	mu       sync.Mutex
	limits   Limits
	running  int
	byWork   counts
	byUser   counts
	bySite   counts
	queue    []*waiter
	waiting  map[uuid.UUID]*waiter
	sequence uint64
}

// New returns a scheduler with the given limits.
func New(limits Limits) *Scheduler {
	return &Scheduler{limits: limits, byWork: counts{}, byUser: counts{}, bySite: counts{}, waiting: map[uuid.UUID]*waiter{}}
}

// SetLimits changes the limits at once; transfers that already run are not interrupted, and waiting ones that now fit are admitted.
func (s *Scheduler) SetLimits(limits Limits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits = limits
	s.dispatch()
}

// within reports whether a count is under its limit; a limit of zero means none.
func within(n, limit int) bool { return limit <= 0 || n < limit }

// fits reports whether admitting a request keeps every limit; the caller holds the lock.
func (s *Scheduler) fits(r Request) bool {
	return within(s.running, s.limits.Global) &&
		within(s.byWork[r.WorkspaceID], s.limits.PerWorkspace) &&
		within(s.byUser[r.UserID], s.limits.PerUser) &&
		within(s.bySite[r.SiteID], s.limits.PerSite)
}

// dispatch admits every waiting request that fits, best first; the caller holds the lock.
func (s *Scheduler) dispatch() {
	sort.SliceStable(s.queue, func(i, j int) bool {
		a, b := s.queue[i], s.queue[j]
		if a.req.Priority != b.req.Priority {
			return a.req.Priority > b.req.Priority
		}
		return a.seq < b.seq
	})
	kept := s.queue[:0]
	for _, w := range s.queue {
		if w.paused || !s.fits(w.req) {
			kept = append(kept, w)
			continue
		}
		s.running++
		s.byWork[w.req.WorkspaceID]++
		s.byUser[w.req.UserID]++
		s.bySite[w.req.SiteID]++
		w.admitted = true
		delete(s.waiting, w.req.ID)
		close(w.ready)
	}
	for i := len(kept); i < len(s.queue); i++ {
		s.queue[i] = nil
	}
	s.queue = kept
}

// free gives back the slot of an admitted request; the caller holds the lock.
func (s *Scheduler) free(r Request) {
	s.running--
	for _, c := range []struct {
		m  counts
		id uuid.UUID
	}{{s.byWork, r.WorkspaceID}, {s.byUser, r.UserID}, {s.bySite, r.SiteID}} {
		if c.m[c.id]--; c.m[c.id] <= 0 {
			delete(c.m, c.id)
		}
	}
	s.dispatch()
}

// Acquire waits until the request may run and returns the function that gives its slot back, which is safe to call
// more than once. It fails with the context error, without holding a slot, when the context ends first.
func (s *Scheduler) Acquire(ctx context.Context, r Request) (release func(), err error) {
	s.mu.Lock()
	s.sequence++
	w := &waiter{req: r, seq: s.sequence, ready: make(chan struct{})}
	s.queue = append(s.queue, w)
	s.waiting[r.ID] = w
	s.dispatch()
	s.mu.Unlock()

	select {
	case <-w.ready:
		var once sync.Once
		return func() {
			once.Do(func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				s.free(r)
			})
		}, nil
	case <-ctx.Done():
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if w.admitted {
		s.free(r)
		return nil, ctx.Err()
	}
	delete(s.waiting, r.ID)
	for i, queued := range s.queue {
		if queued == w {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			break
		}
	}
	return nil, ctx.Err()
}

// waitingFor returns the queued request with an identifier; the caller holds the lock.
func (s *Scheduler) waitingFor(id uuid.UUID) (*waiter, error) {
	w, ok := s.waiting[id]
	if !ok {
		return nil, ErrNotQueued
	}
	return w, nil
}

// Pause holds a waiting transfer back: it keeps its place but is not admitted until resumed.
func (s *Scheduler) Pause(id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.waitingFor(id)
	if err != nil {
		return err
	}
	w.paused = true
	return nil
}

// Resume lets a paused transfer be admitted again.
func (s *Scheduler) Resume(id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.waitingFor(id)
	if err != nil {
		return err
	}
	w.paused = false
	s.dispatch()
	return nil
}

// SetPriority changes the priority of a waiting transfer.
func (s *Scheduler) SetPriority(id uuid.UUID, p Priority) error {
	if !p.Valid() {
		return ErrInvalidPriority
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.waitingFor(id)
	if err != nil {
		return err
	}
	w.req.Priority = p
	s.dispatch()
	return nil
}

// StateOf describes a waiting transfer; the second result is false when it is not waiting.
func (s *Scheduler) StateOf(id uuid.UUID) (State, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.waiting[id]
	if !ok {
		return State{}, false
	}
	return State{Paused: w.paused, Priority: w.req.Priority}, true
}

// Running returns how many transfers hold a slot.
func (s *Scheduler) Running() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Waiting returns how many transfers wait.
func (s *Scheduler) Waiting() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}
