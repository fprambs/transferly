package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	"github.com/fprambs/sentinel/internal/core/limits"
)

var _ limits.Store = (*Store)(nil)

// Limits implements [limits.Store].
func (s *Store) Limits(ctx context.Context) (limits.Limits, bool, error) {
	raw, err := s.queries.GetInstanceLimits(ctx)
	if errors.Is(err, errNoRows) {
		return limits.Limits{}, false, nil
	}
	if err != nil {
		return limits.Limits{}, false, err
	}
	var stored limits.Limits
	if err := json.Unmarshal(raw, &stored); err != nil {
		return limits.Limits{}, false, err
	}
	return stored, true, nil
}

// SaveLimits implements [limits.Store].
func (s *Store) SaveLimits(ctx context.Context, l limits.Limits, by uuid.UUID, at time.Time) error {
	raw, _ := json.Marshal(l)
	return s.queries.UpsertInstanceLimits(ctx, db.UpsertInstanceLimitsParams{Data: raw, UpdatedBy: uuid.NullUUID{UUID: by, Valid: by != uuid.Nil}, UpdatedAt: at})
}

// CountSites implements [limits.Store].
func (s *Store) CountSites(ctx context.Context, workspaceID uuid.UUID) (int, error) {
	n, err := s.queries.CountWorkspaceSites(ctx, workspaceID)
	return int(n), err
}

// CountOwnedTeams implements [limits.Store].
func (s *Store) CountOwnedTeams(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := s.queries.CountOwnedTeams(ctx, userID)
	return int(n), err
}

// CountMembers implements [limits.Store].
func (s *Store) CountMembers(ctx context.Context, workspaceID uuid.UUID, now time.Time) (int, error) {
	n, err := s.queries.CountMembersAndInvitations(ctx, db.CountMembersAndInvitationsParams{WorkspaceID: workspaceID, Now: now})
	return int(n), err
}

// BytesSince implements [limits.Store].
func (s *Store) BytesSince(ctx context.Context, userID uuid.UUID, since time.Time) (int64, error) {
	return s.queries.TransferBytesSince(ctx, db.TransferBytesSinceParams{UserID: userID, UpdatedAt: since})
}
