// Package activity keeps a log of what users did on their connections: folders created, entries renamed and deleted, checksums
// computed and transfers that ended. Entries name paths and outcomes, never credentials, file contents or anything the vault
// holds, and each entry belongs to the user who acted.
package activity

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/ports"
)

// Kind says what was done.
type Kind string

const (
	// Mkdir created a folder.
	Mkdir Kind = "mkdir"
	// Rename moved or renamed an entry; the detail holds the new path.
	Rename Kind = "rename"
	// Delete removed an entry.
	Delete Kind = "delete"
	// Checksum read a file to compute its digest; the detail holds the algorithm.
	Checksum Kind = "checksum"
	// Transfer is a transfer that ended; the detail holds its direction and final state.
	Transfer Kind = "transfer"
	// Compress packed entries into an archive on the server (ADR-0027); the detail holds the archive.
	Compress Kind = "compress"
	// Extract unpacked an archive on the server (ADR-0027); the detail holds the folder it went into.
	Extract Kind = "extract"
)

const (
	// defaultLimit is how many entries a list returns when the caller does not say.
	defaultLimit = 100
	// maxLimit is the most entries a list returns.
	maxLimit = 500
)

// Entry is one line of the log.
type Entry struct {
	// ID identifies the entry.
	ID uuid.UUID
	// UserID is the user who acted.
	UserID uuid.UUID
	// WorkspaceID is the workspace of the connection.
	WorkspaceID uuid.UUID
	// SiteID is the saved site; it is the zero value for a quick connection and once the site was deleted.
	SiteID uuid.UUID
	// SiteName is the name of the connection when it happened.
	SiteName string
	// Kind says what was done.
	Kind Kind
	// Path is the remote path the action was about.
	Path string
	// Detail adds what the kind needs, such as the new path of a rename.
	Detail string
	// ErrorCode is the stable code of the failure, or empty when the action worked.
	ErrorCode string
	// At is when it happened, in UTC.
	At time.Time
}

// Store persists the log.
type Store interface {
	// InsertActivity stores an entry.
	InsertActivity(ctx context.Context, e Entry) error
	// ActivityByUser returns the entries of a user in a workspace, newest first, optionally of one site.
	ActivityByUser(ctx context.Context, userID, workspaceID, siteID uuid.UUID, limit int) ([]Entry, error)
	// PurgeActivity removes the entries older than a time and returns how many.
	PurgeActivity(ctx context.Context, before time.Time) (int, error)
}

// Service records and lists the log.
type Service struct {
	store     Store
	clock     ports.Clock
	newID     func() (uuid.UUID, error)
	retention time.Duration
}

// NewService returns a service that keeps entries for the retention time; newID defaults to time ordered identifiers.
func NewService(store Store, clock ports.Clock, retention time.Duration, newID func() (uuid.UUID, error)) *Service {
	if newID == nil {
		newID = uuid.NewV7
	}
	return &Service{store: store, clock: clock, newID: newID, retention: retention}
}

// Record adds an entry stamped with the current time. Failing to log is returned but never worth failing what was logged.
func (s *Service) Record(ctx context.Context, e Entry) error {
	id, err := s.newID()
	if err != nil {
		return err
	}
	e.ID, e.At = id, s.clock.Now().UTC()
	return s.store.InsertActivity(ctx, e)
}

// List returns the newest entries of a user in a workspace, at most limit (a default when not positive, never more than the maximum),
// optionally only those of one connection.
func (s *Service) List(ctx context.Context, userID, workspaceID, siteID uuid.UUID, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)
	return s.store.ActivityByUser(ctx, userID, workspaceID, siteID, limit)
}

// PurgeExpired removes the entries older than the retention time and returns how many.
func (s *Service) PurgeExpired(ctx context.Context) (int, error) {
	return s.store.PurgeActivity(ctx, s.clock.Now().UTC().Add(-s.retention))
}
