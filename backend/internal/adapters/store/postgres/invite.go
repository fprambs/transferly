package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	"github.com/fprambs/sentinel/internal/core/invite"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

var _ invite.Store = (*Store)(nil)

// Create implements [invite.Store]: the pending invitation for the same email is replaced in one transaction.
func (s *Store) Create(ctx context.Context, i invite.Invitation, tokenHash []byte) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.queries.WithTx(tx)
	if err := q.DeleteInvitationForEmail(ctx, db.DeleteInvitationForEmailParams{WorkspaceID: i.WorkspaceID, Email: i.Email}); err != nil {
		return err
	}
	err = q.InsertInvitation(ctx, db.InsertInvitationParams{
		ID: i.ID, WorkspaceID: i.WorkspaceID, Email: i.Email, Role: string(i.Role), TokenHash: tokenHash,
		InvitedBy: uuid.NullUUID{UUID: i.InvitedBy, Valid: i.InvitedBy != uuid.Nil}, CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt,
	})
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// invitationFrom converts the columns shared by the invitation queries.
func invitationFrom(id, workspaceID uuid.UUID, workspaceName, email, role string, invitedBy uuid.NullUUID, invitedByEmail string, created, expires time.Time) invite.Invitation {
	return invite.Invitation{
		ID: id, WorkspaceID: workspaceID, WorkspaceName: workspaceName, Email: email, Role: workspace.Role(role),
		InvitedBy: invitedBy.UUID, InvitedByEmail: invitedByEmail, CreatedAt: created.UTC(), ExpiresAt: expires.UTC(),
	}
}

// ByTokenHash implements [invite.Store].
func (s *Store) ByTokenHash(ctx context.Context, hash []byte, now time.Time) (invite.Invitation, error) {
	r, err := s.queries.InvitationByTokenHash(ctx, db.InvitationByTokenHashParams{TokenHash: hash, ExpiresAt: now})
	if errors.Is(err, errNoRows) {
		return invite.Invitation{}, invite.ErrNotFound
	}
	if err != nil {
		return invite.Invitation{}, err
	}
	return invitationFrom(r.ID, r.WorkspaceID, r.WorkspaceName, r.Email, r.Role, r.InvitedBy, r.InvitedByEmail, r.CreatedAt, r.ExpiresAt), nil
}

// Pending implements [invite.Store].
func (s *Store) Pending(ctx context.Context, workspaceID uuid.UUID, now time.Time) ([]invite.Invitation, error) {
	rows, err := s.queries.PendingInvitations(ctx, db.PendingInvitationsParams{WorkspaceID: workspaceID, ExpiresAt: now})
	if err != nil {
		return nil, err
	}
	out := make([]invite.Invitation, 0, len(rows))
	for _, r := range rows {
		out = append(out, invitationFrom(r.ID, r.WorkspaceID, r.WorkspaceName, r.Email, r.Role, r.InvitedBy, r.InvitedByEmail, r.CreatedAt, r.ExpiresAt))
	}
	return out, nil
}

// CountPending implements [invite.Store].
func (s *Store) CountPending(ctx context.Context, workspaceID uuid.UUID, now time.Time) (int, error) {
	n, err := s.queries.CountPendingInvitations(ctx, db.CountPendingInvitationsParams{WorkspaceID: workspaceID, ExpiresAt: now})
	return int(n), err
}

// Revoke implements [invite.Store].
func (s *Store) Revoke(ctx context.Context, workspaceID, id uuid.UUID) error {
	n, err := s.queries.DeleteInvitation(ctx, db.DeleteInvitationParams{WorkspaceID: workspaceID, ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return invite.ErrNotFound
	}
	return nil
}

// Accept implements [invite.Store]: the invitation is consumed and the membership created in one transaction, so a token works once.
func (s *Store) Accept(ctx context.Context, id, userID uuid.UUID, now time.Time) (workspace.Summary, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return workspace.Summary{}, fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.queries.WithTx(tx)
	consumed, err := q.ConsumeInvitation(ctx, db.ConsumeInvitationParams{ID: id, ExpiresAt: now})
	if errors.Is(err, errNoRows) {
		return workspace.Summary{}, invite.ErrNotFound
	}
	if err != nil {
		return workspace.Summary{}, err
	}
	if err := q.InsertMembershipIfMissing(ctx, db.InsertMembershipIfMissingParams{WorkspaceID: consumed.WorkspaceID, UserID: userID, Role: consumed.Role}); err != nil {
		return workspace.Summary{}, err
	}
	w, err := q.GetWorkspace(ctx, consumed.WorkspaceID)
	if err != nil {
		return workspace.Summary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return workspace.Summary{}, err
	}
	return workspace.Summary{
		Workspace: workspace.Workspace{ID: w.ID, Name: w.Name, Kind: workspace.Kind(w.Kind), OwnerID: w.OwnerID, CreatedAt: w.CreatedAt.UTC()},
		Role:      workspace.Role(consumed.Role),
	}, nil
}

// PurgeExpired implements [invite.Store].
func (s *Store) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	n, err := s.queries.PurgeExpiredInvitations(ctx, now)
	return int(n), err
}
