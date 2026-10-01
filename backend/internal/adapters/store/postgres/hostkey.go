package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres/db"
	"github.com/fprambs/transferly/internal/core/hostkey"
)

var _ hostkey.Store = (*Store)(nil)

// hostKeyFrom converts a stored row to a trusted key; a deleted accepting user reads as the nil identifier.
func hostKeyFrom(r db.KnownHostsRow) hostkey.Key {
	return hostkey.Key{
		Host: r.Host, Port: int(r.Port), Type: r.KeyType, Fingerprint: r.FingerprintSha256,
		FirstSeen: r.FirstSeen.UTC(), AcceptedBy: r.AcceptedBy.UUID,
	}
}

// Trusted implements [hostkey.Store].
func (s *Store) Trusted(ctx context.Context, workspaceID uuid.UUID, host string, port int) ([]hostkey.Key, error) {
	rows, err := s.queries.KnownHostsFor(ctx, db.KnownHostsForParams{WorkspaceID: workspaceID, Host: host, Port: int32(port)})
	if err != nil {
		return nil, err
	}
	out := make([]hostkey.Key, 0, len(rows))
	for _, r := range rows {
		out = append(out, hostKeyFrom(db.KnownHostsRow(r)))
	}
	return out, nil
}

// List implements [hostkey.Store].
func (s *Store) List(ctx context.Context, workspaceID uuid.UUID) ([]hostkey.Key, error) {
	rows, err := s.queries.KnownHosts(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]hostkey.Key, 0, len(rows))
	for _, r := range rows {
		out = append(out, hostKeyFrom(r))
	}
	return out, nil
}

// Add implements [hostkey.Store].
func (s *Store) Add(ctx context.Context, workspaceID uuid.UUID, k hostkey.Key) error {
	return s.queries.InsertKnownHost(ctx, db.InsertKnownHostParams{
		WorkspaceID: workspaceID, Host: k.Host, Port: int32(k.Port), KeyType: k.Type, FingerprintSha256: k.Fingerprint,
		FirstSeen: k.FirstSeen, AcceptedBy: uuid.NullUUID{UUID: k.AcceptedBy, Valid: k.AcceptedBy != uuid.Nil},
	})
}

// Delete implements [hostkey.Store].
func (s *Store) Delete(ctx context.Context, workspaceID uuid.UUID, host string, port int, keyType, fingerprint string) error {
	rows, err := s.queries.DeleteKnownHost(ctx, db.DeleteKnownHostParams{WorkspaceID: workspaceID, Host: host, Port: int32(port), KeyType: keyType, Fingerprint: fingerprint})
	if err != nil {
		return err
	}
	if rows == 0 {
		return hostkey.ErrNotFound
	}
	return nil
}
