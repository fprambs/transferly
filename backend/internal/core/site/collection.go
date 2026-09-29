package site

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/workspace"
)

// maxCollectionDepth is how many collections may nest, the top level being depth one.
const maxCollectionDepth = 8

// checkCollection verifies that a collection exists in the workspace; the nil identifier is the top level.
func (s *Service) checkCollection(ctx context.Context, workspaceID, id uuid.UUID) error {
	if id == uuid.Nil {
		return nil
	}
	_, err := s.store.Collection(ctx, workspaceID, id)
	return err
}

// cleanCollectionName trims and validates a collection name.
func cleanCollectionName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > maxNameRunes {
		return "", ErrInvalidName
	}
	return name, nil
}

// depthOf returns how deep a collection sits, counting itself, following parents through the given index.
// A chain longer than the index is a cycle already stored, which cannot happen through this service.
func depthOf(byID map[uuid.UUID]Collection, id uuid.UUID) int {
	depth := 0
	for id != uuid.Nil && depth <= len(byID) {
		depth++
		id = byID[id].ParentID
	}
	return depth
}

// height returns how many levels a collection and its descendants span, counting itself.
func height(children map[uuid.UUID][]uuid.UUID, id uuid.UUID) int {
	tallest := 0
	for _, child := range children[id] {
		tallest = max(tallest, height(children, child))
	}
	return tallest + 1
}

// CreateCollection creates a collection inside another one, or at the top level.
func (s *Service) CreateCollection(ctx context.Context, actor, workspaceID uuid.UUID, name string, parentID uuid.UUID) (Collection, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return Collection{}, err
	}
	name, err := cleanCollectionName(name)
	if err != nil {
		return Collection{}, err
	}
	all, err := s.store.Collections(ctx, workspaceID)
	if err != nil {
		return Collection{}, err
	}
	byID := index(all)
	if parentID != uuid.Nil {
		if _, ok := byID[parentID]; !ok {
			return Collection{}, ErrCollectionNotFound
		}
		if depthOf(byID, parentID)+1 > maxCollectionDepth {
			return Collection{}, ErrCollectionTooDeep
		}
	}
	id, err := s.newID()
	if err != nil {
		return Collection{}, fmt.Errorf("generating collection id: %w", err)
	}
	created := Collection{ID: id, WorkspaceID: workspaceID, ParentID: parentID, Name: name}
	if err := s.store.CreateCollection(ctx, created); err != nil {
		return Collection{}, err
	}
	return created, nil
}

// index maps collections by identifier.
func index(all []Collection) map[uuid.UUID]Collection {
	byID := make(map[uuid.UUID]Collection, len(all))
	for _, c := range all {
		byID[c.ID] = c
	}
	return byID
}

// ListCollections returns every collection of a workspace the actor may browse; clients build the tree from the parents.
func (s *Service) ListCollections(ctx context.Context, actor, workspaceID uuid.UUID) ([]Collection, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionBrowse); err != nil {
		return nil, err
	}
	return s.store.Collections(ctx, workspaceID)
}

// CollectionChanges says what to change in a collection. Nil and false leave a field as it is.
type CollectionChanges struct {
	// Name is the new name.
	Name *string
	// SetParent is true to move the collection to ParentID, the nil identifier meaning the top level.
	SetParent bool
	// ParentID is the new parent when SetParent is true.
	ParentID uuid.UUID
}

// UpdateCollection renames a collection and moves it, refusing moves into itself or its descendants and moves
// that would nest collections deeper than eight levels.
func (s *Service) UpdateCollection(ctx context.Context, actor, workspaceID, id uuid.UUID, changes CollectionChanges) (Collection, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return Collection{}, err
	}
	all, err := s.store.Collections(ctx, workspaceID)
	if err != nil {
		return Collection{}, err
	}
	byID := index(all)
	current, ok := byID[id]
	if !ok {
		return Collection{}, ErrCollectionNotFound
	}
	if changes.Name != nil {
		if current.Name, err = cleanCollectionName(*changes.Name); err != nil {
			return Collection{}, err
		}
	}
	if changes.SetParent {
		if err := checkMove(all, byID, id, changes.ParentID); err != nil {
			return Collection{}, err
		}
		current.ParentID = changes.ParentID
	}
	if err := s.store.UpdateCollection(ctx, current); err != nil {
		return Collection{}, err
	}
	return current, nil
}

// checkMove verifies that moving a collection under a parent keeps the tree valid.
func checkMove(all []Collection, byID map[uuid.UUID]Collection, id, parentID uuid.UUID) error {
	if parentID == uuid.Nil {
		return nil
	}
	if _, ok := byID[parentID]; !ok {
		return ErrCollectionNotFound
	}
	for ancestor := parentID; ancestor != uuid.Nil; ancestor = byID[ancestor].ParentID {
		if ancestor == id {
			return ErrCollectionCycle
		}
	}
	children := map[uuid.UUID][]uuid.UUID{}
	for _, c := range all {
		children[c.ParentID] = append(children[c.ParentID], c.ID)
	}
	if depthOf(byID, parentID)+height(children, id) > maxCollectionDepth {
		return ErrCollectionTooDeep
	}
	return nil
}

// DeleteCollection removes an empty collection.
func (s *Service) DeleteCollection(ctx context.Context, actor, workspaceID, id uuid.UUID) error {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return err
	}
	return s.store.DeleteCollection(ctx, workspaceID, id)
}

// MoveSite puts a saved site in a collection, or at the top level for the nil identifier, leaving every other setting as it is (4.14h).
// Unlike an update, it does not check the host again, so a site whose server cannot be reached right now can still be moved.
func (s *Service) MoveSite(ctx context.Context, actor, workspaceID, id, collectionID uuid.UUID) (Site, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return Site{}, err
	}
	if err := s.checkCollection(ctx, workspaceID, collectionID); err != nil {
		return Site{}, err
	}
	current, err := s.store.Site(ctx, workspaceID, id)
	if err != nil {
		return Site{}, err
	}
	current.CollectionID, current.UpdatedAt = collectionID, s.clock.Now().UTC()
	if err := s.store.UpdateSite(ctx, current, nil, nil); err != nil {
		return Site{}, err
	}
	return current, nil
}
