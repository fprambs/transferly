package transfer_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/transfer"
	"github.com/fprambs/transferly/internal/testutil"
)

// errStore is the failure injected by the fake store.
var errStore = errors.New("store failure")

// memStore is an in-memory transfer.Store with switchable failures.
type memStore struct {
	mu         sync.Mutex
	items      map[uuid.UUID]transfer.Transfer
	saves      int
	failGet    error
	failCreate error
	failUpdate error
	interrupts int
}

// CreateTransfer stores a new transfer.
func (m *memStore) CreateTransfer(_ context.Context, t transfer.Transfer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failCreate != nil {
		return m.failCreate
	}
	if _, ok := m.items[t.ID]; ok {
		return transfer.ErrExists
	}
	m.items[t.ID] = t
	return nil
}

// UpdateTransfer replaces a transfer of the same user.
func (m *memStore) UpdateTransfer(_ context.Context, t transfer.Transfer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failUpdate != nil {
		return m.failUpdate
	}
	if old, ok := m.items[t.ID]; !ok || old.UserID != t.UserID {
		return transfer.ErrNotFound
	}
	m.items[t.ID] = t
	m.saves++
	return nil
}

// TransferByID returns a transfer of the user.
func (m *memStore) TransferByID(_ context.Context, user, id uuid.UUID) (transfer.Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failGet != nil {
		return transfer.Transfer{}, m.failGet
	}
	t, ok := m.items[id]
	if !ok || t.UserID != user {
		return transfer.Transfer{}, transfer.ErrNotFound
	}
	return t, nil
}

// ListTransfers returns the user's transfers, honoring the limit.
func (m *memStore) ListTransfers(_ context.Context, user, _ uuid.UUID, limit int) ([]transfer.Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []transfer.Transfer
	for _, t := range m.items {
		if t.UserID == user && len(out) < limit {
			out = append(out, t)
		}
	}
	return out, nil
}

// DeleteTransfer removes a transfer of the user.
func (m *memStore) DeleteTransfer(_ context.Context, user, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.items[id]; !ok || t.UserID != user {
		return transfer.ErrNotFound
	}
	delete(m.items, id)
	return nil
}

// InterruptActive counts the call.
func (m *memStore) InterruptActive(context.Context, time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.interrupts++
	return 3, nil
}

// stored returns the persisted state of a transfer.
func (m *memStore) stored(id uuid.UUID) transfer.Transfer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.items[id]
}

// sink records the published events.
type sink struct {
	mu     sync.Mutex
	events []ports.Event
}

// Publish records an event.
func (s *sink) Publish(e ports.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

// count returns how many events of a type were published.
func (s *sink) count(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.events {
		if e.Type == kind {
			n++
		}
	}
	return n
}

// last returns the most recent event of a type.
func (s *sink) last(kind string) ports.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.events) - 1; i >= 0; i-- {
		if s.events[i].Type == kind {
			return s.events[i]
		}
	}
	return ports.Event{}
}

// fixture bundles a service with its fakes.
type fixture struct {
	svc   *transfer.Service
	store *memStore
	sink  *sink
	clock *testutil.FakeClock
	user  uuid.UUID
	ws    uuid.UUID
	site  uuid.UUID
}

// newFixture returns a service with a fake clock, store and sink.
func newFixture() *fixture {
	f := &fixture{store: &memStore{items: map[uuid.UUID]transfer.Transfer{}}, sink: &sink{}, clock: testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		user: uuid.New(), ws: uuid.New(), site: uuid.New()}
	f.svc = transfer.NewService(f.store, f.sink, f.clock, transfer.Config{EventEvery: 250 * time.Millisecond, SaveEvery: 2 * time.Second, MaxList: 5}, nil)
	return f
}

// input returns a valid upload of a file of 1000 bytes.
func (f *fixture) input() transfer.BeginInput {
	return transfer.BeginInput{UserID: f.user, WorkspaceID: f.ws, SiteID: f.site, SiteName: "Files", Direction: transfer.Upload, Path: "/a.txt", Name: "a.txt", Total: 1000}
}

// TestService_Begin verifies how transfers start.
//
// Scenarios:
//   - a new transfer is queued with a generated identifier, the size, the offset as progress and a state event
//   - an unknown size is left unset
//   - a chosen identifier is used, and taken by another user it is ErrExists
//   - beginning again with an interrupted transfer resumes it: attempts grow, it is queued again from the new offset
//   - an active transfer cannot be begun twice, and the identifier of another file or site is ErrExists
//   - failures of the store and of the identifier generator are returned
func TestService_Begin(t *testing.T) {
	ctx := context.Background()
	f := newFixture()

	in := f.input()
	in.Offset = 10
	k, runCtx, err := f.svc.Begin(ctx, in)
	if err != nil || runCtx == nil {
		t.Fatalf("Begin() = %v", err)
	}
	stored := f.store.stored(k.ID())
	if stored.Status != transfer.Queued || stored.TotalBytes == nil || *stored.TotalBytes != 1000 || stored.BytesDone != 10 || stored.Attempts != 1 || k.ID() == uuid.Nil ||
		stored.CreatedAt.Location() != time.UTC {
		t.Errorf("stored = %+v", stored)
	}
	if f.sink.count(transfer.EventState) != 1 || f.sink.last(transfer.EventState).UserID != f.user {
		t.Errorf("events = %+v; want one state event for the user", f.sink.events)
	}

	unknown := f.input()
	unknown.Total, unknown.Path = -1, "/b"
	k2, _, err := f.svc.Begin(ctx, unknown)
	if err != nil || f.store.stored(k2.ID()).TotalBytes != nil {
		t.Errorf("unknown size: %v, %+v", err, f.store.stored(k2.ID()))
	}

	chosen := f.input()
	chosen.ID, chosen.Path = uuid.New(), "/c"
	k3, _, err := f.svc.Begin(ctx, chosen)
	if err != nil || k3.ID() != chosen.ID {
		t.Fatalf("chosen id: %v", err)
	}
	stranger := chosen
	stranger.UserID = uuid.New()
	if _, _, err := f.svc.Begin(ctx, stranger); !errors.Is(err, transfer.ErrExists) {
		t.Errorf("an identifier of another user = %v; want ErrExists", err)
	}
	if _, _, err := f.svc.Begin(ctx, chosen); !errors.Is(err, transfer.ErrActive) {
		t.Errorf("beginning an active transfer again = %v; want ErrActive", err)
	}
	other := chosen
	other.Path = "/different"
	k3.Finish(io.ErrUnexpectedEOF, "remote_unavailable")
	if _, _, err := f.svc.Begin(ctx, other); !errors.Is(err, transfer.ErrExists) {
		t.Errorf("the identifier of another file = %v; want ErrExists", err)
	}
	otherSite := chosen
	otherSite.SiteID = uuid.New()
	if _, _, err := f.svc.Begin(ctx, otherSite); !errors.Is(err, transfer.ErrExists) {
		t.Errorf("the identifier of another site = %v; want ErrExists", err)
	}
	otherWorkspace := chosen
	otherWorkspace.WorkspaceID = uuid.New()
	if _, _, err := f.svc.Begin(ctx, otherWorkspace); !errors.Is(err, transfer.ErrExists) {
		t.Errorf("the identifier of another workspace = %v; want ErrExists", err)
	}
	f.clock.Advance(time.Minute)
	resume := chosen
	resume.Offset, resume.Total = 400, 1000
	k4, _, err := f.svc.Begin(ctx, resume)
	if err != nil {
		t.Fatalf("resuming = %v", err)
	}
	if got := f.store.stored(k4.ID()); got.Attempts != 2 || got.Status != transfer.Queued || got.BytesDone != 400 || got.ErrorCode != "" || got.FinishedAt != nil || got.StartedAt != nil {
		t.Errorf("resumed transfer = %+v", got)
	}
	k4.Finish(io.ErrUnexpectedEOF, "x")
	f.store.failUpdate = errStore
	if k, _, err := f.svc.Begin(ctx, resume); err == nil {
		k.Finish(io.ErrUnexpectedEOF, "x")
		t.Error("resuming with a failing update succeeded")
	} else if !errors.Is(err, errStore) {
		t.Errorf("resuming with a failing update = %v", err)
	}
	f.store.failUpdate = nil
	noTotal := resume
	noTotal.Total = -1
	if k5, _, err := f.svc.Begin(ctx, noTotal); err != nil || f.store.stored(k5.ID()).TotalBytes != nil {
		t.Errorf("resuming with an unknown size: %v", err)
	}

	f.store.failGet = errStore
	if _, _, err := f.svc.Begin(ctx, f.input()); !errors.Is(err, errStore) {
		t.Errorf("Begin() with a failing lookup = %v", err)
	}
	f.store.failGet = nil
	f.store.failCreate = errStore
	if _, _, err := f.svc.Begin(ctx, f.input()); !errors.Is(err, errStore) {
		t.Errorf("Begin() with a failing create = %v", err)
	}
	f.store.failCreate = nil

	broken := transfer.NewService(f.store, f.sink, f.clock, transfer.Config{}, func() (uuid.UUID, error) { return uuid.Nil, errStore })
	if _, _, err := broken.Begin(ctx, f.input()); !errors.Is(err, errStore) {
		t.Errorf("Begin() with a failing generator = %v", err)
	}
}

// TestTracker_Progress verifies state changes and throttling.
//
// Scenarios:
//   - Started marks the transfer running with its start time and, when given, the size; it is saved and announced
//   - progress accumulates and is announced at most once per event interval
//   - progress is saved at most once per save interval
//   - Complete is true only when every expected byte moved
//   - a failing save does not stop the transfer
func TestTracker_Progress(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	in := f.input()
	in.Total = -1
	k, _, err := f.svc.Begin(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if k.Complete() {
		t.Error("a transfer of unknown size is complete")
	}

	k.Started(500)
	got := f.store.stored(k.ID())
	if got.Status != transfer.Running || got.StartedAt == nil || got.TotalBytes == nil || *got.TotalBytes != 500 {
		t.Fatalf("after Started() = %+v", got)
	}
	stateEvents := f.sink.count(transfer.EventState)

	k.Progress(100)
	k.Progress(100)
	if f.sink.count(transfer.EventProgress) != 0 {
		t.Error("progress was announced before the interval")
	}
	f.clock.Advance(300 * time.Millisecond)
	k.Progress(50)
	if n := f.sink.count(transfer.EventProgress); n != 1 {
		t.Fatalf("progress events = %d; want 1 after the interval", n)
	}
	event := f.sink.last(transfer.EventProgress).Data.(transfer.Progress)
	if event.ID != k.ID() || event.BytesDone != 250 || event.TotalBytes == nil || *event.TotalBytes != 500 {
		t.Errorf("progress event = %+v", event)
	}
	k.Progress(1)
	if f.sink.count(transfer.EventProgress) != 1 {
		t.Error("two progress events within one interval")
	}
	if f.store.stored(k.ID()).BytesDone != 0 {
		t.Error("progress was saved before the save interval")
	}
	f.clock.Advance(3 * time.Second)
	f.store.failUpdate = errStore
	k.Progress(9)
	f.store.failUpdate = nil
	f.clock.Advance(3 * time.Second)
	k.Progress(0)
	if saved := f.store.stored(k.ID()); saved.BytesDone != 260 {
		t.Errorf("saved progress = %d; want 260", saved.BytesDone)
	}
	if k.Complete() {
		t.Error("a partial transfer is complete")
	}
	k.Progress(240)
	if !k.Complete() {
		t.Error("a transfer with every byte is not complete")
	}
	if f.sink.count(transfer.EventState) != stateEvents {
		t.Error("progress produced state events")
	}
}

// TestTracker_Finish verifies how transfers end.
//
// Scenarios:
//   - success is done; a user cancellation is canceled; a context that ended and an incomplete transfer are interrupted
//   - any other error is failed with its code
//   - the end is saved with its time and announced, and the transfer stops being active
//   - finishing twice, and reporting after the end, change nothing
func TestTracker_Finish(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	begin := func(path string, parent context.Context) (*transfer.Tracker, context.Context) {
		in := f.input()
		in.Path = path
		k, runCtx, err := f.svc.Begin(parent, in)
		if err != nil {
			t.Fatal(err)
		}
		k.Started(-1)
		return k, runCtx
	}

	done, _ := begin("/done", ctx)
	done.Progress(1000)
	done.Finish(nil, "")
	if got := f.store.stored(done.ID()); got.Status != transfer.Done || got.FinishedAt == nil || got.ErrorCode != "" {
		t.Errorf("done = %+v", got)
	}
	states := f.sink.count(transfer.EventState)
	done.Finish(errors.New("late"), "late")
	done.Started(5)
	done.Progress(5)
	if got := f.store.stored(done.ID()); got.Status != transfer.Done || got.BytesDone != 1000 || f.sink.count(transfer.EventState) != states {
		t.Errorf("a finished transfer changed: %+v", got)
	}
	if err := f.svc.Cancel(ctx, f.user, done.ID()); !errors.Is(err, transfer.ErrNotActive) {
		t.Errorf("Cancel() of a finished transfer = %v; want ErrNotActive", err)
	}

	failed, _ := begin("/failed", ctx)
	failed.Finish(errors.New("boom"), "remote_unavailable")
	if got := f.store.stored(failed.ID()); got.Status != transfer.Failed || got.ErrorCode != "remote_unavailable" {
		t.Errorf("failed = %+v", got)
	}

	incomplete, _ := begin("/incomplete", ctx)
	incomplete.Finish(transfer.ErrIncomplete, "")
	if got := f.store.stored(incomplete.ID()); got.Status != transfer.Interrupted || got.ErrorCode != "" {
		t.Errorf("incomplete = %+v", got)
	}

	parent, disconnect := context.WithCancel(ctx)
	away, _ := begin("/away", parent)
	disconnect()
	away.Finish(context.Canceled, "")
	if got := f.store.stored(away.ID()); got.Status != transfer.Interrupted {
		t.Errorf("client that went away = %+v", got)
	}
	deadline, stop := context.WithTimeout(ctx, time.Nanosecond)
	defer stop()
	<-deadline.Done()
	late, _ := begin("/late", deadline)
	late.Finish(context.DeadlineExceeded, "")
	if got := f.store.stored(late.ID()); got.Status != transfer.Interrupted {
		t.Errorf("deadline = %+v", got)
	}

	canceled, runCtx := begin("/canceled", ctx)
	if err := f.svc.Cancel(ctx, f.user, canceled.ID()); err != nil {
		t.Fatalf("Cancel() = %v", err)
	}
	if runCtx.Err() == nil {
		t.Error("canceling did not cancel the transfer's context")
	}
	canceled.Finish(runCtx.Err(), "")
	if got := f.store.stored(canceled.ID()); got.Status != transfer.Canceled {
		t.Errorf("canceled = %+v", got)
	}
}

// TestService_Management verifies listing, reading, canceling, deleting and recovery.
//
// Scenarios:
//   - listing returns the user's transfers, capped at the configured maximum, and reading returns one
//   - transfers of another user are not found
//   - canceling an unknown transfer is ErrNotFound, one that is not running here is ErrNotActive
//   - deleting a finished transfer works, an active one is ErrActive, a missing one is ErrNotFound
//   - recovery interrupts the transfers left running by a previous process
//   - store failures are returned
func TestService_Management(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	var keep uuid.UUID
	for i := 0; i < 7; i++ {
		in := f.input()
		in.Path = "/f" + string(rune('a'+i))
		k, _, err := f.svc.Begin(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			keep = k.ID()
			continue
		}
		k.Finish(nil, "")
	}
	if list, err := f.svc.List(ctx, f.user, uuid.Nil, 0); err != nil || len(list) != 5 {
		t.Errorf("List() = %d transfers, %v; want the maximum of 5", len(list), err)
	}
	if list, _ := f.svc.List(ctx, f.user, uuid.Nil, 3); len(list) != 3 {
		t.Errorf("List() with a limit = %d", len(list))
	}
	if list, _ := f.svc.List(ctx, f.user, uuid.Nil, 500); len(list) != 5 {
		t.Errorf("List() with a limit above the maximum = %d", len(list))
	}
	if list, _ := f.svc.List(ctx, uuid.New(), uuid.Nil, 10); len(list) != 0 {
		t.Errorf("a stranger lists %d transfers", len(list))
	}
	if got, err := f.svc.Get(ctx, f.user, keep); err != nil || got.ID != keep {
		t.Errorf("Get() = %+v, %v", got, err)
	}
	stranger := uuid.New()
	if _, err := f.svc.Get(ctx, stranger, keep); !errors.Is(err, transfer.ErrNotFound) {
		t.Errorf("Get() by a stranger = %v", err)
	}
	if err := f.svc.Cancel(ctx, stranger, keep); !errors.Is(err, transfer.ErrNotFound) {
		t.Errorf("Cancel() by a stranger = %v", err)
	}
	if err := f.svc.Delete(ctx, stranger, keep); !errors.Is(err, transfer.ErrNotFound) {
		t.Errorf("Delete() by a stranger = %v", err)
	}

	if err := f.svc.Delete(ctx, f.user, keep); !errors.Is(err, transfer.ErrActive) {
		t.Errorf("Delete() of an active transfer = %v; want ErrActive", err)
	}
	orphan := transfer.Transfer{ID: uuid.New(), UserID: f.user, Status: transfer.Running}
	f.store.items[orphan.ID] = orphan
	if err := f.svc.Cancel(ctx, f.user, orphan.ID); !errors.Is(err, transfer.ErrNotActive) {
		t.Errorf("Cancel() of a transfer that no process runs = %v; want ErrNotActive", err)
	}
	if err := f.svc.Cancel(ctx, f.user, uuid.New()); !errors.Is(err, transfer.ErrNotFound) {
		t.Errorf("Cancel() of an unknown transfer = %v", err)
	}
	finished := transfer.Transfer{ID: uuid.New(), UserID: f.user, Status: transfer.Done}
	f.store.items[finished.ID] = finished
	if err := f.svc.Delete(ctx, f.user, finished.ID); err != nil {
		t.Errorf("Delete() = %v", err)
	}
	if err := f.svc.Delete(ctx, f.user, finished.ID); !errors.Is(err, transfer.ErrNotFound) {
		t.Errorf("second Delete() = %v", err)
	}

	if n, err := f.svc.Recover(ctx); err != nil || n != 3 || f.store.interrupts != 1 {
		t.Errorf("Recover() = %d, %v", n, err)
	}
	f.store.failGet = errStore
	if err := f.svc.Cancel(ctx, f.user, keep); !errors.Is(err, errStore) {
		t.Errorf("Cancel() with a failing store = %v", err)
	}
	if err := f.svc.Delete(ctx, f.user, keep); !errors.Is(err, errStore) {
		t.Errorf("Delete() with a failing store = %v", err)
	}
}

// failingBody is a download stream that fails after a while.
type failingBody struct {
	io.Reader
	closeErr error
}

// Close returns the configured error.
func (f failingBody) Close() error { return f.closeErr }

// TestTracker_Download verifies the download stream wrapper.
//
// Scenarios:
//   - reading every byte and closing ends the transfer as done, and the end of the stream is passed on
//   - closing early is interrupted, and a read error is failed with its code
//   - a read error once every byte moved, as when a server drops the session after sending the file, still ends as done
//   - a canceled transfer stops the reads with the cancellation and ends as canceled
//   - the error of closing the stream is returned
//   - a stream of unknown size is done once its end was read, interrupted when closed before it, and failed by a read error
func TestTracker_Download(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	begin := func(path string) (*transfer.Tracker, context.Context) {
		in := f.input()
		in.Path, in.Total = path, 4
		k, runCtx, err := f.svc.Begin(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		k.Started(4)
		return k, runCtx
	}
	code := func(error) string { return "remote_unavailable" }

	k, _ := begin("/ok")
	body := k.Download(failingBody{Reader: strings.NewReader("data")}, code)
	data, err := io.ReadAll(body)
	k.Progress(4)
	if err != nil || string(data) != "data" || body.Close() != nil {
		t.Fatalf("read = %q, %v", data, err)
	}
	if got := f.store.stored(k.ID()); got.Status != transfer.Done {
		t.Errorf("complete download = %+v", got)
	}

	early, _ := begin("/early")
	partial := early.Download(failingBody{Reader: strings.NewReader("data")}, code)
	_, _ = partial.Read(make([]byte, 2))
	early.Progress(2)
	_ = partial.Close()
	if got := f.store.stored(early.ID()); got.Status != transfer.Interrupted {
		t.Errorf("early close = %+v", got)
	}

	broken, _ := begin("/broken")
	failing := broken.Download(failingBody{Reader: io.MultiReader(strings.NewReader("da"), errReader{errStore})}, code)
	if _, err := io.ReadAll(failing); !errors.Is(err, errStore) {
		t.Errorf("read error = %v", err)
	}
	_ = failing.Close()
	if got := f.store.stored(broken.ID()); got.Status != transfer.Failed || got.ErrorCode != "remote_unavailable" {
		t.Errorf("failed download = %+v", got)
	}

	sent, _ := begin("/sent")
	dropped := sent.Download(failingBody{Reader: io.MultiReader(strings.NewReader("data"), errReader{errStore})}, code)
	if _, err := io.ReadAll(dropped); !errors.Is(err, errStore) {
		t.Errorf("read error after every byte = %v", err)
	}
	sent.Progress(4)
	_ = dropped.Close()
	if got := f.store.stored(sent.ID()); got.Status != transfer.Done {
		t.Errorf("download dropped after every byte = %+v; want done", got)
	}

	canceled, runCtx := begin("/canceled")
	stream := canceled.Download(failingBody{Reader: strings.NewReader("data")}, code)
	if err := f.svc.Cancel(ctx, f.user, canceled.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Read(make([]byte, 4)); !errors.Is(err, runCtx.Err()) {
		t.Errorf("read after the cancellation = %v; want the context error", err)
	}
	_ = stream.Close()
	if got := f.store.stored(canceled.ID()); got.Status != transfer.Canceled {
		t.Errorf("canceled download = %+v", got)
	}

	for name, want := range map[string]transfer.Status{"/whole": transfer.Done, "/cut": transfer.Interrupted} {
		unknown, _ := begin(name)
		stream := unknown.Stream(failingBody{Reader: strings.NewReader("archive")}, code)
		if want == transfer.Done {
			_, _ = io.ReadAll(stream)
		} else {
			_, _ = stream.Read(make([]byte, 2))
		}
		_ = stream.Close()
		if got := f.store.stored(unknown.ID()); got.Status != want {
			t.Errorf("stream %s = %+v; want %s", name, got, want)
		}
	}
	brokenStream, _ := begin("/broken-stream")
	failedStream := brokenStream.Stream(failingBody{Reader: io.MultiReader(strings.NewReader("ar"), errReader{errStore})}, code)
	_, _ = io.ReadAll(failedStream)
	_ = failedStream.Close()
	if got := f.store.stored(brokenStream.ID()); got.Status != transfer.Failed {
		t.Errorf("stream with a read error = %+v", got)
	}

	closing, _ := begin("/closing")
	withError := closing.Download(failingBody{Reader: strings.NewReader(""), closeErr: errStore}, code)
	if err := withError.Close(); !errors.Is(err, errStore) {
		t.Errorf("Close() = %v; want the stream's error", err)
	}
}

// errReader fails every read with its error.
type errReader struct{ err error }

// Read returns the error.
func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// writingBody is a download stream that writes itself, as an SFTP file reading ahead does, failing with fail after the first byte when set.
type writingBody struct {
	io.Reader
	fail error
}

// WriteTo copies the stream, or its first byte and then fail.
func (w writingBody) WriteTo(dst io.Writer) (int64, error) {
	if w.fail != nil {
		n, err := io.CopyN(dst, w.Reader, 1)
		if err != nil {
			return n, err
		}
		return n, w.fail
	}
	return io.Copy(dst, struct{ io.Reader }{w.Reader})
}

// Close closes nothing.
func (writingBody) Close() error { return nil }

// refusingWriter refuses every write, like a browser that went away.
type refusingWriter struct{}

// Write fails.
func (refusingWriter) Write([]byte) (int, error) { return 0, errors.New("client gone") }

// TestTracker_DownloadWriteTo verifies copying a download stream as a whole (4.13).
//
// Use cases:
//   - a stream that writes itself is handed the destination and the transfer ends done; one that cannot is copied chunk by chunk
//   - a stream that fails half way fails the transfer with its code
//   - a destination that fails leaves the transfer interrupted, and a cancellation stops the copy and ends it canceled
func TestTracker_DownloadWriteTo(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	begin := func(path string) *transfer.Tracker {
		in := f.input()
		in.Path, in.Total = path, 4
		k, _, err := f.svc.Begin(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		k.Started(4)
		return k
	}
	code := func(error) string { return "remote_unavailable" }
	status := func(k *transfer.Tracker) transfer.Status { return f.store.stored(k.ID()).Status }

	for name, body := range map[string]io.ReadCloser{"/writes": writingBody{Reader: strings.NewReader("data")}, "/reads": failingBody{Reader: strings.NewReader("data")}} {
		k := begin(name)
		stream := k.Download(body, code)
		var out strings.Builder
		if _, err := io.Copy(&out, stream); err != nil || out.String() != "data" {
			t.Errorf("%s copy = %q, %v", name, out.String(), err)
		}
		k.Progress(4)
		_ = stream.Close()
		if got := status(k); got != transfer.Done {
			t.Errorf("%s = %s; want done", name, got)
		}
	}

	broken := begin("/broken")
	failing := broken.Download(writingBody{Reader: strings.NewReader("data"), fail: errStore}, code)
	if _, err := io.Copy(io.Discard, failing); !errors.Is(err, errStore) {
		t.Errorf("copy of a failing stream = %v", err)
	}
	_ = failing.Close()
	if got := f.store.stored(broken.ID()); got.Status != transfer.Failed || got.ErrorCode != "remote_unavailable" {
		t.Errorf("failing stream = %+v", got)
	}

	gone := begin("/gone")
	refused := gone.Download(writingBody{Reader: strings.NewReader("data")}, code)
	if _, err := io.Copy(refusingWriter{}, refused); err == nil {
		t.Error("a destination that fails did not fail the copy")
	}
	_ = refused.Close()
	if got := status(gone); got != transfer.Interrupted {
		t.Errorf("destination gone = %s; want interrupted", got)
	}

	canceled := begin("/canceled")
	stream := canceled.Download(writingBody{Reader: strings.NewReader("data")}, code)
	if err := f.svc.Cancel(ctx, f.user, canceled.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, stream); err == nil {
		t.Error("a canceled transfer kept copying")
	}
	_ = stream.Close()
	if got := status(canceled); got != transfer.Canceled {
		t.Errorf("canceled = %s; want canceled", got)
	}
}
