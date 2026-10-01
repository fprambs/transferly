package activity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/activity"
	"github.com/fprambs/transferly/internal/testutil"
)

// memStore keeps entries in memory and records what it was asked.
type memStore struct {
	entries []activity.Entry
	limit   int
	site    uuid.UUID
	before  time.Time
	fail    error
}

// InsertActivity keeps the entry.
func (m *memStore) InsertActivity(_ context.Context, e activity.Entry) error {
	if m.fail != nil {
		return m.fail
	}
	m.entries = append(m.entries, e)
	return nil
}

// ActivityByUser remembers the limit and the site it was asked for.
func (m *memStore) ActivityByUser(_ context.Context, _, _, siteID uuid.UUID, limit int) ([]activity.Entry, error) {
	m.limit, m.site = limit, siteID
	return m.entries, m.fail
}

// PurgeActivity remembers the time it was asked to purge before.
func (m *memStore) PurgeActivity(_ context.Context, before time.Time) (int, error) {
	m.before = before
	return 3, m.fail
}

// TestService verifies recording, listing and purging the log.
//
// Scenarios:
//   - an entry is stored with a generated identifier and the current time in UTC, and a failing generator or store is reported
//   - listing asks for a default number of entries when not told, never more than the maximum, and passes the site filter on
//   - purging removes what is older than the retention time
func TestService(t *testing.T) {
	ctx := context.Background()
	clock := testutil.NewFakeClock(time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))
	store := &memStore{}
	svc := activity.NewService(store, clock, 24*time.Hour, nil)

	if err := svc.Record(ctx, activity.Entry{Kind: activity.Mkdir, Path: "/a"}); err != nil {
		t.Fatal(err)
	}
	if got := store.entries[0]; got.ID == uuid.Nil || !got.At.Equal(clock.Now()) || got.Path != "/a" {
		t.Errorf("stored = %+v; want an identifier and the current time", got)
	}
	store.fail = errors.New("disk full")
	if err := svc.Record(ctx, activity.Entry{}); !errors.Is(err, store.fail) {
		t.Errorf("Record() with a failing store = %v", err)
	}
	broken := activity.NewService(store, clock, time.Hour, func() (uuid.UUID, error) { return uuid.Nil, errors.New("no entropy") })
	if err := broken.Record(ctx, activity.Entry{}); err == nil {
		t.Error("Record() with a failing generator = nil")
	}
	store.fail = nil

	site := uuid.New()
	for asked, want := range map[int]int{0: 100, -4: 100, 25: 25, 5000: 500} {
		if _, err := svc.List(ctx, uuid.New(), uuid.New(), site, asked); err != nil || store.limit != want || store.site != site {
			t.Errorf("List(%d) asked the store for %d and site %v, %v; want %d", asked, store.limit, store.site, err, want)
		}
	}

	removed, err := svc.PurgeExpired(ctx)
	if err != nil || removed != 3 || !store.before.Equal(clock.Now().Add(-24*time.Hour)) {
		t.Errorf("PurgeExpired() = %d, %v before %v; want 3 entries older than a day", removed, err, store.before)
	}
}
