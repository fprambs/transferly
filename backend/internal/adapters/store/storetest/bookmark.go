package storetest

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/bookmark"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// BookmarkStore is the persistence an implementation offers to the bookmark package, plus what a bookmark needs to exist.
type BookmarkStore interface {
	bookmark.Store
	site.Store
	workspace.Store
	auth.UserStore
}

// BookmarkFactory returns a new, empty store for one test.
type BookmarkFactory func(t *testing.T) BookmarkStore

// RunBookmark executes the bookmark store suite against stores built by newStore.
//
// Scenarios:
//   - a bookmark round trips with every field, with a UTC time
//   - names are unique per site regardless of case, and reusable on another site
//   - listing returns the site's bookmarks by name, case aside, and nothing for another site or workspace
//   - update replaces the name and path; updating through another site or workspace, or a missing bookmark, is ErrNotFound; a taken name
//     is ErrNameTaken
//   - deleting removes the bookmark; deleting through another site or twice is ErrNotFound
//   - deleting the site removes its bookmarks
func RunBookmark(t *testing.T, newStore BookmarkFactory) {
	t.Helper()
	ctx := t.Context()

	type fixture struct {
		store       BookmarkStore
		ws, other   uuid.UUID
		site, site2 uuid.UUID
		author      uuid.UUID
	}
	setup := func(t *testing.T) fixture {
		t.Helper()
		f := fixture{store: newStore(t)}
		var ids [2]uuid.UUID
		for i, email := range []string{"alice@example.com", "bob@example.com"} {
			u := newUser(email)
			if err := f.store.CreateUser(ctx, u); err != nil {
				t.Fatal(err)
			}
			w := newWorkspace(u.ID, workspace.KindPersonal, "Personal")
			if err := f.store.CreateWorkspace(ctx, w); err != nil {
				t.Fatal(err)
			}
			ids[i] = w.ID
			if i == 0 {
				f.author = u.ID
			}
		}
		f.ws, f.other = ids[0], ids[1]
		for i, name := range []string{"Prod", "Stage"} {
			s := newSite(f.ws, name)
			if err := f.store.CreateSite(ctx, s, nil); err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				f.site = s.ID
			} else {
				f.site2 = s.ID
			}
		}
		return f
	}
	mark := func(f fixture, siteID uuid.UUID, name, path string) bookmark.Bookmark {
		return bookmark.Bookmark{ID: uuid.New(), WorkspaceID: f.ws, SiteID: siteID, Name: name, Path: path, CreatedBy: f.author, CreatedAt: now}
	}

	t.Run("round trip", func(t *testing.T) {
		f := setup(t)
		b := mark(f, f.site, "Logs", "/var/log")
		if err := f.store.CreateBookmark(ctx, b); err != nil {
			t.Fatalf("CreateBookmark() = %v", err)
		}
		if got, err := f.store.Bookmark(ctx, f.ws, f.site, b.ID); err != nil || got != b {
			t.Errorf("Bookmark() = %+v, %v; want %+v", got, err, b)
		}
		if _, err := f.store.Bookmark(ctx, f.ws, f.site2, b.ID); !errors.Is(err, bookmark.ErrNotFound) {
			t.Errorf("Bookmark() through another site = %v", err)
		}
	})

	t.Run("unique names and order", func(t *testing.T) {
		f := setup(t)
		for _, b := range []bookmark.Bookmark{mark(f, f.site, "zeta", "/z"), mark(f, f.site, "Alpha", "/a"), mark(f, f.site2, "Alpha", "/a")} {
			if err := f.store.CreateBookmark(ctx, b); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.store.CreateBookmark(ctx, mark(f, f.site, "ALPHA", "/b")); !errors.Is(err, bookmark.ErrNameTaken) {
			t.Errorf("a taken name = %v; want ErrNameTaken", err)
		}
		list, err := f.store.Bookmarks(ctx, f.ws, f.site)
		if err != nil || len(list) != 2 || list[0].Name != "Alpha" || list[1].Name != "zeta" {
			t.Errorf("Bookmarks() = %+v, %v; want Alpha then zeta", list, err)
		}
		if none, _ := f.store.Bookmarks(ctx, f.other, f.site); len(none) != 0 {
			t.Errorf("Bookmarks() through another workspace = %+v", none)
		}
	})

	t.Run("update", func(t *testing.T) {
		f := setup(t)
		b, taken := mark(f, f.site, "Logs", "/var/log"), mark(f, f.site, "Home", "/home")
		for _, x := range []bookmark.Bookmark{b, taken} {
			if err := f.store.CreateBookmark(ctx, x); err != nil {
				t.Fatal(err)
			}
		}
		b.Name, b.Path = "Web logs", "/srv/logs"
		if err := f.store.UpdateBookmark(ctx, b); err != nil {
			t.Fatalf("UpdateBookmark() = %v", err)
		}
		if got, _ := f.store.Bookmark(ctx, f.ws, f.site, b.ID); got != b {
			t.Errorf("after update = %+v; want %+v", got, b)
		}
		renamed := b
		renamed.Name = "home"
		if err := f.store.UpdateBookmark(ctx, renamed); !errors.Is(err, bookmark.ErrNameTaken) {
			t.Errorf("a taken name = %v; want ErrNameTaken", err)
		}
		for _, wrong := range []bookmark.Bookmark{{ID: b.ID, WorkspaceID: f.ws, SiteID: f.site2, Name: "x", Path: "/"}, {ID: b.ID, WorkspaceID: f.other, SiteID: f.site, Name: "x", Path: "/"}, {ID: uuid.New(), WorkspaceID: f.ws, SiteID: f.site, Name: "x", Path: "/"}} {
			if err := f.store.UpdateBookmark(ctx, wrong); !errors.Is(err, bookmark.ErrNotFound) {
				t.Errorf("UpdateBookmark(%+v) = %v; want ErrNotFound", wrong, err)
			}
		}
	})

	t.Run("delete", func(t *testing.T) {
		f := setup(t)
		b := mark(f, f.site, "Logs", "/var/log")
		if err := f.store.CreateBookmark(ctx, b); err != nil {
			t.Fatal(err)
		}
		if err := f.store.DeleteBookmark(ctx, f.ws, f.site2, b.ID); !errors.Is(err, bookmark.ErrNotFound) {
			t.Errorf("DeleteBookmark() through another site = %v", err)
		}
		if err := f.store.DeleteBookmark(ctx, f.ws, f.site, b.ID); err != nil {
			t.Fatalf("DeleteBookmark() = %v", err)
		}
		if err := f.store.DeleteBookmark(ctx, f.ws, f.site, b.ID); !errors.Is(err, bookmark.ErrNotFound) {
			t.Errorf("deleting twice = %v", err)
		}
	})

	t.Run("site removal", func(t *testing.T) {
		f := setup(t)
		b := mark(f, f.site, "Logs", "/var/log")
		if err := f.store.CreateBookmark(ctx, b); err != nil {
			t.Fatal(err)
		}
		if err := f.store.DeleteSite(ctx, f.ws, f.site); err != nil {
			t.Fatal(err)
		}
		if list, _ := f.store.Bookmarks(ctx, f.ws, f.site); len(list) != 0 {
			t.Errorf("bookmarks of a deleted site = %+v", list)
		}
	})
}
