package fzimport_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/fzimport"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/workspace"
)

// fakeSites keeps sites and collections in memory, settles them with the real rules, and can be told to fail.
type fakeSites struct {
	*site.Service
	sites       []site.Site
	collections []site.Collection
	passwords   map[string]string
	accounts    map[string]string
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
	created := site.Site{ID: uuid.New(), WorkspaceID: ws, Name: in.Name, Connector: in.Connector, Config: in.Config, CollectionID: in.CollectionID}
	f.sites = append(f.sites, created)
	if password, ok := changes[site.SecretPassword]; ok {
		f.passwords[in.Name] = password
	}
	if account, ok := changes[site.SecretAccount]; ok {
		f.accounts[in.Name] = account
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
	f := &fakeSites{Service: settler(), passwords: map[string]string{}, accounts: map[string]string{}, unlocked: true, createErr: map[string]error{}}
	return fzimport.NewService(allow{}, f), f
}

// settler returns a site service that only settles sites, by the schemas of the built-in connectors.
func settler() *site.Service {
	return site.NewService(nil, nil, nil, nil, connectors.Builtin(), nil, nil, nil)
}

// server builds a server of a test with a connector and settings, given as name and value pairs.
func server(path []string, name, connector string, settings ...string) fzimport.Server {
	config := map[string]string{}
	for i := 0; i+1 < len(settings); i += 2 {
		config[settings[i]] = settings[i+1]
	}
	return fzimport.Server{Path: path, Name: name, Connector: connector, Config: config}
}

// servers builds the servers of the tests.
func servers() []fzimport.Server {
	one := server([]string{"Work", "Deep"}, "One", "sftp", "host", "one.example.com", "username", "u")
	one.Password = "pw1"
	two := server([]string{"Work"}, "Two", "ftp", "host", "two.example.com", "username", "u")
	two.PasswordSkipped = true
	three := server(nil, "Three", "ftp", "host", "three.example.com", "username", "u", "encryption", "explicit", "auth", "account")
	three.Account = "billing"
	return []fzimport.Server{one, two, three}
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
//   - sites are created with their folders, which are created once and reused, and their passwords and accounts when asked for
//   - a dry run reports what would happen and writes nothing, including no folders
//   - names that are taken are skipped by default and renamed to a free "Name (n)" when asked, and the same name twice in a file is handled the same way
//   - folders that exist are reused, and everything can go into a chosen collection
//   - passwords and accounts need an unlocked vault, checked before anything is written, unless no site has one or it is a dry run
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
	if f.accounts["Three"] != "billing" || len(f.accounts) != 1 {
		t.Errorf("accounts = %v; want the account of the site of the account logon type", f.accounts)
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
	takenFake.sites = []site.Site{{Name: "one"}, {Name: "three"}, {Name: "Three (1)"}}
	extra := []fzimport.Server{server(nil, "Two", "ftp", "host", "dup.example.com", "username", "u"), server(nil, "Three", "ftp", "host", "again.example.com", "username", "u")}
	first, _ := taken.Import(ctx, actor, ws, "s", append(servers(), extra...), nil, fzimport.Options{})
	statuses := []string{}
	for _, item := range first.Items {
		statuses = append(statuses, item.Name+":"+string(item.Status))
	}
	if strings.Join(statuses, ",") != "One:created,Two:created,Three:skipped,Two:created,Three:skipped" {
		t.Errorf("names taken in the same folder only = %v", statuses)
	}
	renamed, renamedFake := newImport()
	renamedFake.sites = []site.Site{{Name: "one"}, {Name: "Three"}, {Name: "Three (1)"}}
	second, _ := renamed.Import(ctx, actor, ws, "s", append(servers(), extra...), nil, fzimport.Options{OnConflict: fzimport.RenameSite})
	names := []string{}
	for _, item := range second.Items {
		names = append(names, item.Name)
	}
	if strings.Join(names, ",") != "One,Two,Three (2),Two,Three (3)" {
		t.Errorf("renamed names = %v", names)
	}

	planned, plannedFake := newImport()
	work := uuid.New()
	plannedFake.collections = []site.Collection{{ID: work, Name: "Work"}}
	plannedFake.sites = []site.Site{{Name: "Two", CollectionID: work}}
	twins := []fzimport.Server{server([]string{"New"}, "Same", "ftp", "host", "a.example.com", "username", "u"), server([]string{"New"}, "Same", "ftp", "host", "b.example.com", "username", "u")}
	plan, _ := planned.Import(ctx, actor, ws, "s", append(servers(), twins...), nil, fzimport.Options{DryRun: true, OnConflict: fzimport.RenameSite})
	names = names[:0]
	for _, item := range plan.Items {
		names = append(names, item.Name)
	}
	if strings.Join(names, ",") != "One,Two (1),Three,Same,Same (1)" || len(plannedFake.collections) != 1 {
		t.Errorf("dry run names = %v with %d folders; want names per folder and nothing created", names, len(plannedFake.collections))
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
	if _, err := locked.Import(ctx, actor, ws, "s", servers()[1:2], nil, fzimport.Options{Passwords: true}); err != nil {
		t.Errorf("Import() of sites without secrets with a locked vault = %v", err)
	}
	if _, err := locked.Import(ctx, actor, ws, "s", servers()[2:], nil, fzimport.Options{Passwords: true}); !errors.Is(err, site.ErrLocked) {
		t.Errorf("Import() of a site with only an account with a locked vault = %v; want ErrLocked", err)
	}

	failing, failingFake := newImport()
	failingFake.createErr["Two"] = netguard.ErrBlocked
	bad := append(servers(), server(nil, "Bad host", "ftp", "host", "not a host", "username", "u"))
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

	forbidden := fzimport.NewService(allow{err: workspace.ErrForbidden}, &fakeSites{Service: settler()})
	if _, err := forbidden.Import(ctx, actor, ws, "s", servers(), nil, fzimport.Options{}); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("Import() by a viewer = %v; want ErrForbidden", err)
	}
	_ = fmt.Sprint
}

// TestResult_Created verifies counting the sites that were created.
//
// Scenarios:
//   - only the created items count, not the ones skipped, failed or that would be created
func TestResult_Created(t *testing.T) {
	result := fzimport.Result{Items: []fzimport.Item{
		{Status: fzimport.StatusCreated}, {Status: fzimport.StatusSkipped}, {Status: fzimport.StatusCreated}, {Status: fzimport.StatusFailed}, {Status: fzimport.StatusWouldCreate},
	}}
	if got := result.Created(); got != 2 {
		t.Errorf("Created() = %d; want 2", got)
	}
}

// TestService_ImportEntries covers importing entries of any file with their folders (ADR-0017).
//
// Use cases:
//   - every folder given is created, the empty ones too, and the entries go into the folders of their paths
//   - every secret of an entry is stored when asked for, and none otherwise, which is reported as skipped
//   - a dry run creates no folder
//   - a folder that fails for a reason of its own, such as nesting too deep, is left out; an unexpected failure stops the import
func TestService_ImportEntries(t *testing.T) {
	ctx := context.Background()
	ws, actor := uuid.New(), uuid.New()
	key, phrase := "KEY", "phrase"
	entries := []fzimport.Entry{{
		Path: []string{"Work"}, Input: site.Input{Name: "Keyed", Connector: "sftp", Config: map[string]string{"host": "k.example.com", "username": "u", "auth": "key"}},
		Secrets: site.SecretChanges{site.SecretPrivateKey: key, site.SecretPassphrase: phrase},
	}}
	folders := [][]string{{"Work"}, {"Empty", "Inner"}}

	svc, fake := newImport()
	out, err := svc.ImportEntries(ctx, actor, ws, "s", folders, entries, nil, fzimport.Options{Passwords: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.collections) != 3 || len(fake.sites) != 1 || fake.sites[0].CollectionID != fake.collections[0].ID {
		t.Errorf("collections = %+v, sites = %+v", fake.collections, fake.sites)
	}
	if item := out.Items[0]; item.Status != fzimport.StatusCreated || !item.PasswordStored || item.PasswordSkipped {
		t.Errorf("item = %+v", item)
	}

	without, _ := newImport()
	out, err = without.ImportEntries(ctx, actor, ws, "s", nil, entries, nil, fzimport.Options{})
	if item := out.Items[0]; err != nil || item.PasswordStored || !item.PasswordSkipped {
		t.Errorf("item without secrets = %+v, %v", item, err)
	}

	dry, dryFake := newImport()
	if _, err := dry.ImportEntries(ctx, actor, ws, "s", folders, entries, nil, fzimport.Options{DryRun: true}); err != nil || len(dryFake.collections) != 0 {
		t.Errorf("dry run = %v with %d folders", err, len(dryFake.collections))
	}

	deep, deepFake := newImport()
	deepFake.collErr = site.ErrCollectionTooDeep
	if _, err := deep.ImportEntries(ctx, actor, ws, "s", folders, nil, nil, fzimport.Options{}); err != nil {
		t.Errorf("folder too deep = %v; want it left out", err)
	}
	broken, brokenFake := newImport()
	brokenFake.collErr = errors.New("database down")
	if _, err := broken.ImportEntries(ctx, actor, ws, "s", folders, nil, nil, fzimport.Options{}); err == nil {
		t.Error("an unexpected folder failure did not stop the import")
	}
}
