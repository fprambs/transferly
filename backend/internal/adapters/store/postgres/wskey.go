package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/core/wskey"
)

var _ wskey.Store = (*Store)(nil)

// UserKeys implements [wskey.Store]; a user without a vault row or with an empty key pair has no keys.
func (s *Store) UserKeys(ctx context.Context, userID uuid.UUID) (wskey.UserKeys, error) {
	r, err := s.queries.UserKeys(ctx, userID)
	if errors.Is(err, errNoRows) || (err == nil && r.PublicKey == nil) {
		return wskey.UserKeys{}, wskey.ErrNoKeys
	}
	if err != nil {
		return wskey.UserKeys{}, err
	}
	return wskey.UserKeys{Public: r.PublicKey, SealedPrivate: r.SealedPrivateKey}, nil
}

// SetUserKeys implements [wskey.Store].
func (s *Store) SetUserKeys(ctx context.Context, userID uuid.UUID, keys wskey.UserKeys) (bool, error) {
	n, err := s.queries.SetUserKeys(ctx, db.SetUserKeysParams{UserID: userID, PublicKey: keys.Public, SealedPrivateKey: keys.SealedPrivate})
	return n > 0, err
}

// KeyState implements [wskey.Store].
func (s *Store) KeyState(ctx context.Context, workspaceID uuid.UUID) (int, bool, error) {
	r, err := s.queries.WorkspaceKeyState(ctx, workspaceID)
	if errors.Is(err, errNoRows) {
		return 0, false, workspace.ErrNotFound
	}
	return int(r.KeyVersion), r.RekeyPending, err
}

// Grant implements [wskey.Store].
func (s *Store) Grant(ctx context.Context, workspaceID, userID uuid.UUID, version int) ([]byte, error) {
	sealed, err := s.queries.KeyGrant(ctx, db.KeyGrantParams{WorkspaceID: workspaceID, UserID: userID, Version: int32(version)})
	if errors.Is(err, errNoRows) {
		return nil, wskey.ErrPending
	}
	return sealed, err
}

// AddGrant implements [wskey.Store].
func (s *Store) AddGrant(ctx context.Context, workspaceID, userID uuid.UUID, version int, sealed []byte) error {
	return s.queries.InsertKeyGrant(ctx, db.InsertKeyGrantParams{WorkspaceID: workspaceID, UserID: userID, Version: int32(version), Sealed: sealed})
}

// Ungranted implements [wskey.Store].
func (s *Store) Ungranted(ctx context.Context, workspaceID uuid.UUID, version int) ([]wskey.Recipient, error) {
	rows, err := s.queries.UngrantedMembers(ctx, db.UngrantedMembersParams{WorkspaceID: workspaceID, Version: int32(version)})
	if err != nil {
		return nil, err
	}
	out := make([]wskey.Recipient, 0, len(rows))
	for _, r := range rows {
		out = append(out, wskey.Recipient{UserID: r.UserID, Public: r.PublicKey})
	}
	return out, nil
}

// Granted implements [wskey.Store].
func (s *Store) Granted(ctx context.Context, workspaceID uuid.UUID, version int) ([]uuid.UUID, error) {
	return s.queries.GrantedMembers(ctx, db.GrantedMembersParams{WorkspaceID: workspaceID, Version: int32(version)})
}

// CreateTeam implements [wskey.Store]: the workspace, the owner membership and the first grant are stored in one transaction.
func (s *Store) CreateTeam(ctx context.Context, w workspace.Workspace, sealed []byte) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.queries.WithTx(tx)
	if err := q.InsertWorkspace(ctx, db.InsertWorkspaceParams{ID: w.ID, Name: w.Name, Kind: string(w.Kind), OwnerID: w.OwnerID, CreatedAt: w.CreatedAt}); err != nil {
		return err
	}
	if err := q.InsertMembership(ctx, db.InsertMembershipParams{WorkspaceID: w.ID, UserID: w.OwnerID, Role: string(workspace.RoleOwner)}); err != nil {
		return err
	}
	if err := q.InsertKeyGrant(ctx, db.InsertKeyGrantParams{WorkspaceID: w.ID, UserID: w.OwnerID, Version: 1, Sealed: sealed}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
