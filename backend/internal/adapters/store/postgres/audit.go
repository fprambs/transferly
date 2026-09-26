package postgres

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	"github.com/fprambs/sentinel/internal/core/audit"
)

var _ audit.Store = (*Store)(nil)

// InsertAudit implements [audit.Store].
func (s *Store) InsertAudit(ctx context.Context, e audit.Event) error {
	meta := []byte("{}")
	if len(e.Meta) > 0 {
		meta, _ = json.Marshal(e.Meta)
	}
	return s.queries.InsertAudit(ctx, db.InsertAuditParams{
		WorkspaceID: uuid.NullUUID{UUID: e.WorkspaceID, Valid: e.WorkspaceID != uuid.Nil},
		UserID:      uuid.NullUUID{UUID: e.ActorID, Valid: e.ActorID != uuid.Nil},
		Action:      e.Action, Target: e.Target, Ip: e.IP, Meta: meta, At: e.At,
	})
}

// AuditByWorkspace implements [audit.Store].
func (s *Store) AuditByWorkspace(ctx context.Context, workspaceID uuid.UUID, filter audit.Filter, limit int) ([]audit.Event, error) {
	rows, err := s.queries.AuditByWorkspace(ctx, db.AuditByWorkspaceParams{
		WorkspaceID: uuid.NullUUID{UUID: workspaceID, Valid: true}, Prefix: filter.Action, Before: filter.Before, RowLimit: int32(limit), //nolint:gosec // limited by the service
	})
	if err != nil {
		return nil, err
	}
	out := make([]audit.Event, 0, len(rows))
	for _, r := range rows {
		meta := map[string]string{}
		if err := json.Unmarshal(r.Meta, &meta); err != nil {
			return nil, err
		}
		out = append(out, audit.Event{
			ID: r.ID, WorkspaceID: r.WorkspaceID.UUID, ActorID: r.UserID.UUID, ActorEmail: r.ActorEmail,
			Action: r.Action, Target: r.Target, IP: r.Ip, Meta: meta, At: r.At.UTC(),
		})
	}
	return out, nil
}
