// Package quick keeps quick connections: servers a user tries out without saving anything. Their credentials live
// only in memory, are wiped when the connection ends, and can be saved as a site once the server proved to work.
package quick

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// maxPerUser is how many quick connections a user may keep at once.
const maxPerUser = 5

var (
	// ErrNotFound means the user has no such quick connection.
	ErrNotFound = errors.New("quick connection not found")
	// ErrTooMany means the user reached the number of quick connections they may keep.
	ErrTooMany = errors.New("too many quick connections")
)

// Secrets are the credentials of a quick connection.
type Secrets struct {
	// Password is the account password.
	Password []byte
	// PrivateKey is the SSH private key.
	PrivateKey []byte
	// Passphrase is the passphrase of the private key.
	Passphrase []byte
}

// wipe overwrites the secrets in memory.
func (s Secrets) wipe() {
	vault.Wipe(s.Password)
	vault.Wipe(s.PrivateKey)
	vault.Wipe(s.Passphrase)
}

// Connection is a quick connection. Its Site is never stored; its ID stands in for a site identifier.
type Connection struct {
	// Site holds the validated settings; Site.ID is the identifier clients use where a site identifier is expected.
	Site site.Site
	// Secrets are the credentials, in memory only.
	Secrets Secrets
	// UserID is the user the connection belongs to; nobody else can use it.
	UserID uuid.UUID

	lastUsed time.Time
}

// Guard vets the hosts a connection may point to.
type Guard interface {
	// CheckHost returns an error when the host resolves to a destination the server must not reach.
	CheckHost(ctx context.Context, host string) error
}

// Authorizer checks that a user may perform an action in a workspace.
type Authorizer interface {
	// Authorize returns the role of the user or an error that says why the action is refused.
	Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action workspace.Action) (workspace.Role, error)
}

// SiteSaver stores a quick connection as a site.
type SiteSaver interface {
	// Create saves a site with its secrets, opened with the vault key of the session.
	Create(ctx context.Context, actor, workspaceID uuid.UUID, session string, in site.Input, changes site.SecretChanges) (site.Site, error)
}

// Service manages quick connections. It is safe for concurrent use.
type Service struct {
	authz Authorizer
	guard Guard
	saver SiteSaver
	clock ports.Clock
	newID func() (uuid.UUID, error)

	mu    sync.Mutex
	byID  map[uuid.UUID]*Connection
	byKey map[uuid.UUID][]uuid.UUID
}

// NewService returns a service. newID may be nil to use UUIDv7.
func NewService(authz Authorizer, guard Guard, saver SiteSaver, clock ports.Clock, newID func() (uuid.UUID, error)) *Service {
	if newID == nil {
		newID = uuid.NewV7
	}
	return &Service{authz: authz, guard: guard, saver: saver, clock: clock, newID: newID, byID: map[uuid.UUID]*Connection{}, byKey: map[uuid.UUID][]uuid.UUID{}}
}

// Create validates the settings, vets the host and starts keeping a quick connection. The site name is the host.
// It needs the permission to manage sites, since connecting to a server is what a saved site would allow too.
func (s *Service) Create(ctx context.Context, actor, workspaceID uuid.UUID, in site.Input, secrets Secrets) (Connection, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return Connection{}, err
	}
	in.Name = in.Host
	if in.AuthMethod == "" {
		in.AuthMethod = site.AuthPassword
	}
	in, err := site.Normalize(in)
	if err != nil {
		return Connection{}, err
	}
	if err := s.guard.CheckHost(ctx, in.Host); err != nil {
		return Connection{}, err
	}
	id, err := s.newID()
	if err != nil {
		return Connection{}, err
	}
	now := s.clock.Now().UTC()
	c := &Connection{
		UserID:   actor,
		Secrets:  secrets,
		lastUsed: now,
		Site: site.Site{
			ID: id, WorkspaceID: workspaceID, Name: in.Name, Protocol: in.Protocol, Host: in.Host, Port: in.Port, Username: in.Username,
			AuthMethod: in.AuthMethod, ListTimezone: in.ListTimezone, Charset: in.Charset, CreatedAt: now, UpdatedAt: now,
		},
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.byKey[actor]) >= maxPerUser {
		return Connection{}, ErrTooMany
	}
	s.byID[id] = c
	s.byKey[actor] = append(s.byKey[actor], id)
	return *c, nil
}

// Lookup returns a quick connection of the user in the workspace and marks it as used. It reports false for
// unknown identifiers and for connections of other users or workspaces. The returned secrets are copies that
// the caller wipes.
func (s *Service) Lookup(userID, workspaceID, id uuid.UUID) (Connection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.byID[id]
	if !ok || c.UserID != userID || c.Site.WorkspaceID != workspaceID {
		return Connection{}, false
	}
	c.lastUsed = s.clock.Now().UTC()
	out := *c
	out.Secrets = Secrets{Password: append([]byte(nil), c.Secrets.Password...), PrivateKey: append([]byte(nil), c.Secrets.PrivateKey...), Passphrase: append([]byte(nil), c.Secrets.Passphrase...)}
	return out, true
}

// Describe returns the settings of a quick connection of the user, without secrets, and marks it as used.
func (s *Service) Describe(userID, workspaceID, id uuid.UUID) (site.Site, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.byID[id]
	if !ok || c.UserID != userID || c.Site.WorkspaceID != workspaceID {
		return site.Site{}, false
	}
	c.lastUsed = s.clock.Now().UTC()
	return c.Site, true
}

// List returns the user's quick connections of a workspace, oldest first, without their secrets.
func (s *Service) List(ctx context.Context, userID, workspaceID uuid.UUID) ([]Connection, error) {
	if _, err := s.authz.Authorize(ctx, userID, workspaceID, workspace.ActionBrowse); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Connection
	for _, id := range s.byKey[userID] {
		if c := s.byID[id]; c.Site.WorkspaceID == workspaceID {
			out = append(out, Connection{Site: c.Site, UserID: c.UserID})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Site.CreatedAt.Before(out[j].Site.CreatedAt) })
	return out, nil
}

// remove forgets a connection and wipes its secrets; the caller holds the lock.
func (s *Service) remove(c *Connection) {
	c.Secrets.wipe()
	delete(s.byID, c.Site.ID)
	ids := s.byKey[c.UserID]
	for i, id := range ids {
		if id == c.Site.ID {
			s.byKey[c.UserID] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	if len(s.byKey[c.UserID]) == 0 {
		delete(s.byKey, c.UserID)
	}
}

// Close ends a quick connection of the user and wipes its secrets, or returns [ErrNotFound].
func (s *Service) Close(userID, workspaceID, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.byID[id]
	if !ok || c.UserID != userID || c.Site.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	s.remove(c)
	return nil
}

// CloseUser ends every quick connection of a user, for example when they sign out, and returns their identifiers.
func (s *Service) CloseUser(userID uuid.UUID) []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := append([]uuid.UUID(nil), s.byKey[userID]...)
	for _, id := range ids {
		s.remove(s.byID[id])
	}
	return ids
}

// Sweep ends the quick connections unused for longer than idle and returns how many it ended.
func (s *Service) Sweep(idle time.Duration) int {
	now := s.clock.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	ended := 0
	for _, c := range s.byID {
		if now.Sub(c.lastUsed) >= idle {
			s.remove(c)
			ended++
		}
	}
	return ended
}

// Save stores a quick connection as a site named name, in the given collection, and ends the quick connection.
// The vault must be unlocked in the session to seal the credentials; when it is not the quick connection is kept.
func (s *Service) Save(ctx context.Context, userID, workspaceID, id uuid.UUID, session, name string, collectionID uuid.UUID) (site.Site, error) {
	c, ok := s.Lookup(userID, workspaceID, id)
	if !ok {
		return site.Site{}, ErrNotFound
	}
	defer c.Secrets.wipe()
	in := site.Input{
		Name: name, CollectionID: collectionID, Protocol: c.Site.Protocol, Host: c.Site.Host, Port: c.Site.Port, Username: c.Site.Username,
		AuthMethod: c.Site.AuthMethod, ListTimezone: c.Site.ListTimezone, Charset: c.Site.Charset,
	}
	saved, err := s.saver.Create(ctx, userID, workspaceID, session, in, changesFor(c.Secrets))
	if err != nil {
		return site.Site{}, err
	}
	_ = s.Close(userID, workspaceID, id)
	return saved, nil
}

// changesFor turns the secrets that exist into secret changes.
func changesFor(secrets Secrets) site.SecretChanges {
	var changes site.SecretChanges
	for _, entry := range []struct {
		value []byte
		into  **string
	}{{secrets.Password, &changes.Password}, {secrets.PrivateKey, &changes.PrivateKey}, {secrets.Passphrase, &changes.Passphrase}} {
		if len(entry.value) > 0 {
			text := string(entry.value)
			*entry.into = &text
		}
	}
	return changes
}
