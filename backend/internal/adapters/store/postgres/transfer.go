package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fprambs/transferly/internal/adapters/store/postgres/db"
	"github.com/fprambs/transferly/internal/core/transfer"
)

var _ transfer.Store = (*Store)(nil)

// utc returns a copy of an optional time in UTC.
func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// transferFrom converts a stored row to a transfer.
func transferFrom(r db.Transfer) transfer.Transfer {
	return transfer.Transfer{
		ID: r.ID, WorkspaceID: r.WorkspaceID, UserID: r.UserID, SiteID: r.SiteID.UUID, SiteName: r.SiteName,
		Direction: transfer.Direction(r.Direction), Path: r.Path, Name: r.Name, Status: transfer.Status(r.Status),
		TotalBytes: r.TotalBytes, BytesDone: r.BytesDone, ErrorCode: r.ErrorCode, Attempts: int(r.Attempts),
		CreatedAt: r.CreatedAt.UTC(), StartedAt: utc(r.StartedAt), UpdatedAt: r.UpdatedAt.UTC(), FinishedAt: utc(r.FinishedAt),
	}
}

// CreateTransfer implements [transfer.Store].
func (s *Store) CreateTransfer(ctx context.Context, t transfer.Transfer) error {
	err := s.queries.InsertTransfer(ctx, db.InsertTransferParams{
		ID: t.ID, WorkspaceID: t.WorkspaceID, UserID: t.UserID, SiteID: uuid.NullUUID{UUID: t.SiteID, Valid: t.SiteID != uuid.Nil},
		SiteName: t.SiteName, Direction: string(t.Direction), Path: t.Path, Name: t.Name, Status: string(t.Status),
		TotalBytes: t.TotalBytes, BytesDone: t.BytesDone, ErrorCode: t.ErrorCode, Attempts: int32(t.Attempts),
		CreatedAt: t.CreatedAt, StartedAt: t.StartedAt, UpdatedAt: t.UpdatedAt, FinishedAt: t.FinishedAt,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return transfer.ErrExists
	}
	return err
}

// UpdateTransfer implements [transfer.Store].
func (s *Store) UpdateTransfer(ctx context.Context, t transfer.Transfer) error {
	rows, err := s.queries.UpdateTransfer(ctx, db.UpdateTransferParams{
		ID: t.ID, UserID: t.UserID, Status: string(t.Status), TotalBytes: t.TotalBytes, BytesDone: t.BytesDone,
		ErrorCode: t.ErrorCode, Attempts: int32(t.Attempts), StartedAt: t.StartedAt, UpdatedAt: t.UpdatedAt, FinishedAt: t.FinishedAt,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return transfer.ErrNotFound
	}
	return nil
}

// TransferByID implements [transfer.Store].
func (s *Store) TransferByID(ctx context.Context, userID, id uuid.UUID) (transfer.Transfer, error) {
	r, err := s.queries.TransferByID(ctx, db.TransferByIDParams{ID: id, UserID: userID})
	if errors.Is(err, errNoRows) {
		return transfer.Transfer{}, transfer.ErrNotFound
	}
	if err != nil {
		return transfer.Transfer{}, err
	}
	return transferFrom(r), nil
}

// ListTransfers implements [transfer.Store].
func (s *Store) ListTransfers(ctx context.Context, userID, workspaceID uuid.UUID, limit int) ([]transfer.Transfer, error) {
	var rows []db.Transfer
	var err error
	if workspaceID == uuid.Nil {
		rows, err = s.queries.TransfersByUser(ctx, db.TransfersByUserParams{UserID: userID, Limit: int32(limit)})
	} else {
		rows, err = s.queries.TransfersByUserWorkspace(ctx, db.TransfersByUserWorkspaceParams{UserID: userID, WorkspaceID: workspaceID, Limit: int32(limit)})
	}
	if err != nil {
		return nil, err
	}
	out := make([]transfer.Transfer, 0, len(rows))
	for _, r := range rows {
		out = append(out, transferFrom(r))
	}
	return out, nil
}

// DeleteTransfer implements [transfer.Store].
func (s *Store) DeleteTransfer(ctx context.Context, userID, id uuid.UUID) error {
	rows, err := s.queries.DeleteTransfer(ctx, db.DeleteTransferParams{ID: id, UserID: userID})
	if err != nil {
		return err
	}
	if rows == 0 {
		return transfer.ErrNotFound
	}
	return nil
}

// InterruptActive implements [transfer.Store].
func (s *Store) InterruptActive(ctx context.Context, at time.Time) (int, error) {
	rows, err := s.queries.InterruptActiveTransfers(ctx, &at)
	return int(rows), err
}
