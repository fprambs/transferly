package transfer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/core/transfer"
)

// TestPersistenceDiagnostic verifies that a failed final write reaches the operational error callback.
func TestPersistenceDiagnostic(t *testing.T) {
	f := newFixture()
	var reported error
	s := transfer.NewService(f.store, f.sink, f.clock, transfer.Config{OnPersistenceError: func(err error) { reported = err }}, nil)
	k, _, err := s.Begin(t.Context(), f.input())
	if err != nil {
		t.Fatal(err)
	}
	f.store.failUpdate = errStore
	k.Finish(nil, "")
	if !errors.Is(reported, errStore) {
		t.Fatalf("lost persistence error: %v", reported)
	}
}

// retryStore injects a temporary error before allowing the idempotent final update.
type retryStore struct {
	transfer.Store
	remaining, calls int
}

// UpdateTransfer fails the requested number of attempts, then delegates to the durable store.
func (s *retryStore) UpdateTransfer(ctx context.Context, record transfer.Transfer) error {
	s.calls++
	if s.remaining > 0 {
		s.remaining--
		return errStore
	}
	return s.Store.UpdateTransfer(ctx, record)
}

// TestFinalPersistenceRetry verifies recovery, bounded retries and cancellation of backoff.
func TestFinalPersistenceRetry(t *testing.T) {
	for _, stop := range []bool{false, true} {
		f := newFixture()
		store := &retryStore{Store: f.store, remaining: 1}
		var waits int
		s := transfer.NewService(store, f.sink, f.clock, transfer.Config{Sleep: func(context.Context, time.Duration) error {
			waits++
			if stop {
				return context.Canceled
			}
			return nil
		}}, nil)
		k, _, err := s.Begin(t.Context(), f.input())
		if err != nil {
			t.Fatal(err)
		}
		k.Finish(nil, "")
		if waits != 1 || (!stop && f.store.stored(k.ID()).Status != transfer.Done) || (stop && store.calls != 1) {
			t.Fatalf("retry: stop=%v waits=%d calls=%d", stop, waits, store.calls)
		}
	}
}
