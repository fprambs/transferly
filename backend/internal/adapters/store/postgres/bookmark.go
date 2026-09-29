package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	"github.com/fprambs/sentinel/internal/core/bookmark"
)

var _ bookmark.Store = (*Store)(nil)

// bookmarkWrite maps a unique violation to [bookmark.ErrNameTaken] and leaves other errors untouched.
func bookmarkWrite(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return bookmark.ErrNameTaken
	}
	return err
}

// bookmarkFrom converts a stored row to a bookmark; an author whose account was removed is the nil identifier.
func bookmarkFrom(r db.SiteBookmark) bookmark.Bookmark {
	return bookmark.Bookmark{
		ID: r.ID, WorkspaceID: r.WorkspaceID, SiteID: r.SiteID, Name: r.Name, Path: r.Path, CreatedBy: r.CreatedBy.UUID, CreatedAt: r.CreatedAt.UTC(),
	}
}

// CreateBookmark implements [bookmark.Store].
func (s *Store) CreateBookmark(ctx context.Context, b bookmark.Bookmark) error {
	return bookmarkWrite(s.queries.InsertBookmark(ctx, db.InsertBookmarkParams{
		ID: b.ID, WorkspaceID: b.WorkspaceID, SiteID: b.SiteID, Name: b.Name, Path: b.Path, CreatedBy: uuid.NullUUID{UUID: b.CreatedBy, Valid: b.CreatedBy != uuid.Nil}, CreatedAt: b.CreatedAt,
	}))
}

// UpdateBookmark implements [bookmark.Store].
func (s *Store) UpdateBookmark(ctx context.Context, b bookmark.Bookmark) error {
	rows, err := s.queries.UpdateBookmark(ctx, db.UpdateBookmarkParams{ID: b.ID, WorkspaceID: b.WorkspaceID, SiteID: b.SiteID, Name: b.Name, Path: b.Path})
	if err != nil {
		return bookmarkWrite(err)
	}
	if rows == 0 {
		return bookmark.ErrNotFound
	}
	return nil
}

// Bookmark implements [bookmark.Store].
func (s *Store) Bookmark(ctx context.Context, workspaceID, siteID, id uuid.UUID) (bookmark.Bookmark, error) {
	r, err := s.queries.BookmarkByID(ctx, db.BookmarkByIDParams{ID: id, WorkspaceID: workspaceID, SiteID: siteID})
	if errors.Is(err, errNoRows) {
		return bookmark.Bookmark{}, bookmark.ErrNotFound
	}
	if err != nil {
		return bookmark.Bookmark{}, err
	}
	return bookmarkFrom(r), nil
}

// Bookmarks implements [bookmark.Store].
func (s *Store) Bookmarks(ctx context.Context, workspaceID, siteID uuid.UUID) ([]bookmark.Bookmark, error) {
	rows, err := s.queries.BookmarksBySite(ctx, db.BookmarksBySiteParams{WorkspaceID: workspaceID, SiteID: siteID})
	if err != nil {
		return nil, err
	}
	out := make([]bookmark.Bookmark, 0, len(rows))
	for _, r := range rows {
		out = append(out, bookmarkFrom(r))
	}
	return out, nil
}

// DeleteBookmark implements [bookmark.Store].
func (s *Store) DeleteBookmark(ctx context.Context, workspaceID, siteID, id uuid.UUID) error {
	rows, err := s.queries.DeleteBookmark(ctx, db.DeleteBookmarkParams{ID: id, WorkspaceID: workspaceID, SiteID: siteID})
	if err != nil {
		return err
	}
	if rows == 0 {
		return bookmark.ErrNotFound
	}
	return nil
}
