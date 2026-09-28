// Package credentials keeps the passwords of sites whose logon type asks for one when connecting (ADR-0010). A password
// lives only in memory, for one session and one site, and is overwritten when it is dropped: when the session ends, the
// vault is locked, the site changes, the password turns out to be wrong, or, when the person did not ask to remember it,
// once no connection to the site is open any more.
package credentials

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/core/vault"
)

// grace is how long a password not to be remembered is kept even with no connection open, so the connection it was
// typed for has time to open.
const grace = time.Minute

// ErrRequired means the site asks for a password when connecting and the session has not given one.
var ErrRequired = errors.New("credentials required")

// Key identifies what a password is for: one site of a workspace, in one session of one user.
type Key struct {
	// Session is the session the password was typed in.
	Session string
	// UserID is the user of the session.
	UserID uuid.UUID
	// WorkspaceID is the workspace of the site.
	WorkspaceID uuid.UUID
	// SiteID is the site.
	SiteID uuid.UUID
}

// entry is a password held for a key.
type entry struct {
	password []byte
	remember bool
	added    time.Time
	expires  time.Time
}

// Store holds the passwords. It is safe for concurrent use.
type Store struct {
	clock    ports.Clock
	lifetime time.Duration

	mu      sync.Mutex
	entries map[Key]*entry
	ask     map[uuid.UUID]bool
}

// New returns a store whose passwords can live at most lifetime, the longest a session can last.
func New(clock ports.Clock, lifetime time.Duration) *Store {
	return &Store{clock: clock, lifetime: lifetime, entries: map[Key]*entry{}, ask: map[uuid.UUID]bool{}}
}

// Put holds a copy of password for key, replacing and wiping any earlier one. remember keeps it for the rest of the
// session; otherwise it goes once no connection to the site is open, as [Store.Sweep] finds.
func (s *Store) Put(key Key, password []byte, remember bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drop(key)
	now := s.clock.Now()
	s.entries[key] = &entry{password: append([]byte(nil), password...), remember: remember, added: now, expires: now.Add(s.lifetime)}
	s.ask[key.SiteID] = true
}

// Password returns a copy of the password held for key, which the caller wipes after use.
func (s *Store) Password(key Key) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), e.password...), true
}

// MarkAsk records that the site asks for a password, so [Store.Admits] refuses sessions that did not give one.
func (s *Store) MarkAsk(siteID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ask[siteID] = true
}

// Admits reports whether the session of key may use an open connection to its site: always for a site that does not
// ask for a password, and only with a password of its own for one that does.
func (s *Store) Admits(key Key) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ask[key.SiteID] {
		return true
	}
	_, ok := s.entries[key]
	return ok
}

// Forget drops the password held for key, when the server refused it.
func (s *Store) Forget(key Key) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drop(key)
}

// ForgetSite drops every password held for a site, in every session, and forgets that it asks for one: its settings
// changed or it was deleted.
func (s *Store) ForgetSite(siteID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropWhere(func(key Key) bool { return key.SiteID == siteID })
	delete(s.ask, siteID)
}

// CloseSession drops every password typed in a session: it ended, or its vault was locked.
func (s *Store) CloseSession(session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropWhere(func(key Key) bool { return key.Session == session })
}

// CloseUser drops every password of a user, in every session: the account was disabled.
func (s *Store) CloseUser(userID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropWhere(func(key Key) bool { return key.UserID == userID })
}

// Sweep drops the passwords whose session can no longer exist, and the ones not to be remembered whose site has no open
// connection any more, as open reports for a user, a workspace and a site, once they are a minute old. It returns how
// many it dropped.
func (s *Store) Sweep(open func(userID, workspaceID, siteID uuid.UUID) bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	return s.dropWhere(func(key Key) bool {
		e := s.entries[key]
		return !now.Before(e.expires) || (!e.remember && now.Sub(e.added) >= grace && !open(key.UserID, key.WorkspaceID, key.SiteID))
	})
}

// drop removes and wipes the entry of key; the caller holds the lock.
func (s *Store) drop(key Key) {
	if e, ok := s.entries[key]; ok {
		vault.Wipe(e.password)
		delete(s.entries, key)
	}
}

// dropWhere removes and wipes the entries whose key matches, and returns how many; the caller holds the lock.
func (s *Store) dropWhere(match func(Key) bool) int {
	dropped := 0
	for key := range s.entries {
		if match(key) {
			s.drop(key)
			dropped++
		}
	}
	return dropped
}
