package vault

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
	"time"
)

// ErrNoSession is returned by a [SessionStore] when it keeps no key for the session.
var ErrNoSession = errors.New("no vault key kept for the session")

// sessionKeyInfo binds the keys derived from session secrets to their use (ADR-0028).
const sessionKeyInfo = "transferly/vault-session/v1"

// sessionStoreTimeout bounds each operation on the [SessionStore] made outside a request, when a vault is locked.
const sessionStoreTimeout = 5 * time.Second

// SessionStore keeps the keys of sessions that remember their vault across restarts (ADR-0028), each sealed with a key derived
// from the secret of its session, which only the browser holds. A key goes away with its session.
type SessionStore interface {
	// SaveVaultSession keeps the sealed key of a session, replacing the one kept before.
	SaveVaultSession(ctx context.Context, session string, sealed []byte) error
	// VaultSession returns the sealed key of a session, or [ErrNoSession].
	VaultSession(ctx context.Context, session string) ([]byte, error)
	// DeleteVaultSession forgets the key of a session; forgetting a session with none succeeds.
	DeleteVaultSession(ctx context.Context, session string) error
}

// sessionMemory is what a [Service] that remembers sessions needs: where it keeps them, the randomness of their seals, and the
// sessions whose key it already kept since it started.
type sessionMemory struct {
	store  SessionStore
	random io.Reader
	kept   sync.Map
}

// RememberSessions makes the service keep the key of each unlocked session in store, sealed with the secret of the session, so the
// vault of a session stays unlocked across restarts of Transferly (ADR-0028). It is meant for development, where the server restarts
// with every change; without it keys live in memory only. It returns the service.
func (s *Service) RememberSessions(store SessionStore, random io.Reader) *Service {
	s.memory = &sessionMemory{store: store, random: random}
	return s
}

// sessionKey derives the key that seals the vault key of a session from the secret of the session. HKDF only fails for keys longer
// than 255 hashes, which [KeyLength] never is.
func sessionKey(secret string) []byte {
	key, _ := hkdf.Key(sha256.New, []byte(secret), nil, sessionKeyInfo, KeyLength)
	return key
}

// Resume runs before every request of a signed-in session: it keeps an unlocked vault unlocked while the session is in use, and,
// when the service remembers sessions, unlocks the vault again with the key kept for the session if the server lost it. secret is
// the token of the session. Failures leave the vault locked, as it was.
func (s *Service) Resume(ctx context.Context, session, secret string) {
	if s.ring.Touch(session) || s.memory == nil {
		return
	}
	sealed, err := s.memory.store.VaultSession(ctx, session)
	if err != nil {
		return
	}
	key := sessionKey(secret)
	defer Wipe(key)
	dek, err := Open(key, sealed, []byte(session))
	if err != nil {
		return
	}
	defer Wipe(dek)
	s.ring.Put(session, dek)
	s.memory.kept.Store(session, struct{}{})
}

// Remember runs after every request of a signed-in session when the service remembers sessions: once the vault of the session is
// unlocked, its key is kept, sealed with the secret of the session, the first time. Failures are tried again on the next request.
func (s *Service) Remember(ctx context.Context, session, secret string) {
	if s.memory == nil {
		return
	}
	if _, kept := s.memory.kept.Load(session); kept {
		return
	}
	dek, ok := s.ring.Get(session)
	if !ok {
		return
	}
	defer Wipe(dek)
	key := sessionKey(secret)
	defer Wipe(key)
	sealed, err := Seal(s.memory.random, key, dek, []byte(session))
	if err != nil {
		return
	}
	if s.memory.store.SaveVaultSession(ctx, session, sealed) == nil {
		s.memory.kept.Store(session, struct{}{})
	}
}

// forget drops the key kept for a session, when the service remembers sessions.
func (s *Service) forget(session string) {
	if s.memory == nil {
		return
	}
	s.memory.kept.Delete(session)
	ctx, cancel := context.WithTimeout(context.Background(), sessionStoreTimeout)
	defer cancel()
	_ = s.memory.store.DeleteVaultSession(ctx, session)
}
