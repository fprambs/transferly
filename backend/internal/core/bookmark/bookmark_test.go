package bookmark_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/bookmark"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/testutil"
)

// errStore is the failure injected by the fake store.
var errStore = errors.New("store failure")

// memStore is an in-memory bookmark.Store with switchable failures.
type memStore struct {
	items      []bookmark.Bookmark
	failList   bool
	failCreate bool
	failUpdate bool
	failDelete bool
}

// CreateBookmark stores a bookmark unless the name is taken on its site.
func (m *memStore) CreateBookmark(_ context.Context, b bookmark.Bookmark) error {
	if m.failCreate {
		return errStore
	}
	for _, other := range m.items {
		if other.SiteID == b.SiteID && strings.EqualFold(other.Name, b.Name) {
			return bookmark.ErrNameTaken
		}
	}
	m.items = append(m.items, b)
	return nil
}

// UpdateBookmark replaces a bookmark.
func (m *memStore) UpdateBookmark(_ context.Context, b bookmark.Bookmark) error {
	if m.failUpdate {
		return errStore
	}
	for i, other := range m.items {
		if other.ID == b.ID {
			m.items[i] = b
			return nil
		}
	}
	return bookmark.ErrNotFound
}

// Bookmark returns one bookmark of a site.
func (m *memStore) Bookmark(_ context.Context, ws, siteID, id uuid.UUID) (bookmark.Bookmark, error) {
	for _, b := range m.items {
		if b.WorkspaceID == ws && b.SiteID == siteID && b.ID == id {
			return b, nil
		}
	}
	return bookmark.Bookmark{}, bookmark.ErrNotFound
}

// Bookmarks lists the bookmarks of a site.
func (m *memStore) Bookmarks(_ context.Context, ws, siteID uuid.UUID) ([]bookmark.Bookmark, error) {
	if m.failList {
		return nil, errStore
	}
	var out []bookmark.Bookmark
	for _, b := range m.items {
		if b.WorkspaceID == ws && b.SiteID == siteID {
			out = append(out, b)
		}
	}
	return out, nil
}

// DeleteBookmark removes a bookmark.
func (m *memStore) DeleteBookmark(_ context.Context, _, _, id uuid.UUID) error {
	if m.failDelete {
		return errStore
	}
	m.items = slices.DeleteFunc(m.items, func(b bookmark.Bookmark) bool { return b.ID == id })
	return nil
}

// sites is a fake site lookup answering from a set of saved site identifiers.
type sites map[uuid.UUID]bool

// Site returns an empty site for a known identifier.
func (s sites) Site(_ context.Context, _, id uuid.UUID) (site.Site, error) {
	if !s[id] {
		return site.Site{}, site.ErrNotFound
	}
	return site.Site{ID: id}, nil
}

// roles is a fake authorizer answering from a table of roles by user, using the real permission matrix.
type roles map[uuid.UUID]workspace.Role

// Authorize applies the permission matrix to the configured roles.
func (r roles) Authorize(_ context.Context, user, _ uuid.UUID, action workspace.Action) (workspace.Role, error) {
	role, ok := r[user]
	if !ok {
		return "", workspace.ErrNotFound
	}
	if !workspace.Can(role, action) {
		return "", workspace.ErrForbidden
	}
	return role, nil
}

// fixture bundles a service with its store, a saved site and actors.
type fixture struct {
	svc    *bookmark.Service
	store  *memStore
	ws     uuid.UUID
	site   uuid.UUID
	editor uuid.UUID
	viewer uuid.UUID
}

// newFixture returns a service where the editor may change bookmarks and the viewer may only list them.
func newFixture(newID func() (uuid.UUID, error)) *fixture {
	f := &fixture{store: &memStore{}, ws: uuid.New(), site: uuid.New(), editor: uuid.New(), viewer: uuid.New()}
	clock := testutil.NewFakeClock(time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC))
	f.svc = bookmark.NewService(f.store, sites{f.site: true}, roles{f.editor: workspace.RoleEditor, f.viewer: workspace.RoleViewer}, clock, newID)
	return f
}

// TestService_Lifecycle verifies adding, listing, changing and removing bookmarks.
//
// Scenarios:
//   - an editor adds a bookmark with a trimmed name and a cleaned path, stamped with the author and the time; a viewer lists it
//   - a name taken on the site, regardless of case, is refused
//   - an editor renames a bookmark and points it at another folder; nil changes keep the fields
//   - an editor removes a bookmark and gets what it was; removing it again is ErrNotFound
func TestService_Lifecycle(t *testing.T) {
	ctx := context.Background()
	f := newFixture(nil)

	created, err := f.svc.Create(ctx, f.editor, f.ws, f.site, "  Logs  ", "/var/log/./nginx/")
	if err != nil || created.Name != "Logs" || created.Path != "/var/log/nginx" || created.CreatedBy != f.editor || !created.CreatedAt.Equal(time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)) || created.ID == uuid.Nil {
		t.Fatalf("Create() = %+v, %v", created, err)
	}
	list, err := f.svc.List(ctx, f.viewer, f.ws, f.site)
	if err != nil || len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("List() = %+v, %v", list, err)
	}
	if _, err := f.svc.Create(ctx, f.editor, f.ws, f.site, "LOGS", "/tmp"); !errors.Is(err, bookmark.ErrNameTaken) {
		t.Errorf("a taken name = %v, want ErrNameTaken", err)
	}

	name, folder := "Web logs", "/srv/www/logs"
	updated, err := f.svc.Update(ctx, f.editor, f.ws, f.site, created.ID, bookmark.Changes{Name: &name, Path: &folder})
	if err != nil || updated.Name != name || updated.Path != folder {
		t.Fatalf("Update() = %+v, %v", updated, err)
	}
	kept, err := f.svc.Update(ctx, f.editor, f.ws, f.site, created.ID, bookmark.Changes{})
	if err != nil || kept != updated {
		t.Errorf("an empty update = %+v, %v, want the bookmark as it was", kept, err)
	}

	removed, err := f.svc.Delete(ctx, f.editor, f.ws, f.site, created.ID)
	if err != nil || removed.Name != name {
		t.Fatalf("Delete() = %+v, %v", removed, err)
	}
	if _, err := f.svc.Delete(ctx, f.editor, f.ws, f.site, created.ID); !errors.Is(err, bookmark.ErrNotFound) {
		t.Errorf("deleting twice = %v, want ErrNotFound", err)
	}
}

// TestService_Validation verifies what a bookmark may be called and where it may point.
//
// Scenarios:
//   - empty, too long and control-character names are ErrInvalidName, on create and on rename
//   - relative, too long, control-character and invalid UTF-8 paths are ErrInvalidPath, on create and on update
//   - a site may have 200 bookmarks, the next is ErrLimit
func TestService_Validation(t *testing.T) {
	ctx := context.Background()
	f := newFixture(nil)
	existing, _ := f.svc.Create(ctx, f.editor, f.ws, f.site, "Home", "/home")

	for _, name := range []string{"", "   ", strings.Repeat("n", 101), "tab\there", "bell\a"} {
		if _, err := f.svc.Create(ctx, f.editor, f.ws, f.site, name, "/x"); !errors.Is(err, bookmark.ErrInvalidName) {
			t.Errorf("Create(name %q) = %v, want ErrInvalidName", name, err)
		}
		if _, err := f.svc.Update(ctx, f.editor, f.ws, f.site, existing.ID, bookmark.Changes{Name: &name}); !errors.Is(err, bookmark.ErrInvalidName) {
			t.Errorf("Update(name %q) = %v, want ErrInvalidName", name, err)
		}
	}
	for _, folder := range []string{"", "relative/dir", "/" + strings.Repeat("p", 4096), "/new\nline", "/bad\xff"} {
		if _, err := f.svc.Create(ctx, f.editor, f.ws, f.site, "Somewhere", folder); !errors.Is(err, bookmark.ErrInvalidPath) {
			t.Errorf("Create(path %q) = %v, want ErrInvalidPath", folder, err)
		}
		if _, err := f.svc.Update(ctx, f.editor, f.ws, f.site, existing.ID, bookmark.Changes{Path: &folder}); !errors.Is(err, bookmark.ErrInvalidPath) {
			t.Errorf("Update(path %q) = %v, want ErrInvalidPath", folder, err)
		}
	}

	for i := 1; i < bookmark.MaxPerSite; i++ {
		if _, err := f.svc.Create(ctx, f.editor, f.ws, f.site, "Folder "+uuid.NewString(), "/data"); err != nil {
			t.Fatalf("bookmark %d = %v", i+1, err)
		}
	}
	if _, err := f.svc.Create(ctx, f.editor, f.ws, f.site, "One more", "/data"); !errors.Is(err, bookmark.ErrLimit) {
		t.Errorf("bookmark %d = %v, want ErrLimit", bookmark.MaxPerSite+1, err)
	}
}

// TestService_Access verifies who may see and change bookmarks, and on which sites.
//
// Scenarios:
//   - a viewer may list but not add, change or remove bookmarks: workspace.ErrForbidden
//   - a stranger gets workspace.ErrNotFound everywhere
//   - a site that is not saved in the workspace, such as a quick connection, is site.ErrNotFound
//   - changing or removing a bookmark of another site is ErrNotFound
func TestService_Access(t *testing.T) {
	ctx := context.Background()
	f := newFixture(nil)
	b, _ := f.svc.Create(ctx, f.editor, f.ws, f.site, "Home", "/home")
	name := "Renamed"
	stranger, elsewhere := uuid.New(), uuid.New()

	if _, err := f.svc.Create(ctx, f.viewer, f.ws, f.site, "Mine", "/mine"); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer Create() = %v", err)
	}
	if _, err := f.svc.Update(ctx, f.viewer, f.ws, f.site, b.ID, bookmark.Changes{Name: &name}); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer Update() = %v", err)
	}
	if _, err := f.svc.Delete(ctx, f.viewer, f.ws, f.site, b.ID); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer Delete() = %v", err)
	}
	if _, err := f.svc.List(ctx, stranger, f.ws, f.site); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger List() = %v", err)
	}
	if _, err := f.svc.List(ctx, f.viewer, f.ws, elsewhere); !errors.Is(err, site.ErrNotFound) {
		t.Errorf("List() of an unsaved site = %v", err)
	}
	f.svc = bookmark.NewService(f.store, sites{f.site: true, elsewhere: true}, roles{f.editor: workspace.RoleEditor}, testutil.NewFakeClock(time.Now()), nil)
	if _, err := f.svc.Update(ctx, f.editor, f.ws, elsewhere, b.ID, bookmark.Changes{Name: &name}); !errors.Is(err, bookmark.ErrNotFound) {
		t.Errorf("Update() through another site = %v", err)
	}
	if _, err := f.svc.Delete(ctx, f.editor, f.ws, elsewhere, b.ID); !errors.Is(err, bookmark.ErrNotFound) {
		t.Errorf("Delete() through another site = %v", err)
	}
}

// TestService_Failures verifies that failures of the store and of the identifier source are reported.
//
// Scenarios:
//   - a failing list fails List and Create, a failing insert fails Create, a failing update fails Update and a failing delete fails Delete
//   - an identifier source that fails makes Create fail
func TestService_Failures(t *testing.T) {
	ctx := context.Background()
	f := newFixture(nil)
	b, _ := f.svc.Create(ctx, f.editor, f.ws, f.site, "Home", "/home")
	name := "Other"

	f.store.failList = true
	if _, err := f.svc.List(ctx, f.editor, f.ws, f.site); !errors.Is(err, errStore) {
		t.Errorf("List() = %v", err)
	}
	if _, err := f.svc.Create(ctx, f.editor, f.ws, f.site, "New", "/new"); !errors.Is(err, errStore) {
		t.Errorf("Create() with a failing list = %v", err)
	}
	f.store.failList, f.store.failCreate = false, true
	if _, err := f.svc.Create(ctx, f.editor, f.ws, f.site, "New", "/new"); !errors.Is(err, errStore) {
		t.Errorf("Create() with a failing insert = %v", err)
	}
	f.store.failUpdate = true
	if _, err := f.svc.Update(ctx, f.editor, f.ws, f.site, b.ID, bookmark.Changes{Name: &name}); !errors.Is(err, errStore) {
		t.Errorf("Update() = %v", err)
	}
	f.store.failDelete = true
	if _, err := f.svc.Delete(ctx, f.editor, f.ws, f.site, b.ID); !errors.Is(err, errStore) {
		t.Errorf("Delete() = %v", err)
	}

	broken := newFixture(func() (uuid.UUID, error) { return uuid.Nil, errStore })
	if _, err := broken.svc.Create(ctx, broken.editor, broken.ws, broken.site, "New", "/new"); !errors.Is(err, errStore) {
		t.Errorf("Create() with a failing identifier source = %v", err)
	}
}
