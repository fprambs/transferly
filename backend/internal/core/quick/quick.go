// Package quick keeps quick connections: servers a user tries out without saving anything. Their credentials live
// only in memory, are wiped when the connection ends, and can be saved as a site once the server proved to work.
package quick

import (
	"context"
	"errors"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/vault"
	"github.com/fprambs/transferly/internal/core/workspace"
)

// maxPerUser is how many quick connections a user may keep at once.
const maxPerUser = 5

var (
	// ErrNotFound means the user has no such quick connection.
	ErrNotFound = errors.New("quick connection not found")
	// ErrTooMany means the user reached the number of quick connections they may keep.
	ErrTooMany = errors.New("too many quick connections")
)

// Secrets are the credentials of a quick connection, by the name of their field.
type Secrets map[site.SecretKind][]byte

// wipe overwrites the secrets in memory.
func (s Secrets) wipe() {
	for _, value := range s {
		vault.Wipe(value)
	}
}

// clone copies the secrets, so the caller can wipe its copy.
func (s Secrets) clone() Secrets {
	out := make(Secrets, len(s))
	for kind, value := range s {
		out[kind] = append([]byte(nil), value...)
	}
	return out
}

// Connection is a quick connection. Its Site is never stored; its ID stands in for a site identifier.
type Connection struct {
	// Site holds the settled settings; Site.ID is the identifier clients use where a site identifier is expected.
	Site site.Site
	// Secrets are the credentials, in memory only.
	Secrets Secrets
	// UserID is the user the connection belongs to; nobody else can use it.
	UserID uuid.UUID

	lastUsed time.Time
}

// Authorizer checks that a user may perform an action in a workspace.
type Authorizer interface {
	// Authorize returns the role of the user or an error that says why the action is refused.
	Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action workspace.Action) (workspace.Role, error)
}

// Sites settles quick connections the way sites are settled and stores one as a site.
type Sites interface {
	// Settle validates the settings and fills in the defaults of the connector's schema.
	Settle(in site.Input) (site.Input, connectors.Descriptor, error)
	// CheckHosts asks the network guard about every host a settled configuration points to.
	CheckHosts(ctx context.Context, d connectors.Descriptor, config map[string]string) error
	// Create saves a site with its secrets, opened with the vault key of the session.
	Create(ctx context.Context, actor, workspaceID uuid.UUID, session string, in site.Input, changes site.SecretChanges) (site.Site, error)
}

// Service manages quick connections. It is safe for concurrent use.
type Service struct {
	authz Authorizer
	sites Sites
	clock ports.Clock
	newID func() (uuid.UUID, error)

	mu    sync.Mutex
	byID  map[uuid.UUID]*Connection
	byKey map[uuid.UUID][]uuid.UUID
}

// NewService returns a service. newID may be nil to use UUIDv7.
func NewService(authz Authorizer, sites Sites, clock ports.Clock, newID func() (uuid.UUID, error)) *Service {
	if newID == nil {
		newID = uuid.NewV7
	}
	return &Service{authz: authz, sites: sites, clock: clock, newID: newID, byID: map[uuid.UUID]*Connection{}, byKey: map[uuid.UUID][]uuid.UUID{}}
}

// unattended are the logon types a quick connection cannot use: they ask the person connecting or store nothing, which only a saved site
// can do.
var unattended = []string{connectors.AuthAsk, connectors.AuthAnonymous, connectors.AuthInteractive}

// Create validates the settings by the schema of their connector, reads the private keys and vets the hosts as for a saved site, and starts
// keeping a quick connection named after its host. It needs the permission to manage sites, since connecting to a server is what a saved
// site would allow too. Logon types that ask when connecting or store nothing are a field error of the auth field.
func (s *Service) Create(ctx context.Context, actor, workspaceID uuid.UUID, in site.Input, secrets Secrets) (Connection, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return Connection{}, err
	}
	in.Name = in.Config[connectors.FieldHost]
	in, d, err := s.sites.Settle(in)
	if err != nil {
		return Connection{}, err
	}
	if slices.Contains(unattended, in.Config[connectors.FieldAuth]) {
		return Connection{}, &connectors.FieldError{Field: connectors.FieldAuth, Reason: connectors.ReasonChoice}
	}
	changes, err := site.PlanSecrets(d, in.Config, changesFor(secrets), nil)
	if err != nil {
		return Connection{}, err
	}
	if err := s.sites.CheckHosts(ctx, d, in.Config); err != nil {
		return Connection{}, err
	}
	id, err := s.newID()
	if err != nil {
		return Connection{}, err
	}
	kept := keep(secrets, changes)
	now := s.clock.Now().UTC()
	c := &Connection{
		UserID:   actor,
		Secrets:  kept,
		lastUsed: now,
		Site:     site.Site{ID: id, WorkspaceID: workspaceID, Name: in.Name, Connector: in.Connector, Config: in.Config, SpeedLimit: in.SpeedLimit, CreatedAt: now, UpdatedAt: now},
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
	out.Secrets = c.Secrets.clone()
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
	in := site.Input{Name: name, CollectionID: collectionID, Connector: c.Site.Connector, Config: c.Site.Config}
	saved, err := s.sites.Create(ctx, userID, workspaceID, session, in, changesFor(c.Secrets))
	if err != nil {
		return site.Site{}, err
	}
	_ = s.Close(userID, workspaceID, id)
	return saved, nil
}

// keep returns the secrets a quick connection holds after its changes were settled. A secret that came through unchanged keeps the bytes it
// was given, which the service now owns and wipes when the connection ends; one that was rewritten, such as a key converted to OpenSSH's
// format, is a new copy, and the bytes it replaces are wiped at once, like those of secrets that were dropped.
func keep(given Secrets, changes site.SecretChanges) Secrets {
	kept := Secrets{}
	for kind, value := range changes {
		if value == "" {
			continue
		}
		if original, ok := given[kind]; ok && string(original) == value {
			kept[kind] = original
			continue
		}
		kept[kind] = []byte(value)
	}
	for kind, original := range given {
		if held, ok := kept[kind]; len(original) > 0 && (!ok || &held[0] != &original[0]) {
			vault.Wipe(original)
		}
	}
	return kept
}

// changesFor turns the secrets into secret changes.
func changesFor(secrets Secrets) site.SecretChanges {
	changes := site.SecretChanges{}
	for kind, value := range secrets {
		changes[kind] = string(value)
	}
	return changes
}
