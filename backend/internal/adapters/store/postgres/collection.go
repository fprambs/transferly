package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fprambs/transferly/internal/adapters/store/postgres/db"
	"github.com/fprambs/transferly/internal/core/site"
)

var _ site.CollectionStore = (*Store)(nil)

// collectionWrite maps a unique violation to [site.ErrCollectionNameTaken] and leaves other errors untouched.
func collectionWrite(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return site.ErrCollectionNameTaken
	}
	return err
}

// collectionFrom converts a stored row to a collection.
func collectionFrom(r db.SiteFolder) site.Collection {
	return site.Collection{ID: r.ID, WorkspaceID: r.WorkspaceID, ParentID: r.ParentID.UUID, Name: r.Name}
}

// CreateCollection implements [site.CollectionStore].
func (s *Store) CreateCollection(ctx context.Context, c site.Collection) error {
	return collectionWrite(s.queries.InsertCollection(ctx, db.InsertCollectionParams{ID: c.ID, WorkspaceID: c.WorkspaceID, ParentID: folderID(c.ParentID), Name: c.Name}))
}

// UpdateCollection implements [site.CollectionStore].
func (s *Store) UpdateCollection(ctx context.Context, c site.Collection) error {
	rows, err := s.queries.UpdateCollection(ctx, db.UpdateCollectionParams{ID: c.ID, WorkspaceID: c.WorkspaceID, ParentID: folderID(c.ParentID), Name: c.Name})
	if err != nil {
		return collectionWrite(err)
	}
	if rows == 0 {
		return site.ErrCollectionNotFound
	}
	return nil
}

// Collection implements [site.CollectionStore].
func (s *Store) Collection(ctx context.Context, workspaceID, id uuid.UUID) (site.Collection, error) {
	r, err := s.queries.CollectionByID(ctx, db.CollectionByIDParams{ID: id, WorkspaceID: workspaceID})
	if errors.Is(err, errNoRows) {
		return site.Collection{}, site.ErrCollectionNotFound
	}
	if err != nil {
		return site.Collection{}, err
	}
	return collectionFrom(r), nil
}

// Collections implements [site.CollectionStore].
func (s *Store) Collections(ctx context.Context, workspaceID uuid.UUID) ([]site.Collection, error) {
	rows, err := s.queries.CollectionsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]site.Collection, 0, len(rows))
	for _, r := range rows {
		out = append(out, collectionFrom(r))
	}
	return out, nil
}

// DeleteCollection implements [site.CollectionStore].
func (s *Store) DeleteCollection(ctx context.Context, workspaceID, id uuid.UUID) error {
	if _, err := s.Collection(ctx, workspaceID, id); err != nil {
		return err
	}
	used, err := s.queries.CollectionHasContents(ctx, db.CollectionHasContentsParams{ParentID: folderID(id), WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	if used {
		return site.ErrCollectionNotEmpty
	}
	rows, err := s.queries.DeleteCollection(ctx, db.DeleteCollectionParams{ID: id, WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	if rows == 0 {
		return site.ErrCollectionNotFound
	}
	return nil
}
