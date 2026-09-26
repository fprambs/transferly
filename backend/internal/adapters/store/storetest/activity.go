package storetest

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/activity"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// ActivityStore is the persistence an implementation offers to the activity package, plus what an entry needs to exist.
type ActivityStore interface {
	activity.Store
	site.Store
	workspace.Store
	auth.UserStore
}

// ActivityFactory returns a new, empty store for one test.
type ActivityFactory func(t *testing.T) ActivityStore

// RunActivity executes the activity store suite against stores built by newStore.
//
// Scenarios:
//   - an entry round trips with every field and UTC time, and a quick connection is stored without a site
//   - listing returns the newest first, honors the limit and keeps the users and the workspaces apart
//   - listing can be narrowed to one site
//   - deleting a site keeps its entries, without the site identifier
//   - purging removes the entries older than a time and returns how many
func RunActivity(t *testing.T, newStore ActivityFactory) {
	t.Helper()
	ctx := t.Context()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	type world struct {
		store      ActivityStore
		alice, bob auth.User
		wsA, wsB   uuid.UUID
		siteA      site.Site
	}
	setup := func(t *testing.T) world {
		t.Helper()
		w := world{store: newStore(t), alice: newUser("alice@example.com"), bob: newUser("bob@example.com")}
		for _, u := range []auth.User{w.alice, w.bob} {
			if err := w.store.CreateUser(ctx, u); err != nil {
				t.Fatal(err)
			}
			ws := newWorkspace(u.ID, workspace.KindPersonal, "Personal")
			if err := w.store.CreateWorkspace(ctx, ws); err != nil {
				t.Fatal(err)
			}
			if u.ID == w.alice.ID {
				w.wsA = ws.ID
			} else {
				w.wsB = ws.ID
			}
		}
		w.siteA = newSite(w.wsA, "Files")
		if err := w.store.CreateSite(ctx, w.siteA, nil); err != nil {
			t.Fatal(err)
		}
		return w
	}
	entry := func(w world, at time.Time, kind activity.Kind, path string) activity.Entry {
		return activity.Entry{ID: uuid.New(), UserID: w.alice.ID, WorkspaceID: w.wsA, SiteID: w.siteA.ID, SiteName: "Files", Kind: kind, Path: path, At: at}
	}

	t.Run("round trip", func(t *testing.T) {
		w := setup(t)
		want := entry(w, now, activity.Rename, "/a")
		want.Detail, want.ErrorCode = "/b", "path_exists"
		quick := entry(w, now.Add(time.Second), activity.Mkdir, "/q")
		quick.SiteID = uuid.Nil
		for _, e := range []activity.Entry{want, quick} {
			if err := w.store.InsertActivity(ctx, e); err != nil {
				t.Fatalf("Insert() = %v", err)
			}
		}
		got, err := w.store.ActivityByUser(ctx, w.alice.ID, w.wsA, uuid.Nil, 10)
		if err != nil || len(got) != 2 {
			t.Fatalf("List() = %+v, %v", got, err)
		}
		if got[0].SiteID != uuid.Nil || got[0].Path != "/q" {
			t.Errorf("newest = %+v; want the quick connection entry without a site", got[0])
		}
		if got[1] != want || got[1].At.Location().String() != "UTC" {
			t.Errorf("stored = %+v; want %+v", got[1], want)
		}
	})

	t.Run("listing order, limit and privacy", func(t *testing.T) {
		w := setup(t)
		for i := 0; i < 4; i++ {
			if err := w.store.InsertActivity(ctx, entry(w, now.Add(time.Duration(i)*time.Minute), activity.Delete, string(rune('a'+i)))); err != nil {
				t.Fatal(err)
			}
		}
		other := entry(w, now, activity.Delete, "bobs")
		other.UserID, other.WorkspaceID, other.SiteID = w.bob.ID, w.wsB, uuid.Nil
		if err := w.store.InsertActivity(ctx, other); err != nil {
			t.Fatal(err)
		}
		got, _ := w.store.ActivityByUser(ctx, w.alice.ID, w.wsA, uuid.Nil, 3)
		if len(got) != 3 || got[0].Path != "d" || got[2].Path != "b" {
			t.Errorf("List() = %+v; want the three newest, newest first", got)
		}
		if got, _ := w.store.ActivityByUser(ctx, w.bob.ID, w.wsA, uuid.Nil, 10); len(got) != 0 {
			t.Errorf("another user sees %d entries", len(got))
		}
		if got, _ := w.store.ActivityByUser(ctx, w.alice.ID, w.wsB, uuid.Nil, 10); len(got) != 0 {
			t.Errorf("another workspace lists %d entries", len(got))
		}
	})

	t.Run("filtering by site", func(t *testing.T) {
		w := setup(t)
		onSite := entry(w, now, activity.Mkdir, "/on-site")
		quick := entry(w, now.Add(time.Second), activity.Mkdir, "/quick")
		quick.SiteID = uuid.Nil
		for _, e := range []activity.Entry{onSite, quick} {
			if err := w.store.InsertActivity(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
		got, err := w.store.ActivityByUser(ctx, w.alice.ID, w.wsA, w.siteA.ID, 10)
		if err != nil || len(got) != 1 || got[0].Path != "/on-site" {
			t.Errorf("List() of one site = %+v, %v", got, err)
		}
	})

	t.Run("deleting a site keeps its entries", func(t *testing.T) {
		w := setup(t)
		if err := w.store.InsertActivity(ctx, entry(w, now, activity.Checksum, "/f")); err != nil {
			t.Fatal(err)
		}
		if err := w.store.DeleteSite(ctx, w.wsA, w.siteA.ID); err != nil {
			t.Fatal(err)
		}
		got, _ := w.store.ActivityByUser(ctx, w.alice.ID, w.wsA, uuid.Nil, 10)
		if len(got) != 1 || got[0].SiteID != uuid.Nil || got[0].SiteName != "Files" {
			t.Errorf("entries after deleting the site = %+v", got)
		}
	})

	t.Run("purge", func(t *testing.T) {
		w := setup(t)
		for _, at := range []time.Time{now.Add(-48 * time.Hour), now.Add(-24 * time.Hour), now} {
			if err := w.store.InsertActivity(ctx, entry(w, at, activity.Delete, "/x")); err != nil {
				t.Fatal(err)
			}
		}
		removed, err := w.store.PurgeActivity(ctx, now.Add(-12*time.Hour))
		if err != nil || removed != 2 {
			t.Errorf("Purge() = %d, %v; want 2", removed, err)
		}
		if got, _ := w.store.ActivityByUser(ctx, w.alice.ID, w.wsA, uuid.Nil, 10); len(got) != 1 {
			t.Errorf("%d entries left; want 1", len(got))
		}
	})
}
