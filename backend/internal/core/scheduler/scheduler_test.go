package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/scheduler"
)

// ids used across the tests.
var (
	userA, userB = uuid.New(), uuid.New()
	workA, workB = uuid.New(), uuid.New()
	siteA, siteB = uuid.New(), uuid.New()
)

// pending is a request that asked for a slot.
type pending struct {
	req     scheduler.Request
	granted chan struct{}
	release func()
	err     error
}

// ask makes a request in the background and returns what it will get.
func ask(s *scheduler.Scheduler, ctx context.Context, r scheduler.Request) *pending {
	p := &pending{req: r, granted: make(chan struct{})}
	go func() {
		p.release, p.err = s.Acquire(ctx, r)
		close(p.granted)
	}()
	return p
}

// request builds a request with fresh identifiers unless given.
func request(user, work, site uuid.UUID, priority scheduler.Priority) scheduler.Request {
	return scheduler.Request{ID: uuid.New(), UserID: user, WorkspaceID: work, SiteID: site, Priority: priority}
}

// waitFor polls a condition until it holds or the test gives up.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// isGranted reports whether a pending request was admitted.
func (p *pending) isGranted() bool {
	select {
	case <-p.granted:
		return true
	default:
		return false
	}
}

// never checks that a request is still waiting after the scheduler had time to admit it.
func never(t *testing.T, s *scheduler.Scheduler, p *pending, what string) {
	t.Helper()
	waitFor(t, what+" to be queued", func() bool { _, queued := s.StateOf(p.req.ID); return queued })
	time.Sleep(20 * time.Millisecond)
	if p.isGranted() {
		t.Errorf("%s was admitted", what)
	}
}

// TestLimits verifies each of the four limits.
//
// Scenarios:
//   - with no limits everything runs at once
//   - the global limit holds back the request after the limit, until a slot is given back
//   - the per workspace, per user and per site limits each hold back only their own dimension
func TestLimits(t *testing.T) {
	t.Run("no limits", func(t *testing.T) {
		s := scheduler.New(scheduler.Limits{})
		var all []*pending
		for range 50 {
			all = append(all, ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal)))
		}
		for _, p := range all {
			<-p.granted
		}
		if s.Running() != 50 || s.Waiting() != 0 {
			t.Errorf("running %d waiting %d; want 50 and 0", s.Running(), s.Waiting())
		}
	})

	for name, tt := range map[string]struct {
		limits  scheduler.Limits
		blocked scheduler.Request
		free    scheduler.Request
	}{
		"global":    {scheduler.Limits{Global: 1}, request(userB, workB, siteB, 0), scheduler.Request{}},
		"workspace": {scheduler.Limits{PerWorkspace: 1}, request(userB, workA, siteB, 0), request(userB, workB, siteB, 0)},
		"user":      {scheduler.Limits{PerUser: 1}, request(userA, workB, siteB, 0), request(userB, workB, siteB, 0)},
		"site":      {scheduler.Limits{PerSite: 1}, request(userB, workB, siteA, 0), request(userB, workB, siteB, 0)},
	} {
		t.Run(name, func(t *testing.T) {
			s := scheduler.New(tt.limits)
			first := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
			<-first.granted

			blocked := ask(s, t.Context(), tt.blocked)
			never(t, s, blocked, name+" blocked request")
			if tt.free.ID != uuid.Nil {
				other := ask(s, t.Context(), tt.free)
				<-other.granted
			}

			first.release()
			<-blocked.granted
		})
	}
}

// TestOrder verifies who is admitted first.
//
// Scenarios:
//   - waiting requests are served by priority, then by arrival
//   - a request that cannot run because its site is full does not hold back a later one for another site
//   - changing the priority of a waiting request moves it
func TestOrder(t *testing.T) {
	s := scheduler.New(scheduler.Limits{Global: 1})
	holder := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
	<-holder.granted

	low := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Low))
	waitFor(t, "low", func() bool { _, ok := s.StateOf(low.req.ID); return ok })
	normal1 := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
	waitFor(t, "normal1", func() bool { _, ok := s.StateOf(normal1.req.ID); return ok })
	normal2 := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
	waitFor(t, "normal2", func() bool { _, ok := s.StateOf(normal2.req.ID); return ok })
	high := ask(s, t.Context(), request(userA, workA, siteA, scheduler.High))
	waitFor(t, "high", func() bool { _, ok := s.StateOf(high.req.ID); return ok })

	var order []string
	names := map[*pending]string{high: "high", normal1: "normal1", normal2: "normal2", low: "low"}
	current := holder
	for range 4 {
		current.release()
		waitFor(t, "an admission", func() bool {
			for p := range names {
				if p.isGranted() && names[p] != "" {
					order = append(order, names[p])
					current = p
					delete(names, p)
					return true
				}
			}
			return false
		})
	}
	current.release()
	if got := order; len(got) != 4 || got[0] != "high" || got[1] != "normal1" || got[2] != "normal2" || got[3] != "low" {
		t.Errorf("admitted in order %v; want high, normal1, normal2, low", got)
	}

	t.Run("a full site does not block others", func(t *testing.T) {
		s := scheduler.New(scheduler.Limits{PerSite: 1})
		holder := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
		<-holder.granted
		stuck := ask(s, t.Context(), request(userA, workA, siteA, scheduler.High))
		never(t, s, stuck, "the request for the full site")

		other := ask(s, t.Context(), request(userA, workA, siteB, scheduler.Low))
		<-other.granted
		if stuck.isGranted() {
			t.Error("the request for the full site was admitted")
		}
	})

	t.Run("changing the priority moves a request", func(t *testing.T) {
		s := scheduler.New(scheduler.Limits{Global: 1})
		holder := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
		<-holder.granted
		first := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
		waitFor(t, "first", func() bool { _, ok := s.StateOf(first.req.ID); return ok })
		second := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
		waitFor(t, "second", func() bool { _, ok := s.StateOf(second.req.ID); return ok })

		if err := s.SetPriority(second.req.ID, scheduler.High); err != nil {
			t.Fatal(err)
		}
		if state, _ := s.StateOf(second.req.ID); state.Priority != scheduler.High {
			t.Errorf("state = %+v; want High", state)
		}
		holder.release()
		<-second.granted
		if first.isGranted() {
			t.Error("the first request was admitted before the one raised to High")
		}
	})
}

// TestPauseAndPriority verifies the controls of waiting requests.
//
// Scenarios:
//   - a paused request keeps waiting while slots are free and is admitted when resumed
//   - pausing, resuming and changing the priority of a request that does not wait is ErrNotQueued, and a priority that does not exist is refused
func TestPauseAndPriority(t *testing.T) {
	s := scheduler.New(scheduler.Limits{Global: 1})
	holder := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
	<-holder.granted
	held := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
	waitFor(t, "queued", func() bool { _, ok := s.StateOf(held.req.ID); return ok })

	if err := s.Pause(held.req.ID); err != nil {
		t.Fatal(err)
	}
	if state, _ := s.StateOf(held.req.ID); !state.Paused {
		t.Error("the request is not paused")
	}
	holder.release()
	never(t, s, held, "the paused request")
	if s.Running() != 0 || s.Waiting() != 1 {
		t.Errorf("running %d waiting %d; want 0 and 1 with a free slot and a paused request", s.Running(), s.Waiting())
	}

	if err := s.Resume(held.req.ID); err != nil {
		t.Fatal(err)
	}
	<-held.granted

	gone := uuid.New()
	for name, err := range map[string]error{
		"Pause":                 s.Pause(gone),
		"Resume":                s.Resume(gone),
		"SetPriority":           s.SetPriority(gone, scheduler.High),
		"Pause after admission": s.Pause(held.req.ID),
	} {
		if !errors.Is(err, scheduler.ErrNotQueued) {
			t.Errorf("%s = %v; want ErrNotQueued", name, err)
		}
	}
	if _, ok := s.StateOf(gone); ok {
		t.Error("StateOf() knows a request that never queued")
	}
	if err := s.SetPriority(held.req.ID, scheduler.Priority(7)); !errors.Is(err, scheduler.ErrInvalidPriority) {
		t.Errorf("SetPriority(7) = %v; want ErrInvalidPriority", err)
	}
}

// TestCancellation verifies that a request that gives up leaves no trace.
//
// Scenarios:
//   - a waiting request whose context ends leaves the queue with the context error and never takes a slot
//   - a request that is admitted just as its context ends gives its slot back
//   - releasing twice frees one slot only
func TestCancellation(t *testing.T) {
	s := scheduler.New(scheduler.Limits{Global: 1})
	holder := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
	<-holder.granted

	ctx, cancel := context.WithCancel(t.Context())
	waiting := ask(s, ctx, request(userA, workA, siteA, scheduler.Normal))
	waitFor(t, "queued", func() bool { return s.Waiting() == 1 })
	cancel()
	<-waiting.granted
	if !errors.Is(waiting.err, context.Canceled) || waiting.release != nil {
		t.Errorf("Acquire() = %v; want context.Canceled and no release", waiting.err)
	}
	if s.Waiting() != 0 || s.Running() != 1 {
		t.Errorf("waiting %d running %d; want 0 and 1", s.Waiting(), s.Running())
	}
	holder.release()
	holder.release()
	if s.Running() != 0 {
		t.Errorf("running = %d after releasing twice; want 0", s.Running())
	}

	free := scheduler.New(scheduler.Limits{})
	for range 200 {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		release, err := free.Acquire(ctx, request(userA, workA, siteA, scheduler.Normal))
		if err == nil {
			release()
		}
	}
	if free.Running() != 0 || free.Waiting() != 0 {
		t.Errorf("running %d waiting %d after canceled requests; want none", free.Running(), free.Waiting())
	}
}

// TestSetLimits verifies that raising a limit admits waiting requests at once.
func TestSetLimits(t *testing.T) {
	s := scheduler.New(scheduler.Limits{Global: 1})
	holder := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
	<-holder.granted
	waiting := ask(s, t.Context(), request(userA, workA, siteA, scheduler.Normal))
	never(t, s, waiting, "the second request")

	s.SetLimits(scheduler.Limits{Global: 2})

	<-waiting.granted
}

// TestManyTransfersAcrossSites verifies the limits under load, as a mixed workload meets them.
//
// Scenarios:
//   - thirty transfers to three sites, with two slots per site and six overall, never exceed either limit, and all finish
func TestManyTransfersAcrossSites(t *testing.T) {
	s := scheduler.New(scheduler.Limits{Global: 6, PerSite: 2})
	sites := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	var (
		mu        sync.Mutex
		perSite   = map[uuid.UUID]int{}
		total     int
		peakSite  int
		peakTotal int
		finished  atomic.Int32
		wg        sync.WaitGroup
	)
	for i := range 30 {
		site := sites[i%3]
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := s.Acquire(t.Context(), request(userA, workA, site, scheduler.Normal))
			if err != nil {
				t.Errorf("Acquire() = %v", err)
				return
			}
			mu.Lock()
			perSite[site]++
			total++
			peakSite, peakTotal = max(peakSite, perSite[site]), max(peakTotal, total)
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
			mu.Lock()
			perSite[site]--
			total--
			mu.Unlock()
			release()
			finished.Add(1)
		}()
	}
	wg.Wait()
	if peakSite > 2 || peakTotal > 6 || finished.Load() != 30 {
		t.Errorf("peak per site %d, peak total %d, finished %d; want at most 2, 6 and 30", peakSite, peakTotal, finished.Load())
	}
}

// TestPriorityValidity verifies which priorities exist.
func TestPriorityValidity(t *testing.T) {
	for p, want := range map[scheduler.Priority]bool{scheduler.Low: true, scheduler.Normal: true, scheduler.High: true, -2: false, 2: false} {
		if p.Valid() != want {
			t.Errorf("Priority(%d).Valid() = %v; want %v", p, p.Valid(), want)
		}
	}
}
