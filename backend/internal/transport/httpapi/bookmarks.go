package httpapi

import (
	"context"

	"github.com/fprambs/sentinel/internal/core/audit"
	"github.com/fprambs/sentinel/internal/core/bookmark"
)

// bookmarkDTO converts a bookmark to its public representation.
func bookmarkDTO(b bookmark.Bookmark) Bookmark {
	return Bookmark{Id: b.ID, Name: b.Name, Path: b.Path, CreatedAt: b.CreatedAt}
}

// bookmarkMeta is what the audit trail keeps about a bookmark besides its name: the site and the folder.
func bookmarkMeta(b bookmark.Bookmark) map[string]string {
	return map[string]string{"site": b.SiteID.String(), "path": b.Path}
}

// ListBookmarks lists the bookmarked folders of a saved site.
func (s *server) ListBookmarks(ctx context.Context, request ListBookmarksRequestObject) (ListBookmarksResponseObject, error) {
	p, _ := principalFrom(ctx)
	list, err := s.deps.Bookmarks.List(ctx, p.User.ID, request.WorkspaceId, request.SiteId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(ListBookmarks200JSONResponse, 0, len(list))
	for _, b := range list {
		out = append(out, bookmarkDTO(b))
	}
	return out, nil
}

// CreateBookmark bookmarks a folder of a saved site.
func (s *server) CreateBookmark(ctx context.Context, request CreateBookmarkRequestObject) (CreateBookmarkResponseObject, error) {
	p, _ := principalFrom(ctx)
	created, err := s.deps.Bookmarks.Create(ctx, p.User.ID, request.WorkspaceId, request.SiteId, request.Body.Name, request.Body.Path)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.BookmarkCreated, created.Name, bookmarkMeta(created))
	return CreateBookmark201JSONResponse(bookmarkDTO(created)), nil
}

// UpdateBookmark renames a bookmark or points it at another folder.
func (s *server) UpdateBookmark(ctx context.Context, request UpdateBookmarkRequestObject) (UpdateBookmarkResponseObject, error) {
	p, _ := principalFrom(ctx)
	changes := bookmark.Changes{Name: request.Body.Name, Path: request.Body.Path}
	updated, err := s.deps.Bookmarks.Update(ctx, p.User.ID, request.WorkspaceId, request.SiteId, request.BookmarkId, changes)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.BookmarkUpdated, updated.Name, bookmarkMeta(updated))
	return UpdateBookmark200JSONResponse(bookmarkDTO(updated)), nil
}

// DeleteBookmark removes a bookmark.
func (s *server) DeleteBookmark(ctx context.Context, request DeleteBookmarkRequestObject) (DeleteBookmarkResponseObject, error) {
	p, _ := principalFrom(ctx)
	removed, err := s.deps.Bookmarks.Delete(ctx, p.User.ID, request.WorkspaceId, request.SiteId, request.BookmarkId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.BookmarkDeleted, removed.Name, bookmarkMeta(removed))
	return DeleteBookmark204Response{}, nil
}
