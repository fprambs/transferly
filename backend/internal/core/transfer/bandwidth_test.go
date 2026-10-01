package transfer_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/scheduler"
	"github.com/fprambs/transferly/internal/core/transfer"
)

// waits records how long the service asked transfers to wait, and never really waits.
type waits struct {
	mu   sync.Mutex
	list []time.Duration
}

// sleep records a wait.
func (w *waits) sleep(_ context.Context, d time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = append(w.list, d)
	return nil
}

// last returns the most recent wait.
func (w *waits) last() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.list[len(w.list)-1]
}

// shapedFixture is a fixture whose service records its waits.
func shapedFixture() (*fixture, *waits) {
	f := newFixture()
	w := &waits{}
	f.svc = transfer.NewService(f.store, f.sink, f.clock, transfer.Config{Sleep: w.sleep}, nil)
	return f, w
}

// beginFor starts a transfer of a user and returns its tracker.
func (f *fixture) beginFor(t *testing.T, user uuid.UUID) *transfer.Tracker {
	t.Helper()
	in := f.input()
	in.UserID, in.ID = user, uuid.Nil
	k, _, err := f.svc.Begin(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// TestService_Bandwidth verifies the caps on the bytes per second.
//
// Scenarios:
//   - with no cap a transfer never waits
//   - with a cap on the whole server the first chunk goes at once and each next one waits for the time the earlier ones take
//   - after an idle time nothing is owed, so a pause never builds up a burst
//   - a cap per person is shared by the transfers of that person and does not slow other people
//   - when both caps apply the longer wait wins
//   - changing the caps reaches transfers that already run, and removing them ends the waits
//   - a person who has no transfer left starts fresh, and one who still has another keeps sharing the cap
func TestService_Bandwidth(t *testing.T) {
	f, w := shapedFixture()
	a := f.beginFor(t, f.user)
	a.Progress(1000)
	if got := w.last(); got != 0 {
		t.Fatalf("wait without a cap = %v; want none", got)
	}

	f.svc.SetBandwidth(1000, 0)
	a.Progress(500)
	a.Progress(500)
	if got := w.last(); got != 500*time.Millisecond {
		t.Errorf("second chunk waits %v; want 500ms", got)
	}
	a.Progress(1000)
	if got := w.last(); got != time.Second {
		t.Errorf("third chunk waits %v; want 1s", got)
	}
	f.clock.Advance(time.Minute)
	a.Progress(1000)
	if got := w.last(); got != 0 {
		t.Errorf("chunk after an idle minute waits %v; want none", got)
	}

	f.svc.SetBandwidth(0, 500)
	f.clock.Advance(time.Minute)
	b := f.beginFor(t, f.user)
	other := uuid.New()
	c := f.beginFor(t, other)
	a.Progress(500)
	b.Progress(500)
	if got := w.last(); got != time.Second {
		t.Errorf("a second transfer of the same person waits %v; want 1s", got)
	}
	c.Progress(500)
	if got := w.last(); got != 0 {
		t.Errorf("another person waits %v; want none", got)
	}

	f.svc.SetBandwidth(1000, 500)
	f.clock.Advance(time.Minute)
	a.Progress(500)
	b.Progress(500)
	if got := w.last(); got != time.Second {
		t.Errorf("with both caps the wait is %v; want the longer, 1s", got)
	}

	f.svc.SetBandwidth(0, 0)
	a.Progress(1 << 20)
	b.Progress(1 << 20)
	if got := w.last(); got != 0 {
		t.Errorf("after removing the caps the wait is %v; want none", got)
	}

	f.svc.SetBandwidth(0, 500)
	f.clock.Advance(time.Minute)
	a.Progress(500)
	a.Finish(nil, "")
	b.Progress(500)
	if got := w.last(); got != time.Second {
		t.Errorf("after one transfer ended the other waits %v; want it to keep sharing the cap, 1s", got)
	}
	b.Finish(nil, "")
	d := f.beginFor(t, f.user)
	d.Progress(500)
	if got := w.last(); got != 0 {
		t.Errorf("a new transfer of a person with none left waits %v; want a fresh start", got)
	}
}

// beginRoute starts a transfer against a route with the given bandwidth cap, chosen by the caller so several transfers can share one.
func (f *fixture) beginRoute(t *testing.T, route uuid.UUID, bandwidth int64) *transfer.Tracker {
	t.Helper()
	in := f.input()
	in.ID, in.Route, in.RouteBandwidth = uuid.Nil, route, bandwidth
	k, _, err := f.svc.Begin(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// TestService_RouteBandwidth verifies the cap on the bytes per second of a site or quick connection, from 4.3.
//
// Scenarios:
//   - with no cap on the route a transfer never waits
//   - a cap on the route is shared by the transfers that move through it and does not slow a different route
//   - when the route's cap and a global one both apply, the longer wait wins
//   - a later transfer on the same route changes the cap for all of them, since the site's own setting has no separate live
//     setter and is instead refreshed every time a transfer begins on it
//   - once every transfer on a route ends, a fresh one on it starts without owing anything
func TestService_RouteBandwidth(t *testing.T) {
	f, w := shapedFixture()
	siteA, siteB := f.site, uuid.New()

	a := f.beginRoute(t, siteA, 0)
	a.Progress(1 << 20)
	if got := w.last(); got != 0 {
		t.Fatalf("wait without a route cap = %v; want none", got)
	}
	a.Finish(nil, "")

	f.clock.Advance(time.Minute)
	a = f.beginRoute(t, siteA, 500)
	b := f.beginRoute(t, siteA, 500)
	c := f.beginRoute(t, siteB, 500)
	a.Progress(500)
	b.Progress(500)
	if got := w.last(); got != time.Second {
		t.Errorf("a second transfer sharing the route cap waits %v; want 1s", got)
	}
	c.Progress(500)
	if got := w.last(); got != 0 {
		t.Errorf("a transfer on another route waits %v; want none", got)
	}

	f.svc.SetBandwidth(500, 0)
	f.clock.Advance(time.Minute)
	a.Progress(500)
	b.Progress(500)
	if got := w.last(); got != time.Second {
		t.Errorf("with both a route and a global cap the wait is %v; want the longer, 1s", got)
	}

	f.clock.Advance(time.Minute)
	f.svc.SetBandwidth(0, 0)
	d := f.beginRoute(t, siteA, 250)
	a.Progress(500)
	d.Progress(500)
	if got := w.last(); got != 2*time.Second {
		t.Errorf("a later transfer on the route lowering its cap to 250 waits %v; want 2s", got)
	}

	a.Finish(nil, "")
	b.Finish(nil, "")
	c.Finish(nil, "")
	d.Finish(nil, "")
	e := f.beginRoute(t, siteA, 500)
	e.Progress(500)
	if got := w.last(); got != 0 {
		t.Errorf("a fresh transfer once the route was left by everyone waits %v; want none", got)
	}
}

// refusing is a quota that refuses every transfer.
type refusing struct{ err error }

// CheckTransfer returns the error.
func (r refusing) CheckTransfer(context.Context, uuid.UUID) error { return r.err }

// TestService_Quota verifies that a quota decides whether a transfer may start.
//
// Scenarios:
//   - without a quota, and with one that allows, a transfer starts
//   - a quota that refuses stops it before anything is stored, and its error is returned
func TestService_Quota(t *testing.T) {
	f := newFixture()
	if _, _, err := f.svc.Begin(context.Background(), f.input()); err != nil {
		t.Fatalf("Begin() without a quota = %v", err)
	}
	f.svc.UseQuota(refusing{})
	if _, _, err := f.svc.Begin(context.Background(), f.input()); err != nil {
		t.Fatalf("Begin() with a quota that allows = %v", err)
	}
	errQuota := errors.New("quota")
	f.svc.UseQuota(refusing{errQuota})
	before := len(f.store.items)
	if _, _, err := f.svc.Begin(context.Background(), f.input()); !errors.Is(err, errQuota) || len(f.store.items) != before {
		t.Errorf("Begin() with a quota that refuses = %v, %d transfers stored; want the refusal and nothing stored", err, len(f.store.items)-before)
	}
}

// TestService_SetLimits verifies that the caps of concurrent transfers change while the server runs.
//
// Scenarios:
//   - a transfer that waits because the server allows none is admitted once the cap is raised
func TestService_SetLimits(t *testing.T) {
	f := newFixture()
	f.svc = transfer.NewService(f.store, f.sink, f.clock, transfer.Config{}, nil)
	f.svc.SetLimits(limitsOf(1))
	first, second := f.beginFor(t, f.user), f.beginFor(t, f.user)
	ctx := context.Background()
	if err := first.Admit(ctx); err != nil {
		t.Fatal(err)
	}
	admitted := make(chan error, 1)
	go func() { admitted <- second.Admit(ctx) }()
	select {
	case <-admitted:
		t.Fatal("the second transfer was admitted with a cap of one")
	case <-time.After(50 * time.Millisecond):
	}
	f.svc.SetLimits(limitsOf(2))
	select {
	case err := <-admitted:
		if err != nil {
			t.Fatalf("Admit() = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the second transfer was not admitted after the cap was raised")
	}
}

// limitsOf returns caps that only limit how many transfers the server runs at once.
func limitsOf(n int) scheduler.Limits { return scheduler.Limits{Global: n} }
