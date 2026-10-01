// Package bookmark keeps the folders a workspace marked on a saved site, as FileZilla's site bookmarks do (4.14b): a name and a remote
// path, opened from the explorer or the command palette.
package bookmark

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/workspace"
)

const (
	// maxNameRunes is the longest name, as for sites and collections.
	maxNameRunes = 100
	// maxPathBytes is the longest remote path.
	maxPathBytes = 4096
	// MaxPerSite is how many bookmarks a site may have.
	MaxPerSite = 200
)

// Errors returned by the package.
var (
	// ErrNotFound means the site has no such bookmark.
	ErrNotFound = errors.New("bookmark not found")
	// ErrNameTaken means the site has another bookmark with the name, regardless of case.
	ErrNameTaken = errors.New("bookmark name already in use")
	// ErrInvalidName means the name is empty, too long or holds control characters.
	ErrInvalidName = errors.New("invalid bookmark name")
	// ErrInvalidPath means the path is not absolute, is too long or holds control characters.
	ErrInvalidPath = errors.New("invalid bookmark path")
	// ErrLimit means the site already has [MaxPerSite] bookmarks.
	ErrLimit = errors.New("too many bookmarks on this site")
)

// Bookmark is a folder of a saved site, kept under a name.
type Bookmark struct {
	// ID identifies the bookmark.
	ID uuid.UUID
	// WorkspaceID is the workspace that owns the site.
	WorkspaceID uuid.UUID
	// SiteID is the saved site the folder belongs to.
	SiteID uuid.UUID
	// Name is the display name, unique among the bookmarks of the site regardless of case.
	Name string
	// Path is the absolute remote path of the folder, cleaned.
	Path string
	// CreatedBy is the user who added it.
	CreatedBy uuid.UUID
	// CreatedAt is when it was added, in UTC.
	CreatedAt time.Time
}

// Store persists bookmarks. Every lookup is scoped by workspace and site.
type Store interface {
	// CreateBookmark stores a bookmark, or returns [ErrNameTaken].
	CreateBookmark(ctx context.Context, b Bookmark) error
	// UpdateBookmark replaces the name and path of a bookmark, or returns [ErrNotFound] or [ErrNameTaken].
	UpdateBookmark(ctx context.Context, b Bookmark) error
	// Bookmark returns one bookmark, or [ErrNotFound].
	Bookmark(ctx context.Context, workspaceID, siteID, id uuid.UUID) (Bookmark, error)
	// Bookmarks lists the bookmarks of a site by name.
	Bookmarks(ctx context.Context, workspaceID, siteID uuid.UUID) ([]Bookmark, error)
	// DeleteBookmark removes a bookmark, or returns [ErrNotFound].
	DeleteBookmark(ctx context.Context, workspaceID, siteID, id uuid.UUID) error
}

// Sites finds the saved sites bookmarks belong to.
type Sites interface {
	// Site returns a site of the workspace, or [site.ErrNotFound].
	Site(ctx context.Context, workspaceID, id uuid.UUID) (site.Site, error)
}

// Authorizer checks that a user may perform an action in a workspace.
type Authorizer interface {
	// Authorize returns the role of the user or an error that says why the action is refused.
	Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action workspace.Action) (workspace.Role, error)
}

// Clock tells the current time.
type Clock interface {
	// Now returns the current instant.
	Now() time.Time
}

// Service manages bookmarks. Whoever may browse a workspace sees its bookmarks; whoever may manage its sites changes them.
type Service struct {
	store Store
	sites Sites
	authz Authorizer
	clock Clock
	newID func() (uuid.UUID, error)
}

// NewService returns a service. newID may be nil to use UUIDv7.
func NewService(store Store, sites Sites, authz Authorizer, clock Clock, newID func() (uuid.UUID, error)) *Service {
	if newID == nil {
		newID = uuid.NewV7
	}
	return &Service{store: store, sites: sites, authz: authz, clock: clock, newID: newID}
}

// Changes says what to change in a bookmark. Nil leaves a field as it is.
type Changes struct {
	// Name is the new name.
	Name *string
	// Path is the new folder.
	Path *string
}

// hasControl reports whether a text holds a control character, which would hide what a name or path really says.
func hasControl(s string) bool {
	return strings.ContainsFunc(s, unicode.IsControl)
}

// cleanName trims and validates a name.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > maxNameRunes || hasControl(name) {
		return "", ErrInvalidName
	}
	return name, nil
}

// cleanPath validates an absolute remote path and cleans it, so /a/b/ and /a/./b are the same folder.
func cleanPath(p string) (string, error) {
	if !strings.HasPrefix(p, "/") || len(p) > maxPathBytes || !utf8.ValidString(p) || hasControl(p) {
		return "", ErrInvalidPath
	}
	return path.Clean(p), nil
}

// allow checks that the actor may perform the action and that the site is a saved site of the workspace.
func (s *Service) allow(ctx context.Context, actor, workspaceID, siteID uuid.UUID, action workspace.Action) error {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, action); err != nil {
		return err
	}
	_, err := s.sites.Site(ctx, workspaceID, siteID)
	return err
}

// List returns the bookmarks of a site by name.
func (s *Service) List(ctx context.Context, actor, workspaceID, siteID uuid.UUID) ([]Bookmark, error) {
	if err := s.allow(ctx, actor, workspaceID, siteID, workspace.ActionBrowse); err != nil {
		return nil, err
	}
	return s.store.Bookmarks(ctx, workspaceID, siteID)
}

// Create adds a bookmark to a site.
func (s *Service) Create(ctx context.Context, actor, workspaceID, siteID uuid.UUID, name, folder string) (Bookmark, error) {
	if err := s.allow(ctx, actor, workspaceID, siteID, workspace.ActionManageSites); err != nil {
		return Bookmark{}, err
	}
	name, err := cleanName(name)
	if err != nil {
		return Bookmark{}, err
	}
	if folder, err = cleanPath(folder); err != nil {
		return Bookmark{}, err
	}
	existing, err := s.store.Bookmarks(ctx, workspaceID, siteID)
	if err != nil {
		return Bookmark{}, err
	}
	if len(existing) >= MaxPerSite {
		return Bookmark{}, ErrLimit
	}
	id, err := s.newID()
	if err != nil {
		return Bookmark{}, fmt.Errorf("generating bookmark id: %w", err)
	}
	created := Bookmark{ID: id, WorkspaceID: workspaceID, SiteID: siteID, Name: name, Path: folder, CreatedBy: actor, CreatedAt: s.clock.Now().UTC()}
	if err := s.store.CreateBookmark(ctx, created); err != nil {
		return Bookmark{}, err
	}
	return created, nil
}

// Update renames a bookmark or points it at another folder.
func (s *Service) Update(ctx context.Context, actor, workspaceID, siteID, id uuid.UUID, changes Changes) (Bookmark, error) {
	if err := s.allow(ctx, actor, workspaceID, siteID, workspace.ActionManageSites); err != nil {
		return Bookmark{}, err
	}
	current, err := s.store.Bookmark(ctx, workspaceID, siteID, id)
	if err != nil {
		return Bookmark{}, err
	}
	if changes.Name != nil {
		if current.Name, err = cleanName(*changes.Name); err != nil {
			return Bookmark{}, err
		}
	}
	if changes.Path != nil {
		if current.Path, err = cleanPath(*changes.Path); err != nil {
			return Bookmark{}, err
		}
	}
	if err := s.store.UpdateBookmark(ctx, current); err != nil {
		return Bookmark{}, err
	}
	return current, nil
}

// Delete removes a bookmark and returns what it was, for the audit trail.
func (s *Service) Delete(ctx context.Context, actor, workspaceID, siteID, id uuid.UUID) (Bookmark, error) {
	if err := s.allow(ctx, actor, workspaceID, siteID, workspace.ActionManageSites); err != nil {
		return Bookmark{}, err
	}
	current, err := s.store.Bookmark(ctx, workspaceID, siteID, id)
	if err != nil {
		return Bookmark{}, err
	}
	if err := s.store.DeleteBookmark(ctx, workspaceID, siteID, id); err != nil {
		return Bookmark{}, err
	}
	return current, nil
}
