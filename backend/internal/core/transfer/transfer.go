// Package transfer records the files users upload and download: a persistent history with progress, state
// and the information needed to resume, plus the events that tell open clients about changes.
package transfer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/ports"
)

// Direction says which way the data flows.
type Direction string

const (
	// Upload sends a file to a remote server.
	Upload Direction = "upload"
	// Download brings a file from a remote server.
	Download Direction = "download"
)

// Status is the state of a transfer.
type Status string

const (
	// Queued means the transfer waits for a connection to its site.
	Queued Status = "queued"
	// Running means data is moving.
	Running Status = "running"
	// Done means the whole file was moved.
	Done Status = "done"
	// Failed means the transfer stopped with an error.
	Failed Status = "failed"
	// Canceled means the user stopped it.
	Canceled Status = "canceled"
	// Interrupted means it stopped without an error of its own: the client went away or the server restarted.
	// An interrupted upload can be resumed from the bytes already written.
	Interrupted Status = "interrupted"
)

// Active reports whether the transfer is still in progress.
func (s Status) Active() bool { return s == Queued || s == Running }

// Errors returned by the package.
var (
	// ErrNotFound means the transfer does not exist or belongs to another user.
	ErrNotFound = errors.New("transfer not found")
	// ErrActive means the operation needs a finished transfer.
	ErrActive = errors.New("transfer is still active")
	// ErrNotActive means the operation needs a transfer in progress.
	ErrNotActive = errors.New("transfer is not active")
	// ErrExists means a transfer with that identifier already exists.
	ErrExists = errors.New("transfer already exists")
)

// Transfer is the record of one file transfer.
type Transfer struct {
	// ID identifies the transfer; clients may choose it to follow their own transfers.
	ID uuid.UUID
	// WorkspaceID is the workspace of the site.
	WorkspaceID uuid.UUID
	// UserID is the user who owns the transfer; nobody else sees it.
	UserID uuid.UUID
	// SiteID is the site, or the nil identifier once the site was deleted.
	SiteID uuid.UUID
	// SiteName is the name of the site when the transfer started, kept for the history.
	SiteName string
	// Direction is upload or download.
	Direction Direction
	// Path is the remote path.
	Path string
	// Name is the file name.
	Name string
	// Status is the state.
	Status Status
	// TotalBytes is the size of the file, or nil while it is unknown.
	TotalBytes *int64
	// BytesDone is how many bytes moved so far; for an upload, where a resume continues.
	BytesDone int64
	// ErrorCode is the stable code of the error that stopped the transfer, if any.
	ErrorCode string
	// Attempts counts how many times the transfer was started, resumes included.
	Attempts int
	// CreatedAt is when the transfer was requested, in UTC.
	CreatedAt time.Time
	// StartedAt is when data started moving, or nil.
	StartedAt *time.Time
	// UpdatedAt is when the record last changed, in UTC.
	UpdatedAt time.Time
	// FinishedAt is when the transfer ended, or nil while it is active.
	FinishedAt *time.Time
}

// Store persists transfers. Every lookup is scoped by user.
type Store interface {
	// CreateTransfer stores a new transfer, or returns [ErrExists].
	CreateTransfer(ctx context.Context, t Transfer) error
	// UpdateTransfer replaces the mutable fields of a transfer of the user, or returns [ErrNotFound].
	UpdateTransfer(ctx context.Context, t Transfer) error
	// TransferByID returns a transfer of the user, or [ErrNotFound].
	TransferByID(ctx context.Context, userID, id uuid.UUID) (Transfer, error)
	// ListTransfers returns the newest transfers of the user, at most limit; a workspace other than the nil identifier filters.
	ListTransfers(ctx context.Context, userID, workspaceID uuid.UUID, limit int) ([]Transfer, error)
	// DeleteTransfer removes a transfer of the user, or returns [ErrNotFound].
	DeleteTransfer(ctx context.Context, userID, id uuid.UUID) error
	// InterruptActive marks every transfer in progress as interrupted by a server restart and returns how many.
	InterruptActive(ctx context.Context, at time.Time) (int, error)
}

// Config tunes the service. Zero values select the defaults.
type Config struct {
	// EventEvery is the least time between progress events of one transfer; 250 ms by default.
	EventEvery time.Duration
	// SaveEvery is the least time between progress writes to the store; 2 seconds by default.
	SaveEvery time.Duration
	// MaxList caps the number of transfers a listing returns; 200 by default.
	MaxList int
}

// withDefaults fills in the zero values.
func (c Config) withDefaults() Config {
	if c.EventEvery <= 0 {
		c.EventEvery = 250 * time.Millisecond
	}
	if c.SaveEvery <= 0 {
		c.SaveEvery = 2 * time.Second
	}
	if c.MaxList <= 0 {
		c.MaxList = 200
	}
	return c
}

// Service tracks transfers.
type Service struct {
	store Store
	sink  ports.EventSink
	clock ports.Clock
	cfg   Config
	newID func() (uuid.UUID, error)

	mu     sync.Mutex
	active map[uuid.UUID]*Tracker
}

// NewService returns a service. newID may be nil to use UUIDv7.
func NewService(store Store, sink ports.EventSink, clock ports.Clock, cfg Config, newID func() (uuid.UUID, error)) *Service {
	if newID == nil {
		newID = uuid.NewV7
	}
	return &Service{store: store, sink: sink, clock: clock, cfg: cfg.withDefaults(), newID: newID, active: map[uuid.UUID]*Tracker{}}
}

// Recover marks the transfers that were in progress when the server stopped as interrupted. It runs once at startup.
func (s *Service) Recover(ctx context.Context) (int, error) {
	return s.store.InterruptActive(ctx, s.clock.Now().UTC())
}

// List returns the user's newest transfers, at most the configured maximum.
func (s *Service) List(ctx context.Context, userID, workspaceID uuid.UUID, limit int) ([]Transfer, error) {
	if limit <= 0 || limit > s.cfg.MaxList {
		limit = s.cfg.MaxList
	}
	return s.store.ListTransfers(ctx, userID, workspaceID, limit)
}

// Get returns one of the user's transfers.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (Transfer, error) {
	return s.store.TransferByID(ctx, userID, id)
}

// Delete removes a finished transfer from the history.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	t, err := s.store.TransferByID(ctx, userID, id)
	if err != nil {
		return err
	}
	if t.Status.Active() {
		return ErrActive
	}
	return s.store.DeleteTransfer(ctx, userID, id)
}

// Cancel stops a transfer in progress. Its data stops moving and it ends as canceled.
func (s *Service) Cancel(ctx context.Context, userID, id uuid.UUID) error {
	t, err := s.store.TransferByID(ctx, userID, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	tracker, running := s.active[id]
	s.mu.Unlock()
	if !t.Status.Active() || !running {
		return ErrNotActive
	}
	tracker.cancel(errCanceledByUser)
	return nil
}
