package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres/db"
	"github.com/fprambs/transferly/internal/core/connectors"
)

var _ connectors.Store = (*Store)(nil)

// ConnectorSettings implements [connectors.Store].
func (s *Store) ConnectorSettings(ctx context.Context) (map[string]bool, error) {
	rows, err := s.queries.ListConnectorSettings(ctx)
	if err != nil {
		return nil, err
	}
	settings := make(map[string]bool, len(rows))
	for _, row := range rows {
		settings[row.Type] = row.Enabled
	}
	return settings, nil
}

// SetConnector implements [connectors.Store].
func (s *Store) SetConnector(ctx context.Context, kind string, enabled bool, by uuid.UUID, at time.Time) error {
	return s.queries.UpsertConnectorSetting(ctx, db.UpsertConnectorSettingParams{Type: kind, Enabled: enabled, UpdatedBy: uuid.NullUUID{UUID: by, Valid: by != uuid.Nil}, UpdatedAt: at})
}
