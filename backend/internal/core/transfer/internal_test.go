package transfer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/scheduler"
)

// TestMapNotQueued verifies how the scheduler's errors are reported.
//
// Scenarios:
//   - the scheduler's not-waiting error becomes ErrNotQueued, also when wrapped
//   - any other error is returned as it is
func TestMapNotQueued(t *testing.T) {
	if got := mapNotQueued(scheduler.ErrNotQueued); !errors.Is(got, ErrNotQueued) {
		t.Errorf("mapNotQueued(ErrNotQueued) = %v", got)
	}
	if got := mapNotQueued(errors.Join(scheduler.ErrNotQueued, errors.New("x"))); !errors.Is(got, ErrNotQueued) {
		t.Errorf("mapNotQueued(wrapped) = %v", got)
	}
	other := errors.New("other")
	if got := mapNotQueued(other); !errors.Is(got, other) {
		t.Errorf("mapNotQueued(other) = %v", got)
	}
}

// TestChange_Race verifies a change that loses the race against the start of the transfer.
//
// Scenarios:
//   - a transfer that stops waiting between the check and the change is reported as ErrNotQueued
func TestChange_Race(t *testing.T) {
	id := uuid.New()
	s := NewService(&raceStore{t: Transfer{ID: id}}, nil, nil, Config{Limits: scheduler.Limits{Global: 1}}, nil)
	release, err := s.sched.Acquire(context.Background(), scheduler.Request{ID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = s.sched.Acquire(ctx, scheduler.Request{ID: id}) }()
	for i := 0; i < 1000; i++ {
		if _, waiting := s.sched.StateOf(id); waiting {
			break
		}
		time.Sleep(time.Millisecond)
	}
	_, err = s.change(context.Background(), uuid.New(), id, func() error { return scheduler.ErrNotQueued })
	if !errors.Is(err, ErrNotQueued) {
		t.Errorf("change() = %v; want ErrNotQueued", err)
	}
}

// raceStore is a store that only knows one transfer.
type raceStore struct {
	Store
	t Transfer
}

// TransferByID returns the only transfer.
func (r *raceStore) TransferByID(context.Context, uuid.UUID, uuid.UUID) (Transfer, error) {
	return r.t, nil
}
