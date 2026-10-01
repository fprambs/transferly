package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/site"
)

// collectionDTO converts a collection to its public representation.
func collectionDTO(c site.Collection) Collection {
	out := Collection{Id: c.ID, Name: c.Name}
	if c.ParentID != uuid.Nil {
		out.ParentId = &c.ParentID
	}
	return out
}

// optionalID returns the identifier a client sent, or the nil identifier when it left the field out.
func optionalID(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

// ListCollections lists the collections of a workspace.
func (s *server) ListCollections(ctx context.Context, request ListCollectionsRequestObject) (ListCollectionsResponseObject, error) {
	p, _ := principalFrom(ctx)
	list, err := s.deps.Sites.ListCollections(ctx, p.User.ID, request.WorkspaceId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(ListCollections200JSONResponse, 0, len(list))
	for _, c := range list {
		out = append(out, collectionDTO(c))
	}
	return out, nil
}

// CreateCollection creates a collection.
func (s *server) CreateCollection(ctx context.Context, request CreateCollectionRequestObject) (CreateCollectionResponseObject, error) {
	p, _ := principalFrom(ctx)
	created, err := s.deps.Sites.CreateCollection(ctx, p.User.ID, request.WorkspaceId, request.Body.Name, optionalID(request.Body.ParentId))
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return CreateCollection201JSONResponse(collectionDTO(created)), nil
}

// UpdateCollection renames or moves a collection.
func (s *server) UpdateCollection(ctx context.Context, request UpdateCollectionRequestObject) (UpdateCollectionResponseObject, error) {
	p, _ := principalFrom(ctx)
	b := request.Body
	changes := site.CollectionChanges{Name: b.Name}
	switch {
	case b.MoveToTop != nil && *b.MoveToTop:
		changes.SetParent = true
	case b.ParentId != nil:
		changes.SetParent, changes.ParentID = true, *b.ParentId
	}
	updated, err := s.deps.Sites.UpdateCollection(ctx, p.User.ID, request.WorkspaceId, request.CollectionId, changes)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return UpdateCollection200JSONResponse(collectionDTO(updated)), nil
}

// DeleteCollection removes an empty collection.
func (s *server) DeleteCollection(ctx context.Context, request DeleteCollectionRequestObject) (DeleteCollectionResponseObject, error) {
	p, _ := principalFrom(ctx)
	err := s.deps.Sites.DeleteCollection(ctx, p.User.ID, request.WorkspaceId, request.CollectionId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return DeleteCollection204Response{}, nil
}

// MoveSite puts a saved site in another collection, or at the top level.
func (s *server) MoveSite(ctx context.Context, request MoveSiteRequestObject) (MoveSiteResponseObject, error) {
	p, _ := principalFrom(ctx)
	target := uuid.Nil
	if request.Body.CollectionId != nil {
		target = *request.Body.CollectionId
	}
	moved, err := s.deps.Sites.MoveSite(ctx, p.User.ID, request.WorkspaceId, request.SiteId, target)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.SiteUpdated, moved.Name, map[string]string{"host": moved.Host})
	return MoveSite200JSONResponse(siteDTO(moved)), nil
}

// VisitMoveSiteResponse implements [MoveSiteResponseObject].
func (p domainProblem) VisitMoveSiteResponse(w http.ResponseWriter) error { return p.write(w) }
