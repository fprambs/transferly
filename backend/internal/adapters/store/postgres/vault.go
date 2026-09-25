package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	"github.com/fprambs/sentinel/internal/core/vault"
)

var _ vault.Store = (*Store)(nil)

// CreateVault implements [vault.Store].
func (s *Store) CreateVault(ctx context.Context, userID uuid.UUID, e vault.Enrollment) error {
	err := s.queries.InsertVault(ctx, db.InsertVaultParams{
		UserID: userID, KdfMemoryKib: int32(e.Params.MemoryKiB), KdfTime: int32(e.Params.Time), KdfThreads: int32(e.Params.Threads),
		KdfSalt: e.Salt, WrappedDek: e.WrappedDEK, RecoveryWrappedDek: e.RecoveryWrappedDEK,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return vault.ErrVaultExists
	}
	return err
}

// Vault implements [vault.Store].
func (s *Store) Vault(ctx context.Context, userID uuid.UUID) (vault.Enrollment, error) {
	r, err := s.queries.VaultByUser(ctx, userID)
	if errors.Is(err, errNoRows) {
		return vault.Enrollment{}, vault.ErrNoVault
	}
	if err != nil {
		return vault.Enrollment{}, err
	}
	return vault.Enrollment{
		Params:     vault.Params{MemoryKiB: uint32(r.KdfMemoryKib), Time: uint32(r.KdfTime), Threads: uint8(r.KdfThreads)},
		Salt:       r.KdfSalt,
		WrappedDEK: r.WrappedDek, RecoveryWrappedDEK: r.RecoveryWrappedDek,
	}, nil
}

// UpdateVault implements [vault.Store].
func (s *Store) UpdateVault(ctx context.Context, userID uuid.UUID, e vault.Enrollment) error {
	rows, err := s.queries.UpdateVault(ctx, db.UpdateVaultParams{
		UserID: userID, KdfMemoryKib: int32(e.Params.MemoryKiB), KdfTime: int32(e.Params.Time), KdfThreads: int32(e.Params.Threads),
		KdfSalt: e.Salt, WrappedDek: e.WrappedDEK, RecoveryWrappedDek: e.RecoveryWrappedDEK,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return vault.ErrNoVault
	}
	return nil
}
