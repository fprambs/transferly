package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	"github.com/fprambs/sentinel/internal/core/mfa"
)

var _ mfa.Store = (*Store)(nil)

// Factor implements [mfa.Store].
func (s *Store) Factor(ctx context.Context, userID uuid.UUID) (mfa.Factor, error) {
	r, err := s.queries.TotpFactor(ctx, userID)
	if errors.Is(err, errNoRows) {
		return mfa.Factor{}, mfa.ErrNotEnrolled
	}
	if err != nil {
		return mfa.Factor{}, err
	}
	return mfa.Factor{Sealed: r.Sealed, Confirmed: r.Confirmed, LastStep: r.LastStep}, nil
}

// SaveFactor implements [mfa.Store].
func (s *Store) SaveFactor(ctx context.Context, userID uuid.UUID, sealed []byte) error {
	return s.queries.UpsertTotp(ctx, db.UpsertTotpParams{UserID: userID, Sealed: sealed})
}

// ConfirmFactor implements [mfa.Store].
func (s *Store) ConfirmFactor(ctx context.Context, userID uuid.UUID, step int64, recovery [][]byte) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		if err := q.ConfirmTotp(ctx, db.ConfirmTotpParams{UserID: userID, LastStep: step}); err != nil {
			return err
		}
		return insertRecovery(ctx, q, userID, recovery)
	})
}

// insertRecovery replaces the recovery codes of a user.
func insertRecovery(ctx context.Context, q *db.Queries, userID uuid.UUID, recovery [][]byte) error {
	if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
		return err
	}
	for _, hash := range recovery {
		if err := q.InsertRecoveryCode(ctx, db.InsertRecoveryCodeParams{UserID: userID, CodeHash: hash}); err != nil {
			return err
		}
	}
	return nil
}

// AdvanceStep implements [mfa.Store]: the update is conditional, so two requests with the same code cannot both succeed.
func (s *Store) AdvanceStep(ctx context.Context, userID uuid.UUID, step int64) (bool, error) {
	n, err := s.queries.AdvanceTotpStep(ctx, db.AdvanceTotpStepParams{UserID: userID, LastStep: step})
	return n > 0, err
}

// DeleteFactor implements [mfa.Store].
func (s *Store) DeleteFactor(ctx context.Context, userID uuid.UUID) error {
	return s.queries.DeleteTotp(ctx, userID)
}

// ReplaceRecovery implements [mfa.Store].
func (s *Store) ReplaceRecovery(ctx context.Context, userID uuid.UUID, recovery [][]byte) error {
	return s.inTx(ctx, func(q *db.Queries) error { return insertRecovery(ctx, q, userID, recovery) })
}

// UseRecovery implements [mfa.Store].
func (s *Store) UseRecovery(ctx context.Context, userID uuid.UUID, hash []byte) (bool, error) {
	n, err := s.queries.UseRecoveryCode(ctx, db.UseRecoveryCodeParams{UserID: userID, CodeHash: hash})
	return n > 0, err
}

// RecoveryLeft implements [mfa.Store].
func (s *Store) RecoveryLeft(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := s.queries.CountRecoveryCodes(ctx, userID)
	return int(n), err
}

// inTx runs work in a transaction that commits when it returns nil.
func (s *Store) inTx(ctx context.Context, work func(q *db.Queries) error) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := work(s.queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
