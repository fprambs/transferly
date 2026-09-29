package backup_test

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/backup"
	"github.com/fprambs/sentinel/internal/core/fzimport"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/testutil"
)

// fakeSites answers with fixed sites, collections and secrets, and can be told to fail.
type fakeSites struct {
	sites       []site.Site
	collections []site.Collection
	secrets     map[site.SecretKind]string
	unlocked    bool
	listErr     error
	collErr     error
	secretErr   error
}

// List returns the sites.
func (f *fakeSites) List(context.Context, uuid.UUID, uuid.UUID) ([]site.Site, error) {
	return f.sites, f.listErr
}

// ListCollections returns the collections.
func (f *fakeSites) ListCollections(context.Context, uuid.UUID, uuid.UUID) ([]site.Collection, error) {
	return f.collections, f.collErr
}

// OpenSecret returns the secret of a kind.
func (f *fakeSites) OpenSecret(_ context.Context, _, _, _ uuid.UUID, _ string, kind site.SecretKind) ([]byte, error) {
	return []byte(f.secrets[kind]), f.secretErr
}

// CanSeal says whether the vault is unlocked.
func (f *fakeSites) CanSeal(string) bool { return f.unlocked }

// fakeImporter records what it was asked to import.
type fakeImporter struct {
	folders [][]string
	entries []fzimport.Entry
	opts    fzimport.Options
}

// ImportEntries records its arguments.
func (f *fakeImporter) ImportEntries(_ context.Context, _, _ uuid.UUID, _ string, folders [][]string, entries []fzimport.Entry, _ []fzimport.Skipped, opts fzimport.Options) (fzimport.Result, error) {
	f.folders, f.entries, f.opts = folders, entries, opts
	return fzimport.Result{Items: []fzimport.Item{{Status: fzimport.StatusCreated}}}, nil
}

// allow is an authorizer that answers with a fixed error.
type allow struct{ err error }

// Authorize returns the configured error.
func (a allow) Authorize(context.Context, uuid.UUID, uuid.UUID, workspace.Action) (workspace.Role, error) {
	return workspace.RoleOwner, a.err
}

// fixture is a service over fakes: a workspace with a nested collection, an empty one, a keyed site behind a proxy and a jump host
// in the nested collection, and an interactive site at the top level.
type fixture struct {
	svc      *backup.Service
	sites    *fakeSites
	importer *fakeImporter
	sealer   *backup.Sealer
}

// newFixture builds the fixture, authorizing with authz.
func newFixture(authz allow) fixture {
	work, deep, empty := uuid.New(), uuid.New(), uuid.New()
	sites := &fakeSites{
		unlocked: true,
		collections: []site.Collection{
			{ID: deep, ParentID: work, Name: "Deep"}, {ID: work, Name: "Work"}, {ID: empty, Name: "Empty"},
		},
		sites: []site.Site{
			{
				ID: uuid.New(), CollectionID: deep, Name: "Keyed", Protocol: site.SFTP, Host: "k.example.com", Port: 22, Username: "deploy",
				AuthMethod: site.AuthKey, ListTimezone: "UTC", Charset: "utf-8", SpeedLimit: 1024, RemoteDir: "/srv",
				Proxy:   site.Proxy{Kind: site.ProxySOCKS5, Host: "p", Port: 1080, Username: "pu"},
				Jump:    site.Jump{Host: "j", Port: 22, Username: "ju", AuthMethod: site.AuthPassword},
				Secrets: []site.SecretKind{site.SecretPrivateKey, site.SecretPassphrase, site.SecretJumpPassword, site.SecretProxyPassword},
			},
			{ID: uuid.New(), Name: "Asks", Protocol: site.FTP, FTPEncryption: site.EncryptionExplicit, Host: "f.example.com", Port: 21, Username: "u", AuthMethod: site.AuthInteractive, ListTimezone: "UTC", Charset: "utf-8"},
		},
		secrets: map[site.SecretKind]string{site.SecretPrivateKey: "KEY", site.SecretPassphrase: "phrase", site.SecretJumpPassword: "jump-pw", site.SecretProxyPassword: "proxy-pw"},
	}
	importer := &fakeImporter{}
	sealer := newSealer(rand.Reader)
	clock := testutil.NewFakeClock(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))
	return fixture{svc: backup.NewService(authz, sites, importer, sealer, clock), sites: sites, importer: importer, sealer: sealer}
}

// TestService_Export covers exporting the sites of a workspace (ADR-0017).
//
// Use cases:
//   - every site is written with its settings, proxy, jump host, the path of names of its collection and its secrets in clear text
//     inside the seal, and the count is returned
//   - every collection is written as its path, parents first, the empty ones too
//   - a site without secrets, such as an interactive one, is written without them
func TestService_Export(t *testing.T) {
	f := newFixture(allow{})
	file, count, err := f.svc.Export(t.Context(), uuid.New(), uuid.New(), "s", password)
	if err != nil || count != 2 {
		t.Fatalf("Export() = %d sites, %v", count, err)
	}
	content, err := f.sealer.Open(t.Context(), password, file)
	if err != nil {
		t.Fatal(err)
	}
	if !content.ExportedAt.Equal(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)) || len(content.Collections) != 3 || len(content.Collections[2]) != 2 {
		t.Errorf("collections = %v at %v", content.Collections, content.ExportedAt)
	}
	if !slices.ContainsFunc(content.Collections, func(p []string) bool { return slices.Equal(p, []string{"Empty"}) }) {
		t.Errorf("the empty collection is missing: %v", content.Collections)
	}
	keyed := content.Sites[0]
	if !slices.Equal(keyed.Path, []string{"Work", "Deep"}) || keyed.Secrets["private_key"] != "KEY" || keyed.Secrets["proxy_password"] != "proxy-pw" ||
		keyed.Proxy == nil || keyed.Proxy.Username != "pu" || keyed.Jump == nil || keyed.Jump.AuthMethod != "password" || keyed.SpeedLimit != 1024 || keyed.RemoteDir != "/srv" {
		t.Errorf("keyed = %+v", keyed)
	}
	if asks := content.Sites[1]; len(asks.Path) != 0 || len(asks.Secrets) != 0 || asks.Proxy != nil || asks.Jump != nil || asks.FTPEncryption != "explicit" {
		t.Errorf("interactive = %+v", asks)
	}
}

// TestService_Export_Refusals covers the exports that are refused (ADR-0017).
//
// Use cases:
//   - the actor must manage the sites, and a weak password is refused before any secret is opened
//   - sites with secrets need the vault unlocked, while a workspace with none exports with it locked
//   - failures to read the sites, the collections or a secret stop the export with their error
func TestService_Export_Refusals(t *testing.T) {
	ctx := t.Context()
	refused := errors.New("refused")
	if _, _, err := newFixture(allow{err: refused}).svc.Export(ctx, uuid.New(), uuid.New(), "s", password); !errors.Is(err, refused) {
		t.Errorf("unauthorized = %v", err)
	}
	f := newFixture(allow{})
	if _, _, err := f.svc.Export(ctx, uuid.New(), uuid.New(), "s", "short"); !errors.Is(err, backup.ErrWeakPassword) {
		t.Errorf("weak password = %v", err)
	}
	f.sites.unlocked = false
	if _, _, err := f.svc.Export(ctx, uuid.New(), uuid.New(), "s", password); !errors.Is(err, site.ErrLocked) {
		t.Errorf("locked = %v", err)
	}
	f.sites.sites = f.sites.sites[1:]
	if _, count, err := f.svc.Export(ctx, uuid.New(), uuid.New(), "s", password); err != nil || count != 1 {
		t.Errorf("locked with no secrets = %d, %v", count, err)
	}

	broken := errors.New("broken")
	for name, breakIt := range map[string]func(*fakeSites){
		"sites":       func(s *fakeSites) { s.listErr = broken },
		"collections": func(s *fakeSites) { s.collErr = broken },
		"secret":      func(s *fakeSites) { s.secretErr = broken },
	} {
		f := newFixture(allow{})
		breakIt(f.sites)
		if _, _, err := f.svc.Export(ctx, uuid.New(), uuid.New(), "s", password); !errors.Is(err, broken) {
			t.Errorf("%s: Export() = %v", name, err)
		}
	}
}

// TestService_Import covers importing a Sentinel file (ADR-0017).
//
// Use cases:
//   - an exported file imports every collection and every site with its settings and secrets, with the options given
//   - the actor must manage the sites, which is checked before anything is derived
//   - a wrong password is refused, and a file with a secret this version does not know is ErrInvalidFile
func TestService_Import(t *testing.T) {
	ctx := t.Context()
	f := newFixture(allow{})
	file, _, err := f.svc.Export(ctx, uuid.New(), uuid.New(), "s", password)
	if err != nil {
		t.Fatal(err)
	}
	opts := fzimport.Options{Passwords: true, OnConflict: fzimport.RenameSite}
	result, err := f.svc.Import(ctx, uuid.New(), uuid.New(), "s", password, file, opts)
	if err != nil || result.Created() != 1 || f.importer.opts != opts || len(f.importer.folders) != 3 || len(f.importer.entries) != 2 {
		t.Fatalf("Import() = %+v, %v; importer = %+v", result, err, f.importer)
	}
	keyed := f.importer.entries[0]
	in := keyed.Input
	if in.Name != "Keyed" || in.AuthMethod != site.AuthKey || in.Proxy.Kind != site.ProxySOCKS5 || in.Jump.Username != "ju" || in.SpeedLimit != 1024 ||
		*keyed.Secrets.PrivateKey != "KEY" || *keyed.Secrets.JumpPassword != "jump-pw" || keyed.Secrets.Password != nil || !slices.Equal(keyed.Path, []string{"Work", "Deep"}) {
		t.Errorf("keyed entry = %+v", keyed)
	}
	if asks := f.importer.entries[1]; asks.Input.Proxy.Set() || asks.Input.Jump.Set() || asks.Input.FTPEncryption != site.EncryptionExplicit {
		t.Errorf("interactive entry = %+v", asks)
	}

	refused := errors.New("refused")
	if _, err := newFixture(allow{err: refused}).svc.Import(ctx, uuid.New(), uuid.New(), "s", password, file, opts); !errors.Is(err, refused) {
		t.Errorf("unauthorized = %v", err)
	}
	if _, err := f.svc.Import(ctx, uuid.New(), uuid.New(), "s", "not the password at all", file, opts); !errors.Is(err, backup.ErrWrongPassword) {
		t.Errorf("wrong password = %v", err)
	}
	unknown, err := f.sealer.Seal(ctx, password, backup.Content{Sites: []backup.Site{{Name: "Odd", Secrets: map[string]string{"token": "x"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Import(ctx, uuid.New(), uuid.New(), "s", password, unknown, opts); !errors.Is(err, backup.ErrInvalidFile) {
		t.Errorf("unknown secret = %v", err)
	}
}
