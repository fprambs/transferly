package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	"github.com/fprambs/sentinel/internal/core/activity"
)

var _ activity.Store = (*Store)(nil)

// InsertActivity implements [activity.Store].
func (s *Store) InsertActivity(ctx context.Context, e activity.Entry) error {
	return s.queries.InsertActivity(ctx, db.InsertActivityParams{
		ID: e.ID, WorkspaceID: e.WorkspaceID, UserID: e.UserID, SiteID: uuid.NullUUID{UUID: e.SiteID, Valid: e.SiteID != uuid.Nil},
		SiteName: e.SiteName, Kind: string(e.Kind), Path: e.Path, Detail: e.Detail, ErrorCode: e.ErrorCode, CreatedAt: e.At,
	})
}

// ActivityByUser implements [activity.Store].
func (s *Store) ActivityByUser(ctx context.Context, userID, workspaceID, siteID uuid.UUID, limit int) ([]activity.Entry, error) {
	rows, err := s.queries.ActivityByUserWorkspace(ctx, db.ActivityByUserWorkspaceParams{
		UserID: userID, WorkspaceID: workspaceID, SiteID: uuid.NullUUID{UUID: siteID, Valid: siteID != uuid.Nil}, Limit: int32(limit), //nolint:gosec // limited by the service
	})
	if err != nil {
		return nil, err
	}
	out := make([]activity.Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, activity.Entry{
			ID: r.ID, UserID: r.UserID, WorkspaceID: r.WorkspaceID, SiteID: r.SiteID.UUID, SiteName: r.SiteName, Kind: activity.Kind(r.Kind),
			Path: r.Path, Detail: r.Detail, ErrorCode: r.ErrorCode, At: r.CreatedAt.UTC(),
		})
	}
	return out, nil
}

// PurgeActivity implements [activity.Store].
func (s *Store) PurgeActivity(ctx context.Context, before time.Time) (int, error) {
	n, err := s.queries.PurgeActivity(ctx, before)
	return int(n), err
}
