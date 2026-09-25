package storetest

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/transfer"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// TransferStore is the persistence an implementation offers to the transfer package, plus what a transfer needs to exist.
type TransferStore interface {
	transfer.Store
	site.Store
	workspace.Store
	auth.UserStore
}

// TransferFactory returns a new, empty store for one test.
type TransferFactory func(t *testing.T) TransferStore

// RunTransfer executes the transfer store suite against stores built by newStore.
//
// Scenarios:
//   - a transfer round trips with every field, optional ones included, and UTC times
//   - creating an identifier that exists, for any user, is ErrExists
//   - update replaces the mutable fields; updating a missing transfer or one of another user is ErrNotFound
//   - transfers are private to their user for reading, listing, updating and deleting
//   - listing returns the newest first, honors the limit and filters by workspace
//   - deleting a site keeps its transfers, with the nil site identifier
//   - interrupting marks queued and running transfers, and only those, with the restart code
func RunTransfer(t *testing.T, newStore TransferFactory) {
	t.Helper()
	ctx := t.Context()

	type world struct {
		store      TransferStore
		alice, bob auth.User
		wsA, wsB   uuid.UUID
		siteA      site.Site
	}
	setup := func(t *testing.T) world {
		t.Helper()
		w := world{store: newStore(t), alice: newUser("alice@example.com"), bob: newUser("bob@example.com")}
		for i, u := range []auth.User{w.alice, w.bob} {
			if err := w.store.CreateUser(ctx, u); err != nil {
				t.Fatal(err)
			}
			ws := newWorkspace(u.ID, workspace.KindPersonal, "Personal")
			if err := w.store.CreateWorkspace(ctx, ws); err != nil {
				t.Fatal(err)
			}
			if i == 0 {
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
	newTransfer := func(w world, at time.Time, status transfer.Status) transfer.Transfer {
		return transfer.Transfer{
			ID: uuid.New(), WorkspaceID: w.wsA, UserID: w.alice.ID, SiteID: w.siteA.ID, SiteName: "Files", Direction: transfer.Upload,
			Path: "/a/b.txt", Name: "b.txt", Status: status, BytesDone: 0, Attempts: 1, CreatedAt: at, UpdatedAt: at,
		}
	}

	t.Run("round trip", func(t *testing.T) {
		w := setup(t)
		total, started, finished := int64(1234), now.Add(time.Second), now.Add(2*time.Second)
		want := newTransfer(w, now, transfer.Done)
		want.TotalBytes, want.StartedAt, want.FinishedAt, want.BytesDone, want.ErrorCode, want.Attempts = &total, &started, &finished, 1234, "x", 3
		if err := w.store.CreateTransfer(ctx, want); err != nil {
			t.Fatalf("Create() = %v", err)
		}
		got, err := w.store.TransferByID(ctx, w.alice.ID, want.ID)
		if err != nil {
			t.Fatalf("Get() = %v", err)
		}
		if got.ID != want.ID || got.WorkspaceID != want.WorkspaceID || got.SiteID != want.SiteID || got.SiteName != "Files" || got.Direction != transfer.Upload ||
			got.Path != want.Path || got.Name != want.Name || got.Status != transfer.Done || got.BytesDone != 1234 || got.ErrorCode != "x" || got.Attempts != 3 ||
			got.TotalBytes == nil || *got.TotalBytes != 1234 || !got.CreatedAt.Equal(now) || got.CreatedAt.Location().String() != "UTC" ||
			got.StartedAt == nil || !got.StartedAt.Equal(started) || got.StartedAt.Location().String() != "UTC" ||
			got.FinishedAt == nil || !got.FinishedAt.Equal(finished) || !got.UpdatedAt.Equal(now) {
			t.Fatalf("Get() = %+v; want %+v", got, want)
		}
		bare := newTransfer(w, now, transfer.Queued)
		if err := w.store.CreateTransfer(ctx, bare); err != nil {
			t.Fatal(err)
		}
		got, _ = w.store.TransferByID(ctx, w.alice.ID, bare.ID)
		if got.TotalBytes != nil || got.StartedAt != nil || got.FinishedAt != nil {
			t.Errorf("optional fields of a new transfer = %+v; want nil", got)
		}
	})

	t.Run("identifiers are unique", func(t *testing.T) {
		w := setup(t)
		first := newTransfer(w, now, transfer.Queued)
		if err := w.store.CreateTransfer(ctx, first); err != nil {
			t.Fatal(err)
		}
		if err := w.store.CreateTransfer(ctx, first); !errors.Is(err, transfer.ErrExists) {
			t.Errorf("duplicate = %v; want ErrExists", err)
		}
		other := first
		other.UserID, other.WorkspaceID, other.SiteID = w.bob.ID, w.wsB, uuid.Nil
		if err := w.store.CreateTransfer(ctx, other); !errors.Is(err, transfer.ErrExists) {
			t.Errorf("same identifier for another user = %v; want ErrExists", err)
		}
	})

	t.Run("update", func(t *testing.T) {
		w := setup(t)
		tr := newTransfer(w, now, transfer.Queued)
		if err := w.store.CreateTransfer(ctx, tr); err != nil {
			t.Fatal(err)
		}
		total, started, finished := int64(10), now.Add(time.Second), now.Add(time.Minute)
		tr.Status, tr.TotalBytes, tr.BytesDone, tr.ErrorCode, tr.Attempts, tr.StartedAt, tr.FinishedAt, tr.UpdatedAt = transfer.Failed, &total, 7, "remote_unavailable", 2, &started, &finished, finished
		if err := w.store.UpdateTransfer(ctx, tr); err != nil {
			t.Fatalf("Update() = %v", err)
		}
		got, _ := w.store.TransferByID(ctx, w.alice.ID, tr.ID)
		if got.Status != transfer.Failed || got.BytesDone != 7 || got.ErrorCode != "remote_unavailable" || got.Attempts != 2 || got.TotalBytes == nil || *got.TotalBytes != 10 ||
			got.FinishedAt == nil || !got.UpdatedAt.Equal(finished) {
			t.Errorf("Get() after update = %+v", got)
		}
		ghost := tr
		ghost.ID = uuid.New()
		if err := w.store.UpdateTransfer(ctx, ghost); !errors.Is(err, transfer.ErrNotFound) {
			t.Errorf("Update() of a missing transfer = %v; want ErrNotFound", err)
		}
		foreign := tr
		foreign.UserID = w.bob.ID
		if err := w.store.UpdateTransfer(ctx, foreign); !errors.Is(err, transfer.ErrNotFound) {
			t.Errorf("Update() by another user = %v; want ErrNotFound", err)
		}
	})

	t.Run("privacy and listing", func(t *testing.T) {
		w := setup(t)
		older, newer := newTransfer(w, now, transfer.Done), newTransfer(w, now.Add(time.Hour), transfer.Done)
		mine := newTransfer(w, now.Add(2*time.Hour), transfer.Done)
		mine.WorkspaceID = w.wsB
		theirs := newTransfer(w, now.Add(3*time.Hour), transfer.Done)
		theirs.UserID, theirs.WorkspaceID, theirs.SiteID = w.bob.ID, w.wsB, uuid.Nil
		for _, tr := range []transfer.Transfer{older, newer, mine, theirs} {
			if err := w.store.CreateTransfer(ctx, tr); err != nil {
				t.Fatal(err)
			}
		}
		all, err := w.store.ListTransfers(ctx, w.alice.ID, uuid.Nil, 10)
		if err != nil || len(all) != 3 || all[0].ID != mine.ID || all[1].ID != newer.ID || all[2].ID != older.ID {
			t.Fatalf("List() = %+v, %v; want her three transfers, newest first", all, err)
		}
		if limited, _ := w.store.ListTransfers(ctx, w.alice.ID, uuid.Nil, 2); len(limited) != 2 {
			t.Errorf("List() with a limit returned %d", len(limited))
		}
		if filtered, _ := w.store.ListTransfers(ctx, w.alice.ID, w.wsA, 10); len(filtered) != 2 || filtered[0].ID != newer.ID {
			t.Errorf("List() by workspace = %+v", filtered)
		}
		if none, err := w.store.ListTransfers(ctx, uuid.New(), uuid.Nil, 10); err != nil || len(none) != 0 {
			t.Errorf("List() of a stranger = %v, %v", none, err)
		}
		if _, err := w.store.TransferByID(ctx, w.bob.ID, older.ID); !errors.Is(err, transfer.ErrNotFound) {
			t.Errorf("Get() of another user's transfer = %v; want ErrNotFound", err)
		}
		if err := w.store.DeleteTransfer(ctx, w.bob.ID, older.ID); !errors.Is(err, transfer.ErrNotFound) {
			t.Errorf("Delete() of another user's transfer = %v; want ErrNotFound", err)
		}
		if err := w.store.DeleteTransfer(ctx, w.alice.ID, older.ID); err != nil {
			t.Errorf("Delete() = %v", err)
		}
		if err := w.store.DeleteTransfer(ctx, w.alice.ID, older.ID); !errors.Is(err, transfer.ErrNotFound) {
			t.Errorf("second Delete() = %v; want ErrNotFound", err)
		}
	})

	t.Run("site deletion keeps the history", func(t *testing.T) {
		w := setup(t)
		tr := newTransfer(w, now, transfer.Done)
		if err := w.store.CreateTransfer(ctx, tr); err != nil {
			t.Fatal(err)
		}
		if err := w.store.DeleteSite(ctx, w.wsA, w.siteA.ID); err != nil {
			t.Fatal(err)
		}
		got, err := w.store.TransferByID(ctx, w.alice.ID, tr.ID)
		if err != nil || got.SiteID != uuid.Nil || got.SiteName != "Files" {
			t.Errorf("Get() after deleting the site = %+v, %v; want the transfer with the nil site", got, err)
		}
	})

	t.Run("interrupt active", func(t *testing.T) {
		w := setup(t)
		states := []transfer.Status{transfer.Queued, transfer.Running, transfer.Done, transfer.Failed, transfer.Canceled}
		ids := map[transfer.Status]uuid.UUID{}
		for _, s := range states {
			tr := newTransfer(w, now, s)
			ids[s] = tr.ID
			if err := w.store.CreateTransfer(ctx, tr); err != nil {
				t.Fatal(err)
			}
		}
		at := now.Add(time.Hour)
		n, err := w.store.InterruptActive(ctx, at)
		if err != nil || n != 2 {
			t.Fatalf("InterruptActive() = %d, %v; want 2", n, err)
		}
		for _, s := range states {
			got, _ := w.store.TransferByID(ctx, w.alice.ID, ids[s])
			active := s == transfer.Queued || s == transfer.Running
			if active && (got.Status != transfer.Interrupted || got.ErrorCode != "server_restarted" || got.FinishedAt == nil || !got.FinishedAt.Equal(at)) {
				t.Errorf("%s transfer after the restart = %+v", s, got)
			}
			if !active && got.Status != s {
				t.Errorf("%s transfer changed to %s", s, got.Status)
			}
		}
		if n, _ := w.store.InterruptActive(ctx, at); n != 0 {
			t.Errorf("second InterruptActive() = %d; want 0", n)
		}
	})
}
