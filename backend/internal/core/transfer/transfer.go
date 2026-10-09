// Package transfer records the files users upload and download: a persistent history with progress, state
// and the information needed to resume, plus the events that tell open clients about changes.
package transfer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/scheduler"
)

// Direction says which way the data flows.
type Direction string

const (
	// Upload sends a file to a remote server.
	Upload Direction = "upload"
	// Download brings a file from a remote server.
	Download Direction = "download"
	// Copy moves a file from one remote server to another without it reaching the browser.
	Copy Direction = "copy"
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
	// ErrNotQueued means the operation needs a transfer that waits for a free slot: it already runs, or it is over.
	ErrNotQueued = errors.New("transfer is not waiting in the queue")
	// ErrInvalidPriority means the priority is not one of low, normal and high.
	ErrInvalidPriority = errors.New("invalid priority")
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
	// Direction is upload, download or copy.
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
	// Paused is true while a waiting transfer is held back by its user. It lives in memory, not in the store.
	Paused bool
	// Priority is the place of a waiting transfer in the queue: -1 low, 0 normal, 1 high. It lives in memory, not in the store.
	Priority int
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
	// OnPersistenceError reports failed transfer writes without including credentials or remote response bodies.
	OnPersistenceError func(error)
	// EventEvery is the least time between progress events of one transfer; 250 ms by default.
	EventEvery time.Duration
	// SaveEvery is the least time between progress writes to the store; 2 seconds by default.
	SaveEvery time.Duration
	// MaxList caps the number of transfers a listing returns; 200 by default.
	MaxList int
	// Limits are the most transfers that run at once; the others wait in the queue. Zero means no limit.
	Limits scheduler.Limits
	// Sleep waits between the chunks of a transfer that goes faster than its bandwidth cap, and stops when the context ends; a real timer by
	// default. Tests replace it to watch the waits instead of living through them.
	Sleep func(ctx context.Context, d time.Duration) error
	// OnFinish, when set, is called with the record of every transfer that ends, whatever its outcome. It must be quick and must not
	// call back into the transfer service.
	OnFinish func(Transfer)
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
	if c.Sleep == nil {
		c.Sleep = sleep
	}
	return c
}

// Service tracks transfers.
type Service struct {
	store Store
	sink  ports.EventSink
	clock ports.Clock
	cfg   Config
	sched *scheduler.Scheduler
	newID func() (uuid.UUID, error)

	mu         sync.Mutex
	active     map[uuid.UUID]*Tracker
	quota      Quota
	global     *shaper
	perUser    map[uuid.UUID]*shaper
	perUserCap int64
	perRoute   map[uuid.UUID]*shaper
}

// Quota decides whether a person may start another transfer.
type Quota interface {
	// CheckTransfer returns an error when the person may not start one, such as [limits.ErrDailyQuota].
	CheckTransfer(ctx context.Context, userID uuid.UUID) error
}

// NewService returns a service. newID may be nil to use UUIDv7.
func NewService(store Store, sink ports.EventSink, clock ports.Clock, cfg Config, newID func() (uuid.UUID, error)) *Service {
	if newID == nil {
		newID = uuid.NewV7
	}
	return &Service{
		store: store, sink: sink, clock: clock, cfg: cfg.withDefaults(), sched: scheduler.New(cfg.Limits), newID: newID,
		active: map[uuid.UUID]*Tracker{}, global: &shaper{}, perUser: map[uuid.UUID]*shaper{}, perRoute: map[uuid.UUID]*shaper{},
	}
}

// UseQuota makes the service ask a quota before it starts a transfer. Without one nothing is refused.
func (s *Service) UseQuota(q Quota) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quota = q
}

// SetLimits changes how many transfers run at once; the ones that already run are not interrupted and the ones that now fit are admitted.
func (s *Service) SetLimits(limits scheduler.Limits) { s.sched.SetLimits(limits) }

// SetBandwidth changes the caps on the bytes per second: one for all transfers of the server and one for the transfers of each person.
// Zero removes a cap. It takes effect on the next chunk of every running transfer.
func (s *Service) SetBandwidth(global, perUser int64) {
	s.global.setRate(global)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.perUserCap = perUser
	for _, sh := range s.perUser {
		sh.setRate(perUser)
	}
}

// shaperOf returns the shaper of a person, made on first use with the cap in force.
func (s *Service) shaperOf(userID uuid.UUID) *shaper {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.perUser[userID]
	if !ok {
		sh = &shaper{rate: s.perUserCap}
		s.perUser[userID] = sh
	}
	return sh
}

// routeShaperOf returns the shaper of a route, made on first use, with its rate set to the cap in force for the transfer
// that is asking. A site's own bandwidth cap, unlike the global and per-user ones, has no separate setter: it is
// carried on [BeginInput] and refreshed here every time a transfer begins on the route, so editing a site's limit takes
// effect for the transfers that start after the edit, without disturbing the caps of the server or of the people using it.
func (s *Service) routeShaperOf(route uuid.UUID, rate int64) *shaper {
	s.mu.Lock()
	sh, ok := s.perRoute[route]
	if !ok {
		sh = &shaper{}
		s.perRoute[route] = sh
	}
	s.mu.Unlock()
	sh.setRate(rate)
	return sh
}

// throttle makes a transfer that moved n bytes wait as long as the caps of the server, of the person and of the route ask,
// added to the global and per-person caps of 3.8, and stops when ctx ends.
func (s *Service) throttle(ctx context.Context, userID uuid.UUID, route *shaper, n int64) {
	now := s.clock.Now()
	wait := max(s.global.reserve(now, n), s.shaperOf(userID).reserve(now, n), route.reserve(now, n))
	_ = s.cfg.Sleep(ctx, wait)
}

// forget drops the shaper of a person who has no transfer left, so the table does not grow with every person who ever transferred.
func (s *Service) forget(userID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.active {
		if k.t.UserID == userID {
			return
		}
	}
	delete(s.perUser, userID)
}

// forgetRoute drops the shaper of a route no transfer uses any more, so the table does not grow with every site or quick
// connection ever used.
func (s *Service) forgetRoute(route uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.active {
		if k.route == route {
			return
		}
	}
	delete(s.perRoute, route)
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
	list, err := s.store.ListTransfers(ctx, userID, workspaceID, limit)
	for i := range list {
		list[i] = s.decorate(list[i])
	}
	return list, err
}

// decorate adds what only the queue knows to a transfer: whether it is paused and its priority.
func (s *Service) decorate(t Transfer) Transfer {
	if state, waiting := s.sched.StateOf(t.ID); waiting {
		t.Paused, t.Priority = state.Paused, int(state.Priority)
	}
	return t
}

// Get returns one of the user's transfers.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (Transfer, error) {
	t, err := s.store.TransferByID(ctx, userID, id)
	return s.decorate(t), err
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

// waiting returns the transfer of a user that still waits for a slot; anything else is [ErrNotFound] or [ErrNotQueued].
func (s *Service) waiting(ctx context.Context, userID, id uuid.UUID) (Transfer, error) {
	t, err := s.store.TransferByID(ctx, userID, id)
	if err != nil {
		return Transfer{}, err
	}
	if _, waiting := s.sched.StateOf(id); !waiting {
		return Transfer{}, ErrNotQueued
	}
	return t, nil
}

// Pause holds a waiting transfer back: it keeps its place in the queue but is not started until resumed. A transfer
// that already runs cannot be paused, only canceled.
func (s *Service) Pause(ctx context.Context, userID, id uuid.UUID) (Transfer, error) {
	return s.change(ctx, userID, id, func() error { return s.sched.Pause(id) })
}

// Resume lets a paused transfer be started again.
func (s *Service) Resume(ctx context.Context, userID, id uuid.UUID) (Transfer, error) {
	return s.change(ctx, userID, id, func() error { return s.sched.Resume(id) })
}

// SetPriority changes where a waiting transfer stands in the queue: -1 low, 0 normal, 1 high.
func (s *Service) SetPriority(ctx context.Context, userID, id uuid.UUID, priority int) (Transfer, error) {
	p := scheduler.Priority(priority)
	if !p.Valid() {
		return Transfer{}, ErrInvalidPriority
	}
	return s.change(ctx, userID, id, func() error { return s.sched.SetPriority(id, p) })
}

// change applies a change to a waiting transfer and tells the user's clients.
func (s *Service) change(ctx context.Context, userID, id uuid.UUID, apply func() error) (Transfer, error) {
	t, err := s.waiting(ctx, userID, id)
	if err != nil {
		return Transfer{}, err
	}
	if err := apply(); err != nil {
		return Transfer{}, mapNotQueued(err)
	}
	t = s.decorate(t)
	s.publishState(t)
	return t, nil
}

// mapNotQueued turns the scheduler's "not waiting" error into the one of this package: the transfer may have started
// between the check and the change.
func mapNotQueued(err error) error {
	if errors.Is(err, scheduler.ErrNotQueued) {
		return ErrNotQueued
	}
	return err
}
