package vault

import (
	"sync"
	"time"

	"github.com/fprambs/sentinel/internal/core/ports"
)

// entry is a DEK held in memory with the instant it stops being valid.
type entry struct {
	dek     []byte
	expires time.Time
}

// KeyRing keeps unlocked DEKs in memory, per session, for a limited idle time.
// Keys are never persisted and are wiped when removed. It is safe for concurrent use.
type KeyRing struct {
	clock   ports.Clock
	ttl     time.Duration
	mu      sync.Mutex
	entries map[string]entry
}

// NewKeyRing returns a key ring whose entries expire after ttl without use.
func NewKeyRing(clock ports.Clock, ttl time.Duration) *KeyRing {
	return &KeyRing{clock: clock, ttl: ttl, entries: map[string]entry{}}
}

// Put stores a copy of dek for the session, replacing and wiping any previous key.
func (r *KeyRing) Put(session string, dek []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.remove(session)
	r.entries[session] = entry{dek: append([]byte(nil), dek...), expires: r.clock.Now().Add(r.ttl)}
}

// Get returns a copy of the session's DEK and extends its idle time.
// It reports false when the session has no key or the key expired; expired keys are wiped.
func (r *KeyRing) Get(session string) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[session]
	if !ok {
		return nil, false
	}
	now := r.clock.Now()
	if !now.Before(e.expires) {
		r.remove(session)
		return nil, false
	}
	e.expires = now.Add(r.ttl)
	r.entries[session] = e
	return append([]byte(nil), e.dek...), true
}

// Lock removes and wipes the session's DEK.
func (r *KeyRing) Lock(session string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.remove(session)
}

// Sweep removes and wipes every expired key and returns how many were removed.
func (r *KeyRing) Sweep() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.clock.Now()
	removed := 0
	for session, e := range r.entries {
		if !now.Before(e.expires) {
			r.remove(session)
			removed++
		}
	}
	return removed
}

// remove deletes and wipes an entry; the caller holds the lock.
func (r *KeyRing) remove(session string) {
	if e, ok := r.entries[session]; ok {
		Wipe(e.dek)
		delete(r.entries, session)
	}
}
