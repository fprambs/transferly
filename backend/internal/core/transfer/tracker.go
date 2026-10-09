package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/scheduler"
)

// saveTimeout bounds each write of a transfer to the store made after the request ended.
const saveTimeout = 5 * time.Second

// EventState is the type of the event sent when a transfer changes state.
const EventState = "transfer.state"

// EventProgress is the type of the event sent while data moves.
const EventProgress = "transfer.progress"

var (
	// errCanceledByUser is the cause of a context canceled by the user's request.
	errCanceledByUser = errors.New("canceled by the user")
	// ErrIncomplete tells a tracker that a transfer stopped before all its bytes moved, without an error of its own.
	ErrIncomplete = errors.New("transfer stopped before it finished")
)

// Progress is the payload of a progress event.
type Progress struct {
	// ID identifies the transfer.
	ID uuid.UUID
	// BytesDone is how many bytes moved so far.
	BytesDone int64
	// TotalBytes is the size of the file, or nil while it is unknown.
	TotalBytes *int64
}

// BeginInput describes a transfer that is about to start.
type BeginInput struct {
	// ID is chosen by the client to follow the transfer and to resume it; the nil identifier selects a new one.
	ID uuid.UUID
	// UserID is the user who transfers.
	UserID uuid.UUID
	// WorkspaceID is the workspace of the site.
	WorkspaceID uuid.UUID
	// SiteID is the site.
	SiteID uuid.UUID
	// Route is the site or quick connection that carries the data, which the limits per site count; it differs from SiteID
	// for a quick connection, which has no site record. The nil identifier means SiteID.
	Route uuid.UUID
	// RouteBandwidth caps the bytes per second the transfers of Route move together; zero means no cap of its own. It adds
	// to the global and per-user caps: a transfer waits as long as the strictest of the three asks.
	RouteBandwidth int64
	// SiteName is the name of the site, kept in the history.
	SiteName string
	// Direction is upload, download or copy.
	Direction Direction
	// Path is the remote path.
	Path string
	// Name is the file name.
	Name string
	// Total is the number of bytes to move, or a negative number when unknown.
	Total int64
	// Offset is where the transfer starts, for resumed uploads.
	Offset int64
}

// Begin records a transfer as queued and returns the tracker that follows it, with the context the data
// movement must use: canceling the transfer cancels that context. Beginning again with the identifier of an
// interrupted or failed transfer of the same file resumes it.
func (s *Service) Begin(ctx context.Context, in BeginInput) (*Tracker, context.Context, error) {
	s.mu.Lock()
	quota := s.quota
	s.mu.Unlock()
	if quota != nil {
		if err := quota.CheckTransfer(ctx, in.UserID); err != nil {
			return nil, nil, err
		}
	}
	now := s.clock.Now().UTC()
	id := in.ID
	if id == uuid.Nil {
		var err error
		if id, err = s.newID(); err != nil {
			return nil, nil, fmt.Errorf("generating transfer id: %w", err)
		}
	}

	t, err := s.store.TransferByID(ctx, in.UserID, id)
	switch {
	case errors.Is(err, ErrNotFound):
		t = Transfer{ID: id, WorkspaceID: in.WorkspaceID, UserID: in.UserID, SiteID: in.SiteID, SiteName: in.SiteName, Direction: in.Direction,
			Path: in.Path, Name: in.Name, Status: Queued, BytesDone: in.Offset, Attempts: 1, CreatedAt: now, UpdatedAt: now}
		if in.Total >= 0 {
			t.TotalBytes = &in.Total
		}
		if err := s.store.CreateTransfer(ctx, t); err != nil {
			return nil, nil, err
		}
	case err != nil:
		return nil, nil, err
	default:
		if t.Direction != in.Direction || t.Path != in.Path || t.SiteID != in.SiteID || t.WorkspaceID != in.WorkspaceID {
			return nil, nil, ErrExists
		}
		if t.Status.Active() {
			return nil, nil, ErrActive
		}
		t.Status, t.BytesDone, t.ErrorCode, t.Attempts, t.StartedAt, t.FinishedAt, t.UpdatedAt = Queued, in.Offset, "", t.Attempts+1, nil, nil, now
		t.TotalBytes = nil
		if in.Total >= 0 {
			t.TotalBytes = &in.Total
		}
		if err := s.store.UpdateTransfer(ctx, t); err != nil {
			return nil, nil, err
		}
	}

	route := in.Route
	if route == uuid.Nil {
		route = in.SiteID
	}
	runCtx, cancel := context.WithCancelCause(ctx)
	routeShaper := s.routeShaperOf(route, in.RouteBandwidth)
	k := &Tracker{s: s, t: t, route: route, routeShaper: routeShaper, ctx: runCtx, cancel: cancel, base: context.WithoutCancel(ctx), lastEvent: now, lastSave: now}
	s.mu.Lock()
	s.active[id] = k
	s.mu.Unlock()
	s.publishState(t)
	return k, runCtx, nil
}

// publishState tells the user's clients that a transfer changed state.
func (s *Service) publishState(t Transfer) {
	t = s.decorate(t)
	s.sink.Publish(ports.Event{Type: EventState, UserID: t.UserID, Data: t})
}

// Tracker follows one transfer. It is safe for concurrent use.
type Tracker struct {
	s           *Service
	route       uuid.UUID
	routeShaper *shaper
	ctx         context.Context
	cancel      context.CancelCauseFunc
	base        context.Context

	mu        sync.Mutex
	t         Transfer
	lastEvent time.Time
	lastSave  time.Time
	finished  bool
	release   func()
}

// ID returns the identifier of the transfer.
func (k *Tracker) ID() uuid.UUID { return k.t.ID }

// Snapshot returns the record of the transfer as it is now.
func (k *Tracker) Snapshot() Transfer {
	k.mu.Lock()
	t := k.t
	k.mu.Unlock()
	return k.s.decorate(t)
}

// save writes the current record while the caller holds the lock. Progress is coalesced by later saves;
// a final transition retries its idempotent database update three times within one timeout.
func (k *Tracker) save() {
	ctx, cancel := context.WithTimeout(k.base, saveTimeout)
	defer cancel()
	attempts := 1
	if k.finished {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		err := k.s.store.UpdateTransfer(ctx, k.t)
		if err == nil {
			return
		}
		if k.s.cfg.OnPersistenceError != nil {
			k.s.cfg.OnPersistenceError(err)
		}
		if attempt+1 < attempts {
			if err := k.s.cfg.Sleep(ctx, 100*time.Millisecond<<attempt); err != nil {
				return
			}
		}
	}
}

// Admit waits for the transfer's turn: it returns once the limits of the scheduler leave room for it, and the slot is
// held until [Tracker.Finish]. While it waits the transfer is queued and its user can pause it, change its priority or
// cancel it; a canceled transfer, or a context that ends, stops the wait with its error.
func (k *Tracker) Admit(ctx context.Context) error {
	k.mu.Lock()
	t := k.t
	k.mu.Unlock()
	release, err := k.s.sched.Acquire(ctx, scheduler.Request{ID: t.ID, UserID: t.UserID, WorkspaceID: t.WorkspaceID, SiteID: k.route})
	if err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.finished {
		release()
		return context.Canceled
	}
	k.release = release
	return nil
}

// Started marks the transfer as running because a connection was obtained. A non-negative total is the number
// of bytes the transfer will move.
func (k *Tracker) Started(total int64) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.finished {
		return
	}
	now := k.s.clock.Now().UTC()
	k.t.Status, k.t.StartedAt, k.t.UpdatedAt = Running, &now, now
	if total >= 0 {
		k.t.TotalBytes = &total
	}
	k.save()
	k.lastSave = now
	k.s.publishState(k.t)
}

// Progress adds n bytes to the count and then waits as long as the bandwidth caps ask, so the caller's next chunk comes no sooner than
// they allow. Events and writes to the store are throttled.
func (k *Tracker) Progress(n int64) {
	if !k.record(n) {
		return
	}
	k.s.throttle(k.ctx, k.t.UserID, k.routeShaper, n)
}

// record adds n bytes to the count and reports whether the transfer is still going.
func (k *Tracker) record(n int64) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.finished {
		return false
	}
	now := k.s.clock.Now().UTC()
	k.t.BytesDone += n
	k.t.UpdatedAt = now
	if now.Sub(k.lastEvent) >= k.s.cfg.EventEvery {
		k.lastEvent = now
		k.s.sink.Publish(ports.Event{Type: EventProgress, UserID: k.t.UserID, Data: Progress{ID: k.t.ID, BytesDone: k.t.BytesDone, TotalBytes: k.t.TotalBytes}})
	}
	if now.Sub(k.lastSave) >= k.s.cfg.SaveEvery {
		k.lastSave = now
		k.save()
	}
	return true
}

// Complete reports whether every expected byte moved.
func (k *Tracker) Complete() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.t.TotalBytes != nil && k.t.BytesDone >= *k.t.TotalBytes
}

// Finish ends the transfer. A nil err means it is done; a cancellation requested by the user ends as canceled;
// a context that ended, or [ErrIncomplete], as interrupted; anything else as failed with code as its error code.
// Finishing twice has no effect.
func (k *Tracker) Finish(err error, code string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.finished {
		return
	}
	k.finished = true
	if k.release != nil {
		k.release()
	}
	now := k.s.clock.Now().UTC()
	switch {
	case err == nil:
		k.t.Status = Done
	case errors.Is(context.Cause(k.ctx), errCanceledByUser):
		k.t.Status = Canceled
	case errors.Is(err, ErrIncomplete), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		k.t.Status = Interrupted
	default:
		k.t.Status, k.t.ErrorCode = Failed, code
	}
	k.t.UpdatedAt, k.t.FinishedAt = now, &now
	k.save()
	k.cancel(nil)
	k.s.mu.Lock()
	delete(k.s.active, k.t.ID)
	k.s.mu.Unlock()
	k.s.forget(k.t.UserID)
	k.s.forgetRoute(k.route)
	k.s.publishState(k.t)
	if k.s.cfg.OnFinish != nil {
		k.s.cfg.OnFinish(k.t)
	}
}

// finishingBody ends the transfer when a download stream is closed.
type finishingBody struct {
	io.ReadCloser
	k        *Tracker
	code     func(error) string
	untilEOF bool
	readErr  error
	eof      bool
}

// Read reads from the stream, stops when the transfer was canceled and remembers a failure.
func (b *finishingBody) Read(p []byte) (int, error) {
	if err := b.k.ctx.Err(); err != nil {
		b.readErr = context.Cause(b.k.ctx)
		return 0, err
	}
	n, err := b.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) {
		b.eof = true
	} else if err != nil {
		b.readErr = err
	}
	return n, err
}

// WriteTo implements [io.WriterTo]. It hands the destination to a stream that can write itself, so a download keeps reading ahead (4.13),
// and otherwise copies chunk by chunk through [finishingBody.Read]. A cancellation of the transfer stops it; a failure of the destination,
// such as a browser that went away, is not a failure of the transfer, which then ends as interrupted.
func (b *finishingBody) WriteTo(w io.Writer) (int64, error) {
	whole, ok := b.ReadCloser.(io.WriterTo)
	if !ok {
		return io.Copy(w, struct{ io.Reader }{b})
	}
	sink := &cancelWriter{ctx: b.k.ctx, dst: w}
	n, err := whole.WriteTo(sink)
	switch {
	case err == nil:
		b.eof = true
	case b.k.ctx.Err() != nil:
		b.readErr = context.Cause(b.k.ctx)
	case sink.err == nil || !errors.Is(err, sink.err):
		b.readErr = err
	}
	return n, err
}

// cancelWriter writes to a destination until the transfer is canceled, remembering its own failure.
type cancelWriter struct {
	ctx context.Context
	dst io.Writer
	err error
}

// Write implements [io.Writer].
func (w *cancelWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		w.err = err
		return 0, err
	}
	n, err := w.dst.Write(p)
	if err != nil {
		w.err = err
	}
	return n, err
}

// Close closes the stream and ends the transfer: done when every byte was read (or, for a stream of unknown size, when its end was reached), even if
// reading failed afterwards, as when a server drops the session once the file was sent; failed when reading failed; interrupted when the reader stopped early.
func (b *finishingBody) Close() error {
	err := b.ReadCloser.Close()
	switch {
	case b.k.Complete() || (b.untilEOF && b.eof):
		b.k.Finish(nil, "")
	case b.readErr != nil:
		b.k.Finish(b.readErr, b.code(b.readErr))
	default:
		b.k.Finish(ErrIncomplete, "")
	}
	return err
}

// Download wraps a download stream so that closing it ends the transfer. code turns a failure into its stable code.
func (k *Tracker) Download(body io.ReadCloser, code func(error) string) io.ReadCloser {
	return &finishingBody{ReadCloser: body, k: k, code: code}
}

// Stream is like [Tracker.Download] for a stream whose size is not known in advance, such as an archive: it is done when
// the reader reached the end of the stream.
func (k *Tracker) Stream(body io.ReadCloser, code func(error) string) io.ReadCloser {
	return &finishingBody{ReadCloser: body, k: k, code: code, untilEOF: true}
}
