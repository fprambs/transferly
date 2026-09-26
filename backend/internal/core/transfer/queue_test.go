package transfer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/scheduler"
	"github.com/fprambs/sentinel/internal/core/transfer"
)

// limited returns a fixture whose service runs one transfer at a time per site.
func limited() *fixture {
	f := newFixture()
	f.svc = transfer.NewService(f.store, f.sink, f.clock, transfer.Config{EventEvery: time.Millisecond, Limits: scheduler.Limits{PerSite: 1}}, nil)
	return f
}

// admitAsync admits a tracker in the background and reports the outcome on the returned channel.
func admitAsync(k *transfer.Tracker, ctx context.Context) chan error {
	done := make(chan error, 1)
	go func() { done <- k.Admit(ctx) }()
	return done
}

// waitQueued waits until the scheduler holds the transfer as waiting.
func waitQueued(t *testing.T, f *fixture, id uuid.UUID) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if _, err := f.svc.Pause(context.Background(), f.user, id); !errors.Is(err, transfer.ErrNotQueued) {
			if _, err := f.svc.Resume(context.Background(), f.user, id); err != nil {
				t.Fatal(err)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the transfer never waited")
}

// begin starts a transfer of a path.
func (f *fixture) begin(t *testing.T, path string) (*transfer.Tracker, context.Context) {
	t.Helper()
	in := f.input()
	in.Path = path
	k, ctx, err := f.svc.Begin(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return k, ctx
}

// TestService_Queue verifies the waiting line of transfers.
//
// Scenarios:
//   - with one slot per site the second transfer waits, with normal priority and not paused, and the first one is not waiting
//   - a snapshot of a tracker returns its record
//   - pausing a waiting transfer shows in Get and List, tells the clients, and keeps it from starting when the slot frees
//   - resuming a paused transfer lets it start once a slot is free, and finishing a transfer frees its slot
//   - a priority is applied to a waiting transfer and an unknown priority is ErrInvalidPriority
//   - pausing, resuming or reprioritizing a running transfer is ErrNotQueued, an unknown one or one of another user is ErrNotFound
//   - canceling a waiting transfer ends its wait with an error and it takes no slot
//   - the store failing is returned by the changes
//   - a transfer that ended before being admitted does not keep a slot
//   - transfers on different routes of one site id do not wait for each other, while an empty route uses the site
func TestService_Queue(t *testing.T) {
	ctx := context.Background()
	f := limited()
	first, firstCtx := f.begin(t, "/one")
	second, secondCtx := f.begin(t, "/two")
	if err := first.Admit(firstCtx); err != nil {
		t.Fatal(err)
	}
	wait := admitAsync(second, secondCtx)
	waitQueued(t, f, second.ID())

	if snap := second.Snapshot(); snap.ID != second.ID() || snap.Status != transfer.Queued {
		t.Errorf("Snapshot() = %+v; want the queued transfer", snap)
	}
	got, err := f.svc.Get(ctx, f.user, second.ID())
	if err != nil || got.Paused || got.Priority != 0 || got.Status != transfer.Queued {
		t.Errorf("Get() = %+v, %v; want queued, not paused, normal priority", got, err)
	}
	if running, _ := f.svc.Get(ctx, f.user, first.ID()); running.Paused || running.Priority != 0 {
		t.Errorf("a running transfer reports %+v", running)
	}

	before := f.sink.count(transfer.EventState)
	paused, err := f.svc.Pause(ctx, f.user, second.ID())
	if err != nil || !paused.Paused {
		t.Fatalf("Pause() = %+v, %v", paused, err)
	}
	if f.sink.count(transfer.EventState) != before+1 {
		t.Error("pausing did not tell the clients")
	}
	list, _ := f.svc.List(ctx, f.user, uuid.Nil, 10)
	found := false
	for _, item := range list {
		if item.ID == second.ID() {
			found = item.Paused
		}
	}
	if !found {
		t.Error("the listing does not show the pause")
	}
	first.Finish(nil, "")
	select {
	case err := <-wait:
		t.Fatalf("a paused transfer was admitted: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	if _, err := f.svc.SetPriority(ctx, f.user, second.ID(), 7); !errors.Is(err, transfer.ErrInvalidPriority) {
		t.Errorf("SetPriority(7) = %v; want ErrInvalidPriority", err)
	}
	raised, err := f.svc.SetPriority(ctx, f.user, second.ID(), 1)
	if err != nil || raised.Priority != 1 || !raised.Paused {
		t.Errorf("SetPriority(1) = %+v, %v", raised, err)
	}
	resumed, err := f.svc.Resume(ctx, f.user, second.ID())
	if err != nil || resumed.Paused {
		t.Fatalf("Resume() = %+v, %v", resumed, err)
	}
	if err := <-wait; err != nil {
		t.Fatalf("the resumed transfer was not admitted: %v", err)
	}

	if _, err := f.svc.Pause(ctx, f.user, second.ID()); !errors.Is(err, transfer.ErrNotQueued) {
		t.Errorf("Pause() of a running transfer = %v; want ErrNotQueued", err)
	}
	if _, err := f.svc.Resume(ctx, f.user, second.ID()); !errors.Is(err, transfer.ErrNotQueued) {
		t.Errorf("Resume() of a running transfer = %v; want ErrNotQueued", err)
	}
	if _, err := f.svc.SetPriority(ctx, f.user, second.ID(), 0); !errors.Is(err, transfer.ErrNotQueued) {
		t.Errorf("SetPriority() of a running transfer = %v; want ErrNotQueued", err)
	}
	if _, err := f.svc.Pause(ctx, f.user, first.ID()); !errors.Is(err, transfer.ErrNotQueued) {
		t.Errorf("Pause() of a finished transfer = %v; want ErrNotQueued", err)
	}
	if _, err := f.svc.Pause(ctx, uuid.New(), second.ID()); !errors.Is(err, transfer.ErrNotFound) {
		t.Errorf("Pause() of another user's transfer = %v; want ErrNotFound", err)
	}
	if _, err := f.svc.Pause(ctx, f.user, uuid.New()); !errors.Is(err, transfer.ErrNotFound) {
		t.Errorf("Pause() of an unknown transfer = %v; want ErrNotFound", err)
	}

	third, thirdCtx := f.begin(t, "/three")
	cancelWait := admitAsync(third, thirdCtx)
	waitQueued(t, f, third.ID())
	if err := f.svc.Cancel(ctx, f.user, third.ID()); err != nil {
		t.Fatal(err)
	}
	if err := <-cancelWait; !errors.Is(err, context.Canceled) {
		t.Errorf("Admit() of a canceled transfer = %v; want context.Canceled", err)
	}
	third.Finish(context.Canceled, "")
	second.Finish(nil, "")

	fourth, fourthCtx := f.begin(t, "/four")
	if err := fourth.Admit(fourthCtx); err != nil {
		t.Fatalf("a slot was not freed: %v", err)
	}
	fifth, fifthCtx := f.begin(t, "/five")
	fifthWait := admitAsync(fifth, fifthCtx)
	waitQueued(t, f, fifth.ID())
	f.store.failGet = errStore
	if _, err := f.svc.Pause(ctx, f.user, fifth.ID()); !errors.Is(err, errStore) {
		t.Errorf("Pause() with a failing store = %v", err)
	}
	f.store.failGet = nil
	fourth.Finish(nil, "")
	if err := <-fifthWait; err != nil {
		t.Fatal(err)
	}
	fifth.Finish(nil, "")

	ended, _ := f.begin(t, "/ended")
	ended.Finish(nil, "")
	if err := ended.Admit(context.Background()); !errors.Is(err, context.Canceled) {
		t.Errorf("Admit() after Finish() = %v; want context.Canceled", err)
	}
	after, afterCtx := f.begin(t, "/after")
	if err := after.Admit(afterCtx); err != nil {
		t.Errorf("the slot of a transfer that ended before being admitted was kept: %v", err)
	}
	after.Finish(nil, "")

	inA := f.input()
	inA.Path, inA.Route = "/route-a", uuid.New()
	inB := f.input()
	inB.Path, inB.Route = "/route-b", uuid.New()
	a, aCtx, _ := f.svc.Begin(ctx, inA)
	b, bCtx, _ := f.svc.Begin(ctx, inB)
	if err := a.Admit(aCtx); err != nil {
		t.Fatal(err)
	}
	if err := b.Admit(bCtx); err != nil {
		t.Errorf("another route waited: %v", err)
	}
	a.Finish(nil, "")
	b.Finish(nil, "")
}
