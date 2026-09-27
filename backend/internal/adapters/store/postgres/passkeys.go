package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	"github.com/fprambs/sentinel/internal/core/passkey"
)

var _ passkey.Store = (*Store)(nil)

// Passkeys implements [passkey.Store].
func (s *Store) Passkeys(ctx context.Context, userID uuid.UUID) ([]passkey.Passkey, error) {
	rows, err := s.queries.ListPasskeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]passkey.Passkey, 0, len(rows))
	for _, r := range rows {
		p := passkey.Passkey{ID: r.ID, Name: r.Name, CreatedAt: r.CreatedAt.UTC()}
		if err := json.Unmarshal(r.Credential, &p.Credential); err != nil {
			return nil, err
		}
		if r.LastUsedAt != nil {
			p.LastUsedAt = r.LastUsedAt.UTC()
		}
		out = append(out, p)
	}
	return out, nil
}

// AddPasskey implements [passkey.Store].
func (s *Store) AddPasskey(ctx context.Context, userID uuid.UUID, p passkey.Passkey) error {
	credential, _ := json.Marshal(p.Credential)
	return s.queries.InsertPasskey(ctx, db.InsertPasskeyParams{
		ID: p.ID, UserID: userID, Name: p.Name, CredentialID: p.Credential.ID, Credential: credential, CreatedAt: p.CreatedAt,
	})
}

// DeletePasskey implements [passkey.Store].
func (s *Store) DeletePasskey(ctx context.Context, userID, id uuid.UUID) error {
	n, err := s.queries.DeletePasskey(ctx, db.DeletePasskeyParams{UserID: userID, ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return passkey.ErrNotFound
	}
	return nil
}

// TouchPasskey implements [passkey.Store].
func (s *Store) TouchPasskey(ctx context.Context, userID, id uuid.UUID, credential webauthn.Credential, at time.Time) error {
	raw, _ := json.Marshal(credential)
	n, err := s.queries.TouchPasskey(ctx, db.TouchPasskeyParams{UserID: userID, ID: id, Credential: raw, LastUsedAt: &at})
	if err != nil {
		return err
	}
	if n == 0 {
		return passkey.ErrNotFound
	}
	return nil
}
