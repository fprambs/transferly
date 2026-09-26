package fzimport_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/fzimport"
	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// fakeSites keeps sites and collections in memory and can be told to fail.
type fakeSites struct {
	sites       []site.Site
	collections []site.Collection
	passwords   map[string]string
	unlocked    bool
	createErr   map[string]error
	collErr     error
	listErr     error
	collListErr error
}

// Create stores a site after validating it like the real service does.
func (f *fakeSites) Create(_ context.Context, _, ws uuid.UUID, _ string, in site.Input, changes site.SecretChanges) (site.Site, error) {
	if err := f.createErr[in.Name]; err != nil {
		return site.Site{}, err
	}
	created := site.Site{ID: uuid.New(), WorkspaceID: ws, Name: in.Name, Host: in.Host, Port: in.Port, Protocol: in.Protocol, Username: in.Username, CollectionID: in.CollectionID}
	f.sites = append(f.sites, created)
	if changes.Password != nil {
		f.passwords[in.Name] = *changes.Password
	}
	return created, nil
}

// List returns the sites.
func (f *fakeSites) List(context.Context, uuid.UUID, uuid.UUID) ([]site.Site, error) {
	return f.sites, f.listErr
}

// CreateCollection stores a collection.
func (f *fakeSites) CreateCollection(_ context.Context, _, ws uuid.UUID, name string, parent uuid.UUID) (site.Collection, error) {
	if f.collErr != nil {
		return site.Collection{}, f.collErr
	}
	c := site.Collection{ID: uuid.New(), WorkspaceID: ws, ParentID: parent, Name: name}
	f.collections = append(f.collections, c)
	return c, nil
}

// ListCollections returns the collections.
func (f *fakeSites) ListCollections(context.Context, uuid.UUID, uuid.UUID) ([]site.Collection, error) {
	return f.collections, f.collListErr
}

// CanSeal says whether the vault is unlocked.
func (f *fakeSites) CanSeal(string) bool { return f.unlocked }

// allow is an authorizer that answers with a fixed error.
type allow struct{ err error }

// Authorize returns the configured error.
func (a allow) Authorize(context.Context, uuid.UUID, uuid.UUID, workspace.Action) (workspace.Role, error) {
	return workspace.RoleOwner, a.err
}

// newImport returns a service over fake sites.
func newImport() (*fzimport.Service, *fakeSites) {
	f := &fakeSites{passwords: map[string]string{}, unlocked: true, createErr: map[string]error{}}
	return fzimport.NewService(allow{}, f), f
}

// servers builds the servers of the tests.
func servers() []fzimport.Server {
	return []fzimport.Server{
		{Path: []string{"Work", "Deep"}, Name: "One", Protocol: site.SFTP, Host: "one.example.com", Username: "u", Password: "pw1"},
		{Path: []string{"Work"}, Name: "Two", Protocol: site.FTP, Host: "two.example.com", Username: "u", PasswordSkipped: true},
		{Name: "Three", Protocol: site.FTPS, Host: "three.example.com", Username: "u"},
	}
}

// byName indexes the items of a result.
func byName(r fzimport.Result) map[string]fzimport.Item {
	out := map[string]fzimport.Item{}
	for _, item := range r.Items {
		out[item.Name] = item
	}
	return out
}

// TestService_Import verifies creating the sites of a file.
//
// Scenarios:
//   - sites are created with their folders, which are created once and reused, and their passwords when asked for
//   - a dry run reports what would happen and writes nothing, including no folders
//   - names that are taken are skipped by default and renamed to a free "Name (n)" when asked, and the same name twice in a file is handled the same way
//   - folders that exist are reused, and everything can go into a chosen collection
//   - passwords need an unlocked vault, checked before anything is written, unless no site has one or it is a dry run
//   - values that fail validation, blocked destinations, name races and folders too deep fail their site only, and the others are still created
//   - sites the file could not represent are listed as skipped
//   - the actor must manage the sites, and unexpected failures stop the import with their error
func TestService_Import(t *testing.T) {
	ctx := context.Background()
	ws, actor := uuid.New(), uuid.New()

	svc, f := newImport()
	result, err := svc.Import(ctx, actor, ws, "s", servers(), []fzimport.Skipped{{Path: []string{"X"}, Name: "Web", Reason: "unsupported_protocol"}}, fzimport.Options{Passwords: true})
	if err != nil {
		t.Fatal(err)
	}
	items := byName(result)
	if len(f.sites) != 3 || len(f.collections) != 2 || f.passwords["One"] != "pw1" || len(f.passwords) != 1 {
		t.Errorf("created %d sites, %d collections, passwords %v", len(f.sites), len(f.collections), f.passwords)
	}
	if items["One"].Status != fzimport.StatusCreated || !items["One"].PasswordStored || items["One"].Path != "Work/Deep" || items["Two"].PasswordSkipped != true {
		t.Errorf("items = %+v", items)
	}
	if web := items["Web"]; web.Status != fzimport.StatusSkipped || web.Reason != "unsupported_protocol" || web.Path != "X" || result.Items[len(result.Items)-1].Name != "Web" {
		t.Errorf("skipped item = %+v", web)
	}
	if f.sites[0].CollectionID == uuid.Nil || f.sites[2].CollectionID != uuid.Nil || f.sites[1].CollectionID == f.sites[0].CollectionID {
		t.Errorf("sites are in the wrong collections: %+v", f.sites)
	}

	dry, dryFake := newImport()
	dryFake.unlocked = false
	preview, err := dry.Import(ctx, actor, ws, "s", servers(), nil, fzimport.Options{DryRun: true, Passwords: true})
	if err != nil || len(dryFake.sites) != 0 || len(dryFake.collections) != 0 {
		t.Fatalf("dry run wrote %d sites, %d folders, %v", len(dryFake.sites), len(dryFake.collections), err)
	}
	if got := byName(preview)["One"]; got.Status != fzimport.StatusWouldCreate || !got.PasswordStored {
		t.Errorf("preview = %+v", got)
	}
	noPasswords, _ := newImport()
	preview, _ = noPasswords.Import(ctx, actor, ws, "s", servers(), nil, fzimport.Options{DryRun: true})
	if got := byName(preview)["One"]; got.PasswordStored || !got.PasswordSkipped {
		t.Errorf("preview without passwords = %+v", got)
	}

	taken, takenFake := newImport()
	takenFake.sites = []site.Site{{Name: "one"}, {Name: "Three"}, {Name: "Three (2)"}}
	first, _ := taken.Import(ctx, actor, ws, "s", append(servers(), fzimport.Server{Name: "Two", Protocol: site.FTP, Host: "dup.example.com", Username: "u"}), nil, fzimport.Options{})
	items = byName(first)
	if items["One"].Status != fzimport.StatusSkipped || items["One"].Reason != "name_taken" {
		t.Errorf("taken name = %+v", items["One"])
	}
	renamed, renamedFake := newImport()
	renamedFake.sites = []site.Site{{Name: "one"}, {Name: "Three"}, {Name: "Three (2)"}}
	second, _ := renamed.Import(ctx, actor, ws, "s", append(servers(), fzimport.Server{Name: "Two", Protocol: site.FTP, Host: "dup.example.com", Username: "u"}), nil, fzimport.Options{OnConflict: fzimport.RenameSite})
	names := []string{}
	for _, item := range second.Items {
		names = append(names, item.Name)
	}
	if strings.Join(names, ",") != "One (2),Two,Three (3),Two (2)" {
		t.Errorf("renamed names = %v", names)
	}

	reuse, reuseFake := newImport()
	parent := uuid.New()
	reuseFake.collections = []site.Collection{{ID: parent, Name: "Import"}, {ID: uuid.New(), ParentID: parent, Name: "work"}}
	if _, err := reuse.Import(ctx, actor, ws, "s", servers()[:2], nil, fzimport.Options{Into: parent}); err != nil {
		t.Fatal(err)
	}
	if len(reuseFake.collections) != 3 || reuseFake.sites[0].CollectionID == uuid.Nil || reuseFake.sites[1].CollectionID != reuseFake.collections[1].ID {
		t.Errorf("collections = %+v, sites = %+v", reuseFake.collections, reuseFake.sites)
	}

	locked, lockedFake := newImport()
	lockedFake.unlocked = false
	if _, err := locked.Import(ctx, actor, ws, "s", servers(), nil, fzimport.Options{Passwords: true}); !errors.Is(err, site.ErrLocked) || len(lockedFake.sites) != 0 || len(lockedFake.collections) != 0 {
		t.Errorf("Import() with a locked vault = %v, %d sites, %d folders", err, len(lockedFake.sites), len(lockedFake.collections))
	}
	if _, err := locked.Import(ctx, actor, ws, "s", servers()[1:], nil, fzimport.Options{Passwords: true}); err != nil {
		t.Errorf("Import() of sites without passwords with a locked vault = %v", err)
	}

	failing, failingFake := newImport()
	failingFake.createErr["Two"] = netguard.ErrBlocked
	bad := append(servers(), fzimport.Server{Name: "Bad host", Protocol: site.FTP, Host: "not a host", Username: "u"})
	out, err := failing.Import(ctx, actor, ws, "s", bad, nil, fzimport.Options{})
	if err != nil {
		t.Fatal(err)
	}
	items = byName(out)
	if items["Two"].Status != fzimport.StatusFailed || items["Two"].Reason != "destination_blocked" || items["Bad host"].Reason != "invalid_host" || items["One"].Status != fzimport.StatusCreated || items["Three"].Status != fzimport.StatusCreated {
		t.Errorf("items = %+v", items)
	}

	deep, deepFake := newImport()
	deepFake.collErr = site.ErrCollectionTooDeep
	out, err = deep.Import(ctx, actor, ws, "s", servers()[:1], nil, fzimport.Options{})
	if err != nil || byName(out)["One"].Reason != "collection_too_deep" {
		t.Errorf("too deep = %+v, %v", out, err)
	}

	unexpected := errors.New("database is down")
	for name, mutate := range map[string]func(*fakeSites){
		"collections":     func(f *fakeSites) { f.collErr = unexpected },
		"create":          func(f *fakeSites) { f.createErr["Three"] = unexpected },
		"list":            func(f *fakeSites) { f.listErr = unexpected },
		"collection list": func(f *fakeSites) { f.collListErr = unexpected },
	} {
		s, fake := newImport()
		mutate(fake)
		if _, err := s.Import(ctx, actor, ws, "s", servers(), nil, fzimport.Options{}); !errors.Is(err, unexpected) {
			t.Errorf("%s failure = %v; want the error", name, err)
		}
	}

	forbidden := fzimport.NewService(allow{err: workspace.ErrForbidden}, &fakeSites{})
	if _, err := forbidden.Import(ctx, actor, ws, "s", servers(), nil, fzimport.Options{}); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("Import() by a viewer = %v; want ErrForbidden", err)
	}
	_ = fmt.Sprint
}
