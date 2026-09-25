package site_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/testutil"
)

// errStore is the failure injected by the fakes.
var errStore = errors.New("store failure")

// memStore is an in-memory site.Store with switchable failures.
type memStore struct {
	sites       map[uuid.UUID]site.Site
	secrets     map[string]site.Sealed
	collections map[uuid.UUID]site.Collection
	failCreate  error
	failUpdate  error
	failGet     error
	failList    error
	failCreateC error
	failUpdateC error
	failDeleteC error
	failGetC    error
}

// CreateSite stores the site and its secrets.
func (m *memStore) CreateSite(_ context.Context, s site.Site, secrets []site.Sealed) error {
	if m.failCreate != nil {
		return m.failCreate
	}
	m.sites[s.ID] = s
	for _, sealed := range secrets {
		m.secrets[s.ID.String()+string(sealed.Kind)] = sealed
	}
	return nil
}

// UpdateSite replaces the site and applies the secret writes.
func (m *memStore) UpdateSite(_ context.Context, s site.Site, secrets []site.Sealed, remove []site.SecretKind) error {
	if m.failUpdate != nil {
		return m.failUpdate
	}
	m.sites[s.ID] = s
	for _, sealed := range secrets {
		m.secrets[s.ID.String()+string(sealed.Kind)] = sealed
	}
	for _, kind := range remove {
		delete(m.secrets, s.ID.String()+string(kind))
	}
	return nil
}

// Site returns one site.
func (m *memStore) Site(_ context.Context, ws, id uuid.UUID) (site.Site, error) {
	if m.failGet != nil {
		return site.Site{}, m.failGet
	}
	s, ok := m.sites[id]
	if !ok || s.WorkspaceID != ws {
		return site.Site{}, site.ErrNotFound
	}
	return s, nil
}

// Sites lists the sites of a workspace.
func (m *memStore) Sites(_ context.Context, ws uuid.UUID) ([]site.Site, error) {
	var out []site.Site
	for _, s := range m.sites {
		if s.WorkspaceID == ws {
			out = append(out, s)
		}
	}
	return out, nil
}

// DeleteSite removes a site.
func (m *memStore) DeleteSite(_ context.Context, ws, id uuid.UUID) error {
	if s, ok := m.sites[id]; !ok || s.WorkspaceID != ws {
		return site.ErrNotFound
	}
	delete(m.sites, id)
	return nil
}

// Secret returns one sealed secret.
func (m *memStore) Secret(_ context.Context, _, id uuid.UUID, kind site.SecretKind) (site.Sealed, error) {
	sealed, ok := m.secrets[id.String()+string(kind)]
	if !ok {
		return site.Sealed{}, site.ErrNoSecret
	}
	return sealed, nil
}

// CreateCollection stores a collection unless its name is taken among its siblings.
func (m *memStore) CreateCollection(_ context.Context, c site.Collection) error {
	if m.failCreateC != nil {
		return m.failCreateC
	}
	for _, other := range m.collections {
		if other.WorkspaceID == c.WorkspaceID && other.ParentID == c.ParentID && strings.EqualFold(other.Name, c.Name) {
			return site.ErrCollectionNameTaken
		}
	}
	m.collections[c.ID] = c
	return nil
}

// UpdateCollection replaces a collection.
func (m *memStore) UpdateCollection(_ context.Context, c site.Collection) error {
	if m.failUpdateC != nil {
		return m.failUpdateC
	}
	m.collections[c.ID] = c
	return nil
}

// Collection returns one collection.
func (m *memStore) Collection(_ context.Context, ws, id uuid.UUID) (site.Collection, error) {
	if m.failGetC != nil {
		return site.Collection{}, m.failGetC
	}
	c, ok := m.collections[id]
	if !ok || c.WorkspaceID != ws {
		return site.Collection{}, site.ErrCollectionNotFound
	}
	return c, nil
}

// Collections lists the collections of a workspace.
func (m *memStore) Collections(_ context.Context, ws uuid.UUID) ([]site.Collection, error) {
	if m.failList != nil {
		return nil, m.failList
	}
	var out []site.Collection
	for _, c := range m.collections {
		if c.WorkspaceID == ws {
			out = append(out, c)
		}
	}
	return out, nil
}

// DeleteCollection removes an empty collection.
func (m *memStore) DeleteCollection(_ context.Context, ws, id uuid.UUID) error {
	if m.failDeleteC != nil {
		return m.failDeleteC
	}
	if c, ok := m.collections[id]; !ok || c.WorkspaceID != ws {
		return site.ErrCollectionNotFound
	}
	for _, c := range m.collections {
		if c.ParentID == id {
			return site.ErrCollectionNotEmpty
		}
	}
	for _, s := range m.sites {
		if s.CollectionID == id {
			return site.ErrCollectionNotEmpty
		}
	}
	delete(m.collections, id)
	return nil
}

// roles is a fake authorizer answering from a table of roles by user.
type roles map[uuid.UUID]workspace.Role

// Authorize applies the real permission matrix to the configured roles.
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

// keys is a fake key provider with one unlocked session.
type keys struct {
	dek     []byte
	session string
	failure error
}

// Key returns the key of the unlocked session.
func (k keys) Key(session string) ([]byte, error) {
	if k.failure != nil {
		return nil, k.failure
	}
	if session != k.session {
		return nil, vault.ErrLocked
	}
	return append([]byte(nil), k.dek...), nil
}

// fixture bundles a service with its fakes and actors.
type fixture struct {
	svc    *site.Service
	store  *memStore
	ws     uuid.UUID
	editor uuid.UUID
	viewer uuid.UUID
	guard  *netguard.Policy
}

// newFixture returns a service where the editor may manage sites and the viewer may only browse.
func newFixture(t *testing.T, random *switchable) *fixture {
	t.Helper()
	dek := make([]byte, vault.KeyLength)
	if _, err := rand.Read(dek); err != nil {
		t.Fatal(err)
	}
	f := &fixture{store: &memStore{sites: map[uuid.UUID]site.Site{}, secrets: map[string]site.Sealed{}, collections: map[uuid.UUID]site.Collection{}}, ws: uuid.New(), editor: uuid.New(), viewer: uuid.New(), guard: netguard.NewPolicy(nil, resolver{})}
	authz := roles{f.editor: workspace.RoleEditor, f.viewer: workspace.RoleViewer}
	clock := testutil.NewFakeClock(time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC))
	f.svc = site.NewService(f.store, authz, keys{dek: dek, session: "open"}, f.guard, clock, random, nil)
	return f
}

// resolver resolves public.example to a public address and everything else to a private one.
type resolver struct{}

// LookupNetIP implements netguard.Resolver.
func (resolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if host == "public.example" {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	if host == "private.example" {
		return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
	}
	return nil, errors.New("no such host")
}

// switchable is a random source that can be made to fail.
type switchable struct{ broken bool }

// Read fills p from the system source, or fails once broken.
func (s *switchable) Read(p []byte) (int, error) {
	if s.broken {
		return 0, errStore
	}
	return rand.Read(p)
}

// valid returns input that passes validation.
func valid() site.Input {
	return site.Input{Name: "Production", Protocol: site.SFTP, Host: "public.example", Username: "deploy"}
}

// text returns a pointer to a string, for secret changes.
func text(s string) *string { return &s }

// TestService_CreateValidation verifies the rules on site settings.
//
// Scenarios:
//   - valid input is stored with defaults: port by protocol, password authentication, UTC listings, trimmed fields
//   - empty or oversized names, unknown protocols, malformed hosts, ports out of range, empty user names,
//     unknown or protocol-incompatible authentication methods and unknown time zones are each refused with their error
//   - key authentication is accepted for SFTP only
//   - hosts that resolve to private addresses are refused with ErrBlocked, unresolvable ones with ErrUnresolvable
func TestService_CreateValidation(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})

	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", site.Input{Name: "  Prod  ", Protocol: site.FTPS, Host: " public.example ", Username: " me "}, site.SecretChanges{})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if created.Name != "Prod" || created.Host != "public.example" || created.Username != "me" || created.Port != 21 ||
		created.AuthMethod != site.AuthPassword || created.ListTimezone != "UTC" || created.CreatedAt.Location() != time.UTC || created.ID == uuid.Nil {
		t.Errorf("Create() = %+v", created)
	}
	if sftp, err := f.svc.Create(ctx, f.editor, f.ws, "open", func() site.Input { in := valid(); in.Name = "Key"; in.AuthMethod = site.AuthKey; return in }(), site.SecretChanges{}); err != nil || sftp.Port != 22 {
		t.Errorf("SFTP with key = %+v, %v", sftp, err)
	}

	tests := []struct {
		name   string
		mutate func(*site.Input)
		want   error
	}{
		{"empty name", func(in *site.Input) { in.Name = "  " }, site.ErrInvalidName},
		{"long name", func(in *site.Input) { in.Name = strings.Repeat("n", 101) }, site.ErrInvalidName},
		{"unknown protocol", func(in *site.Input) { in.Protocol = "gopher" }, site.ErrInvalidProtocol},
		{"empty host", func(in *site.Input) { in.Host = "" }, site.ErrInvalidHost},
		{"host with scheme", func(in *site.Input) { in.Host = "ftp://public.example" }, site.ErrInvalidHost},
		{"host with port", func(in *site.Input) { in.Host = "public.example:22" }, site.ErrInvalidHost},
		{"host with space", func(in *site.Input) { in.Host = "public example" }, site.ErrInvalidHost},
		{"long host", func(in *site.Input) { in.Host = strings.Repeat("a", 254) }, site.ErrInvalidHost},
		{"negative port", func(in *site.Input) { in.Port = -1 }, site.ErrInvalidPort},
		{"huge port", func(in *site.Input) { in.Port = 70000 }, site.ErrInvalidPort},
		{"empty user", func(in *site.Input) { in.Username = " " }, site.ErrInvalidUsername},
		{"long user", func(in *site.Input) { in.Username = strings.Repeat("u", 256) }, site.ErrInvalidUsername},
		{"unknown auth", func(in *site.Input) { in.AuthMethod = "token" }, site.ErrInvalidAuthMethod},
		{"key over ftp", func(in *site.Input) { in.Protocol, in.AuthMethod = site.FTP, site.AuthKey }, site.ErrInvalidAuthMethod},
		{"bad zone", func(in *site.Input) { in.ListTimezone = "Mars/Base" }, site.ErrInvalidTimezone},
		{"local zone", func(in *site.Input) { in.ListTimezone = "Local" }, site.ErrInvalidTimezone},
		{"private host", func(in *site.Input) { in.Host = "private.example" }, netguard.ErrBlocked},
		{"private address", func(in *site.Input) { in.Host = "192.168.1.1" }, netguard.ErrBlocked},
		{"unresolvable host", func(in *site.Input) { in.Host = "nowhere.example" }, netguard.ErrUnresolvable},
	}
	for _, tt := range tests {
		in := valid()
		tt.mutate(&in)
		if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", in, site.SecretChanges{}); !errors.Is(err, tt.want) {
			t.Errorf("%s = %v; want %v", tt.name, err, tt.want)
		}
	}
	invalid, blocked := valid(), valid()
	invalid.Name, blocked.Host = "", "private.example"
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", invalid, site.SecretChanges{}); !errors.Is(err, site.ErrInvalidName) {
		t.Errorf("Update() with an invalid name = %v; want ErrInvalidName", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", blocked, site.SecretChanges{}); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("Update() to a private host = %v; want ErrBlocked", err)
	}
	for _, host := range []string{"8.8.8.8", "2606:4700::1111", "my_host.example.com.", "a-b.example"} {
		in := valid()
		in.Name, in.Host = "h-"+host, host
		if err := f.guard.CheckHost(ctx, host); err != nil && !errors.Is(err, netguard.ErrUnresolvable) {
			t.Fatalf("guard(%s) = %v", host, err)
		}
		if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", in, site.SecretChanges{}); errors.Is(err, site.ErrInvalidHost) {
			t.Errorf("host %q refused as malformed", host)
		}
	}
}

// TestService_Authorization verifies who may do what.
//
// Scenarios:
//   - a viewer may list and get sites but not create, update or delete them (ErrForbidden)
//   - a non member gets workspace.ErrNotFound for every operation
//   - the editor may do all of them
//   - a site is not found through a workspace that does not own it
func TestService_Authorization(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	stranger := uuid.New()
	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{})
	if err != nil {
		t.Fatal(err)
	}

	if list, err := f.svc.List(ctx, f.viewer, f.ws); err != nil || len(list) != 1 {
		t.Errorf("viewer List() = %v, %v", list, err)
	}
	if got, err := f.svc.Get(ctx, f.viewer, f.ws, created.ID); err != nil || got.ID != created.ID {
		t.Errorf("viewer Get() = %v, %v", got, err)
	}
	if _, err := f.svc.Create(ctx, f.viewer, f.ws, "open", valid(), site.SecretChanges{}); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer Create() = %v; want ErrForbidden", err)
	}
	if _, err := f.svc.Update(ctx, f.viewer, f.ws, created.ID, "open", valid(), site.SecretChanges{}); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer Update() = %v; want ErrForbidden", err)
	}
	if err := f.svc.Delete(ctx, f.viewer, f.ws, created.ID); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer Delete() = %v; want ErrForbidden", err)
	}

	if _, err := f.svc.List(ctx, stranger, f.ws); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger List() = %v; want ErrNotFound", err)
	}
	if _, err := f.svc.Get(ctx, stranger, f.ws, created.ID); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger Get() = %v; want ErrNotFound", err)
	}
	if _, err := f.svc.OpenSecret(ctx, stranger, f.ws, created.ID, "open", site.SecretPassword); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger OpenSecret() = %v; want ErrNotFound", err)
	}

	if _, err := f.svc.Get(ctx, f.editor, uuid.New(), created.ID); !errors.Is(err, site.ErrNotFound) {
		t.Errorf("Get through an unrelated workspace = %v; want site.ErrNotFound", err)
	}
}

// TestService_Secrets verifies sealing, opening and changing secrets.
//
// Scenarios:
//   - secrets written at creation are stored sealed, never in clear text, and listed by kind only
//   - OpenSecret returns what was saved and ErrNoSecret for kinds that are not stored
//   - a secret sealed for one site cannot be opened as another site's (bound by site and kind)
//   - a locked vault refuses writing secrets and opening them, but not changing settings or removing secrets
//   - nil leaves a secret untouched, an empty string removes it, another value replaces it
//   - oversized secrets are ErrSecretTooLarge
//   - key provider and random source failures are returned
func TestService_Secrets(t *testing.T) {
	ctx := context.Background()
	random := &switchable{}
	f := newFixture(t, random)
	password := "s3cret-passw0rd"

	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{Password: text(password), PrivateKey: text("-----BEGIN KEY-----")})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Secrets) != 2 {
		t.Errorf("Create() secret kinds = %v", created.Secrets)
	}
	for _, sealed := range f.store.secrets {
		if strings.Contains(string(sealed.Data), password) {
			t.Fatal("a secret is stored in clear text")
		}
	}
	got, err := f.svc.OpenSecret(ctx, f.editor, f.ws, created.ID, "open", site.SecretPassword)
	if err != nil || string(got) != password {
		t.Fatalf("OpenSecret() = %q, %v", got, err)
	}
	if _, err := f.svc.OpenSecret(ctx, f.editor, f.ws, created.ID, "open", site.SecretPassphrase); !errors.Is(err, site.ErrNoSecret) {
		t.Errorf("OpenSecret(missing) = %v; want ErrNoSecret", err)
	}
	if _, err := f.svc.OpenSecret(ctx, f.editor, f.ws, created.ID, "closed", site.SecretPassword); !errors.Is(err, site.ErrLocked) {
		t.Errorf("OpenSecret(locked) = %v; want ErrLocked", err)
	}

	other := valid()
	other.Name = "Other"
	twin, err := f.svc.Create(ctx, f.editor, f.ws, "open", other, site.SecretChanges{})
	if err != nil {
		t.Fatal(err)
	}
	f.store.secrets[twin.ID.String()+string(site.SecretPassword)] = f.store.secrets[created.ID.String()+string(site.SecretPassword)]
	if _, err := f.svc.OpenSecret(ctx, f.editor, f.ws, twin.ID, "open", site.SecretPassword); !errors.Is(err, vault.ErrCorrupt) {
		t.Errorf("a secret moved to another site opened: %v", err)
	}

	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "closed", valid(), site.SecretChanges{Password: text("new")}); !errors.Is(err, site.ErrLocked) {
		t.Errorf("writing a secret while locked = %v; want ErrLocked", err)
	}
	if _, err := f.svc.Create(ctx, f.editor, f.ws, "closed", func() site.Input { in := valid(); in.Name = "Locked"; return in }(), site.SecretChanges{Password: text("x")}); !errors.Is(err, site.ErrLocked) {
		t.Errorf("creating with a secret while locked = %v; want ErrLocked", err)
	}
	renamed := valid()
	renamed.Name = "Renamed"
	updated, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "closed", renamed, site.SecretChanges{PrivateKey: text("")})
	if err != nil || updated.Name != "Renamed" {
		t.Fatalf("Update() while locked = %+v, %v", updated, err)
	}
	if _, ok := f.store.secrets[created.ID.String()+string(site.SecretPrivateKey)]; ok {
		t.Error("an emptied secret was not removed")
	}
	if _, ok := f.store.secrets[created.ID.String()+string(site.SecretPassword)]; !ok {
		t.Error("an untouched secret was removed")
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", renamed, site.SecretChanges{Password: text("replaced")}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.OpenSecret(ctx, f.editor, f.ws, created.ID, "open", site.SecretPassword); string(got) != "replaced" {
		t.Errorf("replaced secret = %q", got)
	}

	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", renamed, site.SecretChanges{Password: text(strings.Repeat("p", 1025))}); !errors.Is(err, site.ErrSecretTooLarge) {
		t.Errorf("oversized password = %v; want ErrSecretTooLarge", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", renamed, site.SecretChanges{PrivateKey: text(strings.Repeat("k", 64*1024))}); err != nil {
		t.Errorf("key at the limit = %v", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", renamed, site.SecretChanges{PrivateKey: text(strings.Repeat("k", 64*1024+1))}); !errors.Is(err, site.ErrSecretTooLarge) {
		t.Errorf("oversized key = %v; want ErrSecretTooLarge", err)
	}

	random.broken = true
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", renamed, site.SecretChanges{Password: text("x")}); !errors.Is(err, errStore) {
		t.Errorf("random failure = %v; want the source error", err)
	}
	random.broken = false
}

// TestService_KeyAndStoreFailures verifies that failures of collaborators are returned.
//
// Scenarios:
//   - a key provider failure other than a locked vault is returned when writing and when opening secrets
//   - an id generation failure, store failures on create, update, get and delete are returned
//   - updating or deleting a missing site is ErrNotFound, and updating with a used name is ErrNameTaken
func TestService_KeyAndStoreFailures(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{Password: text("x")})
	if err != nil {
		t.Fatal(err)
	}

	broken := site.NewService(f.store, roles{f.editor: workspace.RoleEditor}, keys{failure: errStore}, f.guard, testutil.NewFakeClock(time.Now()), rand.Reader, nil)
	if _, err := broken.Update(ctx, f.editor, f.ws, created.ID, "open", valid(), site.SecretChanges{Password: text("y")}); !errors.Is(err, errStore) {
		t.Errorf("key failure while writing = %v; want the provider error", err)
	}
	if _, err := broken.OpenSecret(ctx, f.editor, f.ws, created.ID, "open", site.SecretPassword); !errors.Is(err, errStore) {
		t.Errorf("key failure while opening = %v; want the provider error", err)
	}

	noID := site.NewService(f.store, roles{f.editor: workspace.RoleEditor}, keys{}, f.guard, testutil.NewFakeClock(time.Now()), rand.Reader, func() (uuid.UUID, error) { return uuid.Nil, errStore })
	if _, err := noID.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{}); !errors.Is(err, errStore) {
		t.Errorf("id failure = %v; want the generator error", err)
	}

	f.store.failCreate = errStore
	if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{}); !errors.Is(err, errStore) {
		t.Errorf("create failure = %v; want the store error", err)
	}
	f.store.failCreate = nil
	f.store.failUpdate = site.ErrNameTaken
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", valid(), site.SecretChanges{}); !errors.Is(err, site.ErrNameTaken) {
		t.Errorf("update onto a used name = %v; want ErrNameTaken", err)
	}
	f.store.failUpdate = nil
	f.store.failGet = errStore
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", valid(), site.SecretChanges{}); !errors.Is(err, errStore) {
		t.Errorf("read failure during update = %v; want the store error", err)
	}
	f.store.failGet = nil

	if _, err := f.svc.Update(ctx, f.editor, f.ws, uuid.New(), "open", valid(), site.SecretChanges{}); !errors.Is(err, site.ErrNotFound) {
		t.Errorf("update of a missing site = %v; want ErrNotFound", err)
	}
	if err := f.svc.Delete(ctx, f.editor, f.ws, uuid.New()); !errors.Is(err, site.ErrNotFound) {
		t.Errorf("delete of a missing site = %v; want ErrNotFound", err)
	}
	if err := f.svc.Delete(ctx, f.editor, f.ws, created.ID); err != nil {
		t.Errorf("Delete() = %v", err)
	}
}

// TestService_Collections verifies the tree of collections.
//
// Scenarios:
//   - collections are created at the top level and inside others, trimmed, with names checked and unique among siblings
//   - a missing parent is ErrCollectionNotFound, and nesting deeper than eight levels is ErrCollectionTooDeep
//   - renaming and moving work, moving to the top level too; moves into the collection itself or its descendants
//     are ErrCollectionCycle, moves that would exceed the depth are ErrCollectionTooDeep
//   - only empty collections can be deleted
//   - viewers may list but not change, strangers get workspace.ErrNotFound
//   - sites can be placed in, and moved between, collections that exist in their workspace
//   - failures of the store and of the identifier generator are returned
func TestService_Collections(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	create := func(name string, parent uuid.UUID) site.Collection {
		t.Helper()
		c, err := f.svc.CreateCollection(ctx, f.editor, f.ws, name, parent)
		if err != nil {
			t.Fatalf("CreateCollection(%q) = %v", name, err)
		}
		return c
	}

	acme := create("  Acme  ", uuid.Nil)
	if acme.Name != "Acme" || acme.WorkspaceID != f.ws || acme.ID == uuid.Nil {
		t.Errorf("collection = %+v", acme)
	}
	prod := create("Production", acme.ID)
	if _, err := f.svc.CreateCollection(ctx, f.editor, f.ws, "acme", uuid.Nil); !errors.Is(err, site.ErrCollectionNameTaken) {
		t.Errorf("duplicate name = %v; want ErrCollectionNameTaken", err)
	}
	create("Production", uuid.Nil)
	for _, bad := range []string{" ", strings.Repeat("n", 101)} {
		if _, err := f.svc.CreateCollection(ctx, f.editor, f.ws, bad, uuid.Nil); !errors.Is(err, site.ErrInvalidName) {
			t.Errorf("name %q = %v; want ErrInvalidName", bad, err)
		}
	}
	if _, err := f.svc.CreateCollection(ctx, f.editor, f.ws, "Orphan", uuid.New()); !errors.Is(err, site.ErrCollectionNotFound) {
		t.Errorf("missing parent = %v; want ErrCollectionNotFound", err)
	}

	deep := prod
	for i := 3; i <= 8; i++ {
		deep = create(fmt.Sprintf("Level %d", i), deep.ID)
	}
	if _, err := f.svc.CreateCollection(ctx, f.editor, f.ws, "Too deep", deep.ID); !errors.Is(err, site.ErrCollectionTooDeep) {
		t.Errorf("a ninth level = %v; want ErrCollectionTooDeep", err)
	}

	renamed := "Acme Inc"
	if c, err := f.svc.UpdateCollection(ctx, f.editor, f.ws, acme.ID, site.CollectionChanges{Name: &renamed}); err != nil || c.Name != renamed || c.ParentID != uuid.Nil {
		t.Errorf("rename = %+v, %v", c, err)
	}
	other := create("Clients", uuid.Nil)
	sub := create("Sub", other.ID)
	if c, err := f.svc.UpdateCollection(ctx, f.editor, f.ws, prod.ID, site.CollectionChanges{SetParent: true, ParentID: sub.ID}); err == nil {
		t.Errorf("moving a subtree of seven levels under a second level succeeded: %+v", c)
	} else if !errors.Is(err, site.ErrCollectionTooDeep) {
		t.Errorf("moving a deep subtree = %v; want ErrCollectionTooDeep", err)
	}
	if _, err := f.svc.UpdateCollection(ctx, f.editor, f.ws, acme.ID, site.CollectionChanges{SetParent: true, ParentID: acme.ID}); !errors.Is(err, site.ErrCollectionCycle) {
		t.Errorf("moving into itself = %v; want ErrCollectionCycle", err)
	}
	if _, err := f.svc.UpdateCollection(ctx, f.editor, f.ws, acme.ID, site.CollectionChanges{SetParent: true, ParentID: deep.ID}); !errors.Is(err, site.ErrCollectionCycle) {
		t.Errorf("moving into a descendant = %v; want ErrCollectionCycle", err)
	}
	if _, err := f.svc.UpdateCollection(ctx, f.editor, f.ws, acme.ID, site.CollectionChanges{SetParent: true, ParentID: uuid.New()}); !errors.Is(err, site.ErrCollectionNotFound) {
		t.Errorf("moving under a missing collection = %v; want ErrCollectionNotFound", err)
	}
	if _, err := f.svc.UpdateCollection(ctx, f.editor, f.ws, uuid.New(), site.CollectionChanges{Name: &renamed}); !errors.Is(err, site.ErrCollectionNotFound) {
		t.Errorf("updating a missing collection = %v; want ErrCollectionNotFound", err)
	}
	blank := " "
	if _, err := f.svc.UpdateCollection(ctx, f.editor, f.ws, acme.ID, site.CollectionChanges{Name: &blank}); !errors.Is(err, site.ErrInvalidName) {
		t.Errorf("renaming to a blank = %v; want ErrInvalidName", err)
	}
	if c, err := f.svc.UpdateCollection(ctx, f.editor, f.ws, other.ID, site.CollectionChanges{SetParent: true, ParentID: acme.ID}); err != nil || c.ParentID != acme.ID {
		t.Errorf("move = %+v, %v", c, err)
	}
	if c, err := f.svc.UpdateCollection(ctx, f.editor, f.ws, other.ID, site.CollectionChanges{SetParent: true}); err != nil || c.ParentID != uuid.Nil {
		t.Errorf("move to the top level = %+v, %v", c, err)
	}

	if err := f.svc.DeleteCollection(ctx, f.editor, f.ws, acme.ID); !errors.Is(err, site.ErrCollectionNotEmpty) {
		t.Errorf("deleting a non-empty collection = %v; want ErrCollectionNotEmpty", err)
	}
	if err := f.svc.DeleteCollection(ctx, f.editor, f.ws, sub.ID); err != nil {
		t.Errorf("deleting an empty collection = %v", err)
	}
	if err := f.svc.DeleteCollection(ctx, f.editor, f.ws, other.ID); err != nil {
		t.Errorf("deleting a collection that became empty = %v", err)
	}

	list, err := f.svc.ListCollections(ctx, f.viewer, f.ws)
	if err != nil || len(list) < 3 {
		t.Errorf("viewer ListCollections() = %d, %v", len(list), err)
	}
	stranger := uuid.New()
	if _, err := f.svc.ListCollections(ctx, stranger, f.ws); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger ListCollections() = %v", err)
	}
	if _, err := f.svc.CreateCollection(ctx, f.viewer, f.ws, "Nope", uuid.Nil); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer CreateCollection() = %v; want ErrForbidden", err)
	}
	if _, err := f.svc.UpdateCollection(ctx, f.viewer, f.ws, acme.ID, site.CollectionChanges{Name: &renamed}); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer UpdateCollection() = %v; want ErrForbidden", err)
	}
	if err := f.svc.DeleteCollection(ctx, f.viewer, f.ws, acme.ID); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer DeleteCollection() = %v; want ErrForbidden", err)
	}

	in := valid()
	in.CollectionID = prod.ID
	placed, err := f.svc.Create(ctx, f.editor, f.ws, "open", in, site.SecretChanges{})
	if err != nil || placed.CollectionID != prod.ID {
		t.Fatalf("Create() in a collection = %+v, %v", placed, err)
	}
	if err := f.svc.DeleteCollection(ctx, f.editor, f.ws, deep.ID); err != nil {
		t.Fatal(err)
	}
	in.Name, in.CollectionID = "Elsewhere", uuid.New()
	if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", in, site.SecretChanges{}); !errors.Is(err, site.ErrCollectionNotFound) {
		t.Errorf("Create() in a missing collection = %v; want ErrCollectionNotFound", err)
	}
	moved := valid()
	if got, err := f.svc.Update(ctx, f.editor, f.ws, placed.ID, "open", moved, site.SecretChanges{}); err != nil || got.CollectionID != uuid.Nil {
		t.Errorf("Update() to the top level = %+v, %v", got, err)
	}
	moved.CollectionID = uuid.New()
	if _, err := f.svc.Update(ctx, f.editor, f.ws, placed.ID, "open", moved, site.SecretChanges{}); !errors.Is(err, site.ErrCollectionNotFound) {
		t.Errorf("Update() into a missing collection = %v; want ErrCollectionNotFound", err)
	}

	f.store.failList = errStore
	if _, err := f.svc.CreateCollection(ctx, f.editor, f.ws, "X", uuid.Nil); !errors.Is(err, errStore) {
		t.Errorf("CreateCollection() with a failing list = %v", err)
	}
	if _, err := f.svc.UpdateCollection(ctx, f.editor, f.ws, acme.ID, site.CollectionChanges{}); !errors.Is(err, errStore) {
		t.Errorf("UpdateCollection() with a failing list = %v", err)
	}
	f.store.failList = nil
	f.store.failCreateC = errStore
	if _, err := f.svc.CreateCollection(ctx, f.editor, f.ws, "X", uuid.Nil); !errors.Is(err, errStore) {
		t.Errorf("CreateCollection() with a failing create = %v", err)
	}
	f.store.failCreateC = nil
	f.store.failUpdateC = errStore
	if _, err := f.svc.UpdateCollection(ctx, f.editor, f.ws, acme.ID, site.CollectionChanges{Name: &renamed}); !errors.Is(err, errStore) {
		t.Errorf("UpdateCollection() with a failing update = %v", err)
	}
	f.store.failUpdateC = nil
	f.store.failGetC = errStore
	if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", in, site.SecretChanges{}); !errors.Is(err, errStore) {
		t.Errorf("Create() with a failing collection lookup = %v", err)
	}
	f.store.failGetC = nil

	noID := site.NewService(f.store, roles{f.editor: workspace.RoleEditor}, keys{}, f.guard, testutil.NewFakeClock(time.Now()), rand.Reader, func() (uuid.UUID, error) { return uuid.Nil, errStore })
	if _, err := noID.CreateCollection(ctx, f.editor, f.ws, "Y", uuid.Nil); !errors.Is(err, errStore) {
		t.Errorf("CreateCollection() with a failing generator = %v", err)
	}
}
