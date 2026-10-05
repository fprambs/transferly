package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres/db"
	"github.com/fprambs/transferly/internal/core/vault"
)

var _ vault.SessionStore = (*Store)(nil)

// SaveVaultSession implements [vault.SessionStore].
func (s *Store) SaveVaultSession(ctx context.Context, session string, sealed []byte) error {
	id, err := uuid.Parse(session)
	if err != nil {
		return err
	}
	return s.queries.SaveVaultSession(ctx, db.SaveVaultSessionParams{SessionID: id, Sealed: sealed})
}

// VaultSession implements [vault.SessionStore].
func (s *Store) VaultSession(ctx context.Context, session string) ([]byte, error) {
	id, err := uuid.Parse(session)
	if err != nil {
		return nil, vault.ErrNoSession
	}
	sealed, err := s.queries.GetVaultSession(ctx, id)
	if errors.Is(err, errNoRows) {
		return nil, vault.ErrNoSession
	}
	return sealed, err
}

// DeleteVaultSession implements [vault.SessionStore].
func (s *Store) DeleteVaultSession(ctx context.Context, session string) error {
	id, err := uuid.Parse(session)
	if err != nil {
		return nil
	}
	return s.queries.DeleteVaultSession(ctx, id)
}
