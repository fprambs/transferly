package httpapi

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// terminalSession identifies a revocable interactive resource without sharing its connection with transfers.
type terminalSession struct {
	session         string
	user, workspace uuid.UUID
	cancel          context.CancelFunc
}

// terminalSessions owns the lifetimes of terminals opened through one router.
type terminalSessions struct {
	mu     sync.Mutex
	next   uint64
	active map[uint64]terminalSession
}

// register tracks a terminal before it opens and returns its context and idempotent cleanup.
func (s *terminalSessions) register(ctx context.Context, session string, user, workspace uuid.UUID) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.active == nil {
		s.active = make(map[uint64]terminalSession)
	}
	s.next++
	id := s.next
	s.active[id] = terminalSession{session: session, user: user, workspace: workspace, cancel: cancel}
	s.mu.Unlock()
	return ctx, func() {
		cancel()
		s.mu.Lock()
		delete(s.active, id)
		s.mu.Unlock()
	}
}

// revoke cancels resources matching a session, user or workspace; empty selectors match every value.
func (s *terminalSessions) revoke(session string, user, workspace uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.active {
		if (session == "" || entry.session == session) && (user == uuid.Nil || entry.user == user) && (workspace == uuid.Nil || entry.workspace == workspace) {
			entry.cancel()
		}
	}
}
