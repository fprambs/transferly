package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fprambs/transferly/internal/adapters/store/postgres/db"
	"github.com/fprambs/transferly/internal/core/workspace"
)

var _ workspace.Store = (*Store)(nil)

// CreateWorkspace implements [workspace.Store]: the workspace and its owner membership are stored in one transaction.
func (s *Store) CreateWorkspace(ctx context.Context, w workspace.Workspace) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.queries.WithTx(tx)
	err = q.InsertWorkspace(ctx, db.InsertWorkspaceParams{ID: w.ID, Name: w.Name, Kind: string(w.Kind), OwnerID: w.OwnerID, CreatedAt: w.CreatedAt})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return workspace.ErrExists
	}
	if err != nil {
		return err
	}
	if err := q.InsertMembership(ctx, db.InsertMembershipParams{WorkspaceID: w.ID, UserID: w.OwnerID, Role: string(workspace.RoleOwner)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PersonalWorkspace implements [workspace.Store].
func (s *Store) PersonalWorkspace(ctx context.Context, userID uuid.UUID) (workspace.Workspace, error) {
	r, err := s.queries.PersonalWorkspace(ctx, userID)
	if err != nil {
		return workspace.Workspace{}, workspaceNotFound(err)
	}
	return workspace.Workspace{ID: r.ID, Name: r.Name, Kind: workspace.Kind(r.Kind), OwnerID: r.OwnerID, CreatedAt: r.CreatedAt.UTC()}, nil
}

// WorkspacesForUser implements [workspace.Store].
func (s *Store) WorkspacesForUser(ctx context.Context, userID uuid.UUID) ([]workspace.Summary, error) {
	rows, err := s.queries.WorkspacesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]workspace.Summary, 0, len(rows))
	for _, r := range rows {
		out = append(out, workspace.Summary{
			Workspace: workspace.Workspace{ID: r.ID, Name: r.Name, Kind: workspace.Kind(r.Kind), OwnerID: r.OwnerID, CreatedAt: r.CreatedAt.UTC()},
			Role:      workspace.Role(r.Role),
		})
	}
	return out, nil
}

// RoleOf implements [workspace.Store].
func (s *Store) RoleOf(ctx context.Context, workspaceID, userID uuid.UUID) (workspace.Role, error) {
	role, err := s.queries.MembershipRole(ctx, db.MembershipRoleParams{WorkspaceID: workspaceID, UserID: userID})
	if err != nil {
		return "", workspaceNotFound(err)
	}
	return workspace.Role(role), nil
}

// workspaceNotFound maps a missing row to [workspace.ErrNotFound].
func workspaceNotFound(err error) error {
	if errors.Is(err, errNoRows) {
		return workspace.ErrNotFound
	}
	return err
}

// Workspace implements [workspace.Store].
func (s *Store) Workspace(ctx context.Context, workspaceID uuid.UUID) (workspace.Workspace, error) {
	r, err := s.queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return workspace.Workspace{}, workspaceNotFound(err)
	}
	return workspace.Workspace{ID: r.ID, Name: r.Name, Kind: workspace.Kind(r.Kind), OwnerID: r.OwnerID, CreatedAt: r.CreatedAt.UTC()}, nil
}

// Members implements [workspace.Store].
func (s *Store) Members(ctx context.Context, workspaceID uuid.UUID) ([]workspace.Member, error) {
	rows, err := s.queries.WorkspaceMembers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]workspace.Member, 0, len(rows))
	for _, r := range rows {
		out = append(out, workspace.Member{UserID: r.UserID, Email: r.Email, Role: workspace.Role(r.Role), JoinedAt: r.CreatedAt.UTC()})
	}
	return out, nil
}

// SetRole implements [workspace.Store]; the owner's membership is never touched.
func (s *Store) SetRole(ctx context.Context, workspaceID, userID uuid.UUID, role workspace.Role) error {
	n, err := s.queries.UpdateMembershipRole(ctx, db.UpdateMembershipRoleParams{WorkspaceID: workspaceID, UserID: userID, Role: string(role)})
	if err != nil {
		return err
	}
	if n == 0 {
		return workspace.ErrMemberNotFound
	}
	return nil
}

// RemoveMember implements [workspace.Store]: the membership and the copies of the workspace key of the member are deleted and the
// workspace is marked as needing a new key, in one transaction. The owner's membership is never deleted.
func (s *Store) RemoveMember(ctx context.Context, workspaceID, userID uuid.UUID) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.queries.WithTx(tx)
	n, err := q.DeleteMembership(ctx, db.DeleteMembershipParams{WorkspaceID: workspaceID, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return workspace.ErrMemberNotFound
	}
	if err := q.DeleteMemberGrants(ctx, db.DeleteMemberGrantsParams{WorkspaceID: workspaceID, UserID: userID}); err != nil {
		return err
	}
	if err := q.MarkRekeyPending(ctx, workspaceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// TransferOwnership implements [workspace.Store]: the three writes happen in one transaction.
func (s *Store) TransferOwnership(ctx context.Context, workspaceID, from, to uuid.UUID) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.queries.WithTx(tx)
	for _, change := range []struct {
		user uuid.UUID
		role workspace.Role
	}{{to, workspace.RoleOwner}, {from, workspace.RoleAdmin}} {
		n, err := q.SetMembershipRole(ctx, db.SetMembershipRoleParams{WorkspaceID: workspaceID, UserID: change.user, Role: string(change.role)})
		if err != nil {
			return err
		}
		if n == 0 {
			return workspace.ErrMemberNotFound
		}
	}
	if err := q.SetWorkspaceOwner(ctx, db.SetWorkspaceOwnerParams{ID: workspaceID, OwnerID: to}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AddMember implements [workspace.Store].
func (s *Store) AddMember(ctx context.Context, workspaceID, userID uuid.UUID, role workspace.Role) error {
	err := s.queries.InsertMembership(ctx, db.InsertMembershipParams{WorkspaceID: workspaceID, UserID: userID, Role: string(role)})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return workspace.ErrAlreadyMember
	}
	return err
}
