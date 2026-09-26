// Package wskey hands out the keys that seal the secrets of a workspace.
//
// A personal workspace uses the DEK of its owner. A team workspace has a key of its own, sealed for every member with their X25519
// public key (see ADR-0006 and the guide on workspace keys), so any member with an unlocked vault can open the secrets of the team
// and nobody needs to share a password.
package wskey

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// personalVersion is the key version reported for personal workspaces, which have no key of their own.
const personalVersion = 1

// Errors returned by the package.
var (
	// ErrNoKeys means the user has no key pair yet: their vault has never been unlocked since it was set up.
	ErrNoKeys = errors.New("the user has no key pair")
	// ErrPending means the workspace key has not been sealed for the user yet. An admin with an unlocked vault does it the next time
	// they open the workspace.
	ErrPending = errors.New("the workspace key has not been shared with the user yet")
)

// UserKeys is the persisted key pair of a user.
type UserKeys struct {
	// Public is the X25519 public key.
	Public []byte
	// SealedPrivate is the private key sealed with the user's DEK.
	SealedPrivate []byte
}

// Recipient is a member a workspace key has to be sealed for.
type Recipient struct {
	// UserID identifies the member.
	UserID uuid.UUID
	// Public is their public key.
	Public []byte
}

// Store persists key pairs and grants.
type Store interface {
	// UserKeys returns the key pair of a user, or [ErrNoKeys].
	UserKeys(ctx context.Context, userID uuid.UUID) (UserKeys, error)
	// SetUserKeys stores a key pair unless one exists; it reports whether it stored it.
	SetUserKeys(ctx context.Context, userID uuid.UUID, keys UserKeys) (bool, error)
	// KeyState returns the current key version of a workspace and whether a rotation is waiting.
	KeyState(ctx context.Context, workspaceID uuid.UUID) (version int, rekeyPending bool, err error)
	// Grant returns the copy of a workspace key sealed for a user, or [ErrPending].
	Grant(ctx context.Context, workspaceID, userID uuid.UUID, version int) ([]byte, error)
	// AddGrant stores a copy of a workspace key for a user; an existing copy is kept.
	AddGrant(ctx context.Context, workspaceID, userID uuid.UUID, version int, sealed []byte) error
	// Ungranted lists the members with a public key and no copy of the workspace key at a version.
	Ungranted(ctx context.Context, workspaceID uuid.UUID, version int) ([]Recipient, error)
	// Granted lists the members with a copy of the workspace key at a version.
	Granted(ctx context.Context, workspaceID uuid.UUID, version int) ([]uuid.UUID, error)
	// CreateTeam stores a team workspace, its owner membership and the first copy of its key in one step.
	CreateTeam(ctx context.Context, w workspace.Workspace, sealed []byte) error
}

// Vault gives the decrypted DEK of a session.
type Vault interface {
	// Key returns a copy of the session's DEK, or [vault.ErrLocked]. The caller wipes it.
	Key(session string) ([]byte, error)
}

// Workspaces reads workspaces and prepares team workspaces.
type Workspaces interface {
	// Get returns a workspace the actor belongs to, with their role.
	Get(ctx context.Context, actor, workspaceID uuid.UUID) (workspace.Summary, error)
	// List returns the workspaces a user belongs to.
	List(ctx context.Context, userID uuid.UUID) ([]workspace.Summary, error)
	// NewTeam validates a name and returns the team workspace the actor would own, without storing it.
	NewTeam(actor uuid.UUID, name string) (workspace.Workspace, error)
}

// Service hands out workspace keys.
type Service struct {
	store      Store
	vault      Vault
	workspaces Workspaces
	random     io.Reader
}

// NewService returns a service.
func NewService(store Store, vault Vault, workspaces Workspaces, random io.Reader) *Service {
	return &Service{store: store, vault: vault, workspaces: workspaces, random: random}
}

// Unlocked reports whether a session has its vault unlocked, which every use of a key needs.
func (s *Service) Unlocked(session string) bool {
	key, err := s.vault.Key(session)
	vault.Wipe(key)
	return err == nil
}

// Ensure creates the key pair of a user when they have none. It needs the session's vault unlocked, because the private key is
// sealed with the DEK. It is safe to call every time the vault is unlocked.
func (s *Service) Ensure(ctx context.Context, userID uuid.UUID, session string) error {
	_, err := s.publicKey(ctx, userID, session)
	return err
}

// publicKey returns the public key of a user, creating their key pair when missing.
func (s *Service) publicKey(ctx context.Context, userID uuid.UUID, session string) ([]byte, error) {
	keys, err := s.store.UserKeys(ctx, userID)
	if err == nil {
		return keys.Public, nil
	}
	if !errors.Is(err, ErrNoKeys) {
		return nil, err
	}
	dek, err := s.vault.Key(session)
	if err != nil {
		return nil, err
	}
	defer vault.Wipe(dek)
	pair, err := vault.GenerateKeyPair(s.random)
	if err != nil {
		return nil, err
	}
	defer vault.Wipe(pair.Private)
	sealed, err := vault.SealPrivateKey(s.random, dek, userID.String(), pair.Private)
	if err != nil {
		return nil, err
	}
	stored, err := s.store.SetUserKeys(ctx, userID, UserKeys{Public: pair.Public, SealedPrivate: sealed})
	if err != nil {
		return nil, err
	}
	if stored {
		return pair.Public, nil
	}
	existing, err := s.store.UserKeys(ctx, userID)
	return existing.Public, err
}

// privateKey opens the private key of a user with the session's DEK. The caller wipes it.
func (s *Service) privateKey(ctx context.Context, userID uuid.UUID, session string) ([]byte, error) {
	keys, err := s.store.UserKeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	dek, err := s.vault.Key(session)
	if err != nil {
		return nil, err
	}
	defer vault.Wipe(dek)
	return vault.OpenPrivateKey(dek, userID.String(), keys.SealedPrivate)
}

// CreateTeam creates a team workspace owned by the actor with a new key sealed for them. The actor needs a vault, unlocked in the
// session unless their key pair already exists.
func (s *Service) CreateTeam(ctx context.Context, actor uuid.UUID, session, name string) (workspace.Workspace, error) {
	w, err := s.workspaces.NewTeam(actor, name)
	if err != nil {
		return workspace.Workspace{}, err
	}
	public, err := s.publicKey(ctx, actor, session)
	if err != nil {
		return workspace.Workspace{}, err
	}
	key := make([]byte, vault.KeyLength)
	defer vault.Wipe(key)
	if _, err := io.ReadFull(s.random, key); err != nil {
		return workspace.Workspace{}, fmt.Errorf("generating the workspace key: %w", err)
	}
	sealed, err := vault.SealTo(s.random, public, vault.GrantRef{Workspace: w.ID.String(), User: actor.String(), Version: 1}, key)
	if err != nil {
		return workspace.Workspace{}, err
	}
	if err := s.store.CreateTeam(ctx, w, sealed); err != nil {
		return workspace.Workspace{}, err
	}
	return w, nil
}

// Current returns the key that seals new secrets of a workspace and its version. The caller wipes the key.
func (s *Service) Current(ctx context.Context, actor, workspaceID uuid.UUID, session string) ([]byte, int, error) {
	w, err := s.workspaces.Get(ctx, actor, workspaceID)
	if err != nil {
		return nil, 0, err
	}
	if w.Kind == workspace.KindPersonal {
		key, err := s.vault.Key(session)
		return key, personalVersion, err
	}
	version, _, err := s.store.KeyState(ctx, workspaceID)
	if err != nil {
		return nil, 0, err
	}
	key, err := s.open(ctx, actor, workspaceID, session, version)
	return key, version, err
}

// Version returns the key that opens the secrets sealed at a version. The caller wipes it.
func (s *Service) Version(ctx context.Context, actor, workspaceID uuid.UUID, session string, version int) ([]byte, error) {
	w, err := s.workspaces.Get(ctx, actor, workspaceID)
	if err != nil {
		return nil, err
	}
	if w.Kind == workspace.KindPersonal {
		return s.vault.Key(session)
	}
	return s.open(ctx, actor, workspaceID, session, version)
}

// open returns the key of a team workspace at a version, opening the copy sealed for the actor.
func (s *Service) open(ctx context.Context, actor, workspaceID uuid.UUID, session string, version int) ([]byte, error) {
	sealed, err := s.store.Grant(ctx, workspaceID, actor, version)
	if err != nil {
		return nil, err
	}
	private, err := s.privateKey(ctx, actor, session)
	if err != nil {
		return nil, err
	}
	defer vault.Wipe(private)
	return vault.OpenGrant(private, vault.GrantRef{Workspace: workspaceID.String(), User: actor.String(), Version: version}, sealed)
}

// Complete seals the current key of a team workspace for the members that have none yet and can receive one. The actor must hold
// the key and have the vault unlocked; a workspace whose key the actor lacks is left alone and reported as [ErrPending].
func (s *Service) Complete(ctx context.Context, actor, workspaceID uuid.UUID, session string) error {
	w, err := s.workspaces.Get(ctx, actor, workspaceID)
	if err != nil {
		return err
	}
	if w.Kind == workspace.KindPersonal {
		return nil
	}
	version, _, err := s.store.KeyState(ctx, workspaceID)
	if err != nil {
		return err
	}
	waiting, err := s.store.Ungranted(ctx, workspaceID, version)
	if err != nil || len(waiting) == 0 {
		return err
	}
	key, err := s.open(ctx, actor, workspaceID, session, version)
	if err != nil {
		return err
	}
	defer vault.Wipe(key)
	for _, r := range waiting {
		sealed, err := vault.SealTo(s.random, r.Public, vault.GrantRef{Workspace: workspaceID.String(), User: r.UserID.String(), Version: version}, key)
		if err != nil {
			return err
		}
		if err := s.store.AddGrant(ctx, workspaceID, r.UserID, version, sealed); err != nil {
			return err
		}
	}
	return nil
}

// Refresh prepares the keys of a user whose vault has just been unlocked: their key pair is created when missing and every team they
// belong to is completed, so members who joined before their own vault existed get their copy as soon as somebody with the key
// unlocks. Teams whose key the user has not received are skipped.
func (s *Service) Refresh(ctx context.Context, userID uuid.UUID, session string) error {
	if err := s.Ensure(ctx, userID, session); err != nil {
		return err
	}
	all, err := s.workspaces.List(ctx, userID)
	if err != nil {
		return err
	}
	for _, w := range all {
		if w.Kind != workspace.KindTeam {
			continue
		}
		if err := s.Complete(ctx, userID, w.ID, session); err != nil && !errors.Is(err, ErrPending) {
			return err
		}
	}
	return nil
}

// Sealed lists the members that hold a copy of the current key of a workspace.
func (s *Service) Sealed(ctx context.Context, actor, workspaceID uuid.UUID) (map[uuid.UUID]bool, error) {
	if _, err := s.workspaces.Get(ctx, actor, workspaceID); err != nil {
		return nil, err
	}
	version, _, err := s.store.KeyState(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	users, err := s.store.Granted(ctx, workspaceID, version)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]bool, len(users))
	for _, u := range users {
		out[u] = true
	}
	return out, nil
}
