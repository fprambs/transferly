package site_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/sshkey"
	"github.com/fprambs/transferly/internal/core/vault"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/testutil"
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
	for _, other := range m.sites {
		if other.WorkspaceID == s.WorkspaceID && other.CollectionID == s.CollectionID && strings.EqualFold(other.Name, s.Name) {
			return site.ErrNameTaken
		}
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
	for id, other := range m.sites {
		if id != s.ID && other.WorkspaceID == s.WorkspaceID && other.CollectionID == s.CollectionID && strings.EqualFold(other.Name, s.Name) {
			return site.ErrNameTaken
		}
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

// Site returns one site with the kinds of its stored secrets, as the real store does.
func (m *memStore) Site(_ context.Context, ws, id uuid.UUID) (site.Site, error) {
	if m.failGet != nil {
		return site.Site{}, m.failGet
	}
	s, ok := m.sites[id]
	if !ok || s.WorkspaceID != ws {
		return site.Site{}, site.ErrNotFound
	}
	s.Secrets = nil
	for key, sealed := range m.secrets {
		if strings.HasPrefix(key, id.String()) {
			s.Secrets = append(s.Secrets, sealed.Kind)
		}
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
	version int
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

// Current returns the key of the unlocked session at version 1, or at the configured version.
func (k keys) Current(_ context.Context, _, _ uuid.UUID, session string) ([]byte, int, error) {
	key, err := k.Key(session)
	return key, max(k.version, 1), err
}

// Version returns the key of the unlocked session whatever the version.
func (k keys) Version(_ context.Context, _, _ uuid.UUID, session string, _ int) ([]byte, error) {
	return k.Key(session)
}

// Unlocked reports whether the session is the unlocked one and no failure is configured.
func (k keys) Unlocked(session string) bool {
	key, err := k.Key(session)
	vault.Wipe(key)
	return err == nil
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
	f.svc = site.NewService(f.store, authz, keys{dek: dek, session: "open"}, f.guard, connectors.Builtin(), clock, random, nil)
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

// valid returns input that passes validation: an SFTP site of a public host.
func valid() site.Input {
	return site.Input{Name: "Production", Connector: "sftp", Config: map[string]string{"host": "public.example", "username": "deploy"}}
}

// with returns a copy of input with its name and some settings changed, given as name and value pairs; an empty value removes the setting.
func with(in site.Input, name string, settings ...string) site.Input {
	config := make(map[string]string, len(in.Config))
	for k, v := range in.Config {
		config[k] = v
	}
	for i := 0; i+1 < len(settings); i += 2 {
		if settings[i+1] == "" {
			delete(config, settings[i])
		} else {
			config[settings[i]] = settings[i+1]
		}
	}
	in.Name, in.Config = name, config
	return in
}

// ftp returns input that passes validation for an FTP site.
func ftp(name string) site.Input {
	return site.Input{Name: name, Connector: "ftp", Config: map[string]string{"host": "public.example", "username": "deploy"}}
}

// TestService_CreateValidation verifies the rules on site settings.
//
// Scenarios:
//   - valid input is stored settled by the schema of its connector: trimmed, with the defaults filled in
//   - the default remote folder is stored cleaned, the root is stored empty, and a relative, too long, control-character or invalid UTF-8
//     one is refused with ErrInvalidRemoteDir
//   - a known label color and a speed limit are stored, and an unknown color or a speed limit out of range are refused
//   - empty or oversized names are refused with ErrInvalidName, an unknown connector and a configuration that breaks the schema with a
//     field error
//   - every host the configuration points to, the server's, the proxy's and the jump host's, is checked against the network guard
func TestService_CreateValidation(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})

	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", site.Input{Name: "  Prod  ", Connector: "ftp", Config: map[string]string{"host": " public.example ", "username": " me "}}, site.SecretChanges{})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	want := map[string]string{"host": "public.example", "encryption": "explicit", "port": "21", "username": "me", "auth": "password", "list_timezone": "UTC", "charset": "utf-8", "connections": "4"}
	if created.Name != "Prod" || created.Connector != "ftp" || !maps.Equal(created.Config, want) || created.Setting("port") != "21" ||
		created.CreatedAt.Location() != time.UTC || created.ID == uuid.Nil {
		t.Errorf("Create() = %+v", created)
	}
	if capped, err := f.svc.Create(ctx, f.editor, f.ws, "open", func() site.Input { in := with(valid(), "Capped"); in.SpeedLimit = 65536; return in }(), site.SecretChanges{}); err != nil || capped.SpeedLimit != 65536 {
		t.Errorf("site with a speed limit = %+v, %v", capped, err)
	}
	if labeled, err := f.svc.Create(ctx, f.editor, f.ws, "open", func() site.Input { in := with(valid(), "Labeled"); in.Color = "red"; return in }(), site.SecretChanges{}); err != nil || labeled.Color != "red" {
		t.Errorf("site with a color = %+v, %v", labeled, err)
	}
	for dir, want := range map[string]string{" /var/www/./html/ ": "/var/www/html", "/": "", "": ""} {
		in := with(valid(), "Dir "+uuid.NewString())
		in.RemoteDir = dir
		if got, err := f.svc.Create(ctx, f.editor, f.ws, "open", in, site.SecretChanges{}); err != nil || got.RemoteDir != want {
			t.Errorf("remote folder %q = %q, %v; want %q", dir, got.RemoteDir, err, want)
		}
	}
	routed, err := f.svc.Create(ctx, f.editor, f.ws, "open", with(valid(), "Routed", "proxy_kind", "socks5", "proxy_host", "public.example", "jump_host", "public.example", "jump_username", "bastion"), site.SecretChanges{})
	if err != nil || routed.Setting("proxy_port") != "1080" || routed.Setting("jump_port") != "22" {
		t.Errorf("site with a proxy and a jump host = %+v, %v", routed, err)
	}

	tests := []struct {
		name  string
		in    site.Input
		want  error
		field string
	}{
		{"empty name", with(valid(), "  "), site.ErrInvalidName, ""},
		{"long name", with(valid(), strings.Repeat("n", 101)), site.ErrInvalidName, ""},
		{"unknown connector", func() site.Input { in := with(valid(), "Gopher"); in.Connector = "gopher"; return in }(), connectors.ErrInvalidConfig, "connector"},
		{"bad host", with(valid(), "Bad host", "host", "ftp://public.example"), connectors.ErrInvalidConfig, "host"},
		{"huge port", with(valid(), "Huge port", "port", "70000"), connectors.ErrInvalidConfig, "port"},
		{"empty user", with(valid(), "No user", "username", ""), connectors.ErrInvalidConfig, "username"},
		{"negative speed limit", func() site.Input { in := with(valid(), "Slow"); in.SpeedLimit = -1; return in }(), site.ErrInvalidSpeedLimit, ""},
		{"speed limit below the smallest cap", func() site.Input { in := with(valid(), "Slow"); in.SpeedLimit = 1023; return in }(), site.ErrInvalidSpeedLimit, ""},
		{"speed limit above the largest cap", func() site.Input { in := with(valid(), "Fast"); in.SpeedLimit = 1<<40 + 1; return in }(), site.ErrInvalidSpeedLimit, ""},
		{"relative remote folder", func() site.Input { in := with(valid(), "Dir"); in.RemoteDir = "var/www"; return in }(), site.ErrInvalidRemoteDir, ""},
		{"remote folder with a control character", func() site.Input { in := with(valid(), "Dir"); in.RemoteDir = "/var/\nwww"; return in }(), site.ErrInvalidRemoteDir, ""},
		{"remote folder too long", func() site.Input {
			in := with(valid(), "Dir")
			in.RemoteDir = "/" + strings.Repeat("d", 4096)
			return in
		}(), site.ErrInvalidRemoteDir, ""},
		{"remote folder not UTF-8", func() site.Input { in := with(valid(), "Dir"); in.RemoteDir = "/bad\xff"; return in }(), site.ErrInvalidRemoteDir, ""},
		{"unknown color", func() site.Input { in := with(valid(), "Purple"); in.Color = "purple"; return in }(), site.ErrInvalidColor, ""},
		{"private host", with(valid(), "Private", "host", "private.example"), netguard.ErrBlocked, ""},
		{"private address", with(valid(), "Private", "host", "192.168.1.1"), netguard.ErrBlocked, ""},
		{"unresolvable host", with(valid(), "Nowhere", "host", "nowhere.example"), netguard.ErrUnresolvable, ""},
		{"proxy on a private host", with(valid(), "Proxy", "proxy_kind", "http", "proxy_host", "private.example"), netguard.ErrBlocked, ""},
		{"jump host on a private host", with(valid(), "Jump", "jump_host", "private.example", "jump_username", "b"), netguard.ErrBlocked, ""},
	}
	for _, tt := range tests {
		_, err := f.svc.Create(ctx, f.editor, f.ws, "open", tt.in, site.SecretChanges{})
		var field *connectors.FieldError
		if !errors.Is(err, tt.want) || (tt.field != "" && (!errors.As(err, &field) || field.Field != tt.field)) {
			t.Errorf("%s = %v; want %v of field %q", tt.name, err, tt.want, tt.field)
		}
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", with(valid(), ""), site.SecretChanges{}); !errors.Is(err, site.ErrInvalidName) {
		t.Errorf("Update() with an invalid name = %v; want ErrInvalidName", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", with(valid(), "Prod", "host", "private.example"), site.SecretChanges{}); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("Update() to a private host = %v; want ErrBlocked", err)
	}
}

// failingSchemas is a catalog that cannot be read.
type failingSchemas struct{}

// Lookup always fails.
func (failingSchemas) Lookup(string) (connectors.Descriptor, error) {
	return connectors.Descriptor{}, errStore
}

// TestService_SchemaFailure verifies that a catalog that cannot be read is reported.
//
// Use cases:
//   - creating a site with a catalog that fails returns its error
func TestService_SchemaFailure(t *testing.T) {
	f := newFixture(t, &switchable{})
	broken := site.NewService(f.store, roles{f.editor: workspace.RoleEditor}, keys{}, f.guard, failingSchemas{}, testutil.NewFakeClock(time.Now()), rand.Reader, nil)
	if _, err := broken.Create(context.Background(), f.editor, f.ws, "open", valid(), site.SecretChanges{}); !errors.Is(err, errStore) {
		t.Errorf("Create() with a failing catalog = %v; want its error", err)
	}
}

// TestService_Authorization verifies who may do what.
//
// Scenarios:
//   - a viewer may list and get sites but not create, update or delete them (ErrForbidden)
//   - a non member gets workspace.ErrNotFound for every operation
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

// TestService_Elevation verifies who may turn on the sudo mode of a site (ADR-0027).
//
// Scenarios:
//   - an editor may not create a site with sudo on, nor change a site that has it, even to turn it off; both fail as ErrElevation
//   - an admin may create it with its sudo password, and change and turn it off; an editor may then change the site again
func TestService_Elevation(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	admin := uuid.New()
	dek := make([]byte, vault.KeyLength)
	if _, err := rand.Read(dek); err != nil {
		t.Fatal(err)
	}
	svc := site.NewService(f.store, roles{f.editor: workspace.RoleEditor, admin: workspace.RoleAdmin}, keys{dek: dek, session: "open"}, f.guard, connectors.Builtin(), testutil.NewFakeClock(time.Now()), rand.Reader, nil)
	sudo := with(valid(), "Root", "sudo", "true")
	if _, err := svc.Create(ctx, f.editor, f.ws, "open", sudo, site.SecretChanges{}); !errors.Is(err, site.ErrElevation) || !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("editor Create(sudo) = %v; want ErrElevation", err)
	}
	created, err := svc.Create(ctx, admin, f.ws, "open", sudo, site.SecretChanges{site.SecretSudoPassword: "root-pass"})
	if err != nil || created.Config["sudo_user"] != "root" || !slices.Contains(created.Secrets, site.SecretSudoPassword) {
		t.Fatalf("admin Create(sudo) = %+v, %v", created, err)
	}
	if _, err := svc.Update(ctx, f.editor, f.ws, created.ID, "open", with(sudo, "Root", "sudo", "false"), site.SecretChanges{}); !errors.Is(err, site.ErrElevation) {
		t.Errorf("editor Update(sudo site) = %v; want ErrElevation", err)
	}
	if _, err := svc.Update(ctx, admin, f.ws, created.ID, "open", with(sudo, "Root", "sudo", "false"), site.SecretChanges{}); err != nil {
		t.Fatalf("admin Update(sudo off) = %v", err)
	}
	if _, err := svc.Update(ctx, f.editor, f.ws, created.ID, "open", with(valid(), "Plain"), site.SecretChanges{}); err != nil {
		t.Errorf("editor Update(after sudo was turned off) = %v", err)
	}
}

// TestService_Secrets verifies sealing, opening and changing secrets.
//
// Scenarios:
//   - secrets written at creation are stored sealed, never in clear text, and listed by kind only
//   - OpenSecret returns what was saved and ErrNoSecret for kinds that are not stored
//   - a secret sealed for one site cannot be opened as another site's (bound by site and kind)
//   - a locked vault refuses writing secrets and opening them, but not changing settings or removing secrets
//   - a secret left out stays, an empty value removes it, another value replaces it
//   - a secret the connector does not have is refused naming it
//   - oversized secrets are ErrSecretTooLarge, and the random source failing is returned
func TestService_Secrets(t *testing.T) {
	ctx := context.Background()
	random := &switchable{}
	f := newFixture(t, random)
	password := "s3cret-passw0rd"
	proxied := with(valid(), "Production", "proxy_kind", "http", "proxy_host", "public.example")

	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", proxied, site.SecretChanges{site.SecretPassword: password, site.SecretProxyPassword: "proxy"})
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

	twin, err := f.svc.Create(ctx, f.editor, f.ws, "open", with(valid(), "Other"), site.SecretChanges{})
	if err != nil {
		t.Fatal(err)
	}
	f.store.secrets[twin.ID.String()+string(site.SecretPassword)] = f.store.secrets[created.ID.String()+string(site.SecretPassword)]
	if _, err := f.svc.OpenSecret(ctx, f.editor, f.ws, twin.ID, "open", site.SecretPassword); !errors.Is(err, vault.ErrCorrupt) {
		t.Errorf("a secret moved to another site opened: %v", err)
	}

	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "closed", proxied, site.SecretChanges{site.SecretPassword: "new"}); !errors.Is(err, site.ErrLocked) {
		t.Errorf("writing a secret while locked = %v; want ErrLocked", err)
	}
	if _, err := f.svc.Create(ctx, f.editor, f.ws, "closed", with(valid(), "Locked"), site.SecretChanges{site.SecretPassword: "x"}); !errors.Is(err, site.ErrLocked) {
		t.Errorf("creating with a secret while locked = %v; want ErrLocked", err)
	}
	renamed := with(proxied, "Renamed")
	updated, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "closed", renamed, site.SecretChanges{site.SecretProxyPassword: ""})
	if err != nil || updated.Name != "Renamed" {
		t.Fatalf("Update() while locked = %+v, %v", updated, err)
	}
	if _, ok := f.store.secrets[created.ID.String()+string(site.SecretProxyPassword)]; ok {
		t.Error("an emptied secret was not removed")
	}
	if _, ok := f.store.secrets[created.ID.String()+string(site.SecretPassword)]; !ok {
		t.Error("an untouched secret was removed")
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", renamed, site.SecretChanges{site.SecretPassword: "replaced"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.OpenSecret(ctx, f.editor, f.ws, created.ID, "open", site.SecretPassword); string(got) != "replaced" {
		t.Errorf("replaced secret = %q", got)
	}

	var unknown *connectors.FieldError
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", renamed, site.SecretChanges{"token": "x"}); !errors.As(err, &unknown) || unknown.Field != "token" {
		t.Errorf("an unknown secret = %v; want a field error naming it", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", renamed, site.SecretChanges{"host": "x"}); !errors.Is(err, connectors.ErrInvalidConfig) {
		t.Errorf("a setting given as a secret = %v; want ErrInvalidConfig", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", renamed, site.SecretChanges{site.SecretPassword: strings.Repeat("p", 1025)}); !errors.Is(err, site.ErrSecretTooLarge) {
		t.Errorf("oversized password = %v; want ErrSecretTooLarge", err)
	}

	random.broken = true
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", renamed, site.SecretChanges{site.SecretPassword: "x"}); !errors.Is(err, errStore) {
		t.Errorf("random failure = %v; want the source error", err)
	}
	random.broken = false
}

// TestService_LogonTypes covers how the logon type decides which secrets a site keeps (ADR-0010, ADR-0016, 4.8e).
//
// Use cases:
//   - sites that ask for their password, anonymous sites and interactive sites of either connector store no secret, even when the request
//     sends one, and need no unlocked vault for it; anonymous ones log in as the anonymous user whatever name was sent
//   - switching a saved site to one of those logon types removes the secrets it had
//   - an FTP site of the account logon type keeps its password and its account, and switching it to a password drops the account only
//   - a site moved to another connector drops the secrets the new one does not have
func TestService_LogonTypes(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	for _, auth := range []string{"ask", "anonymous", "interactive"} {
		for _, base := range []site.Input{valid(), ftp("")} {
			in := with(base, auth+" "+base.Connector, "auth", auth, "username", "someone")
			created, err := f.svc.Create(ctx, f.editor, f.ws, "closed", in, site.SecretChanges{site.SecretPassword: "typed"})
			if err != nil {
				t.Fatalf("Create(%s, %s) = %v", base.Connector, auth, err)
			}
			wantUser := "someone"
			if auth == "anonymous" {
				wantUser = connectors.AnonymousUser
			}
			if len(created.Secrets) != 0 || created.Setting("username") != wantUser {
				t.Errorf("Create(%s, %s) = %v as %q; want no secrets as %q", base.Connector, auth, created.Secrets, created.Setting("username"), wantUser)
			}
		}
	}

	keyed := with(valid(), "Keyed", "auth", "key")
	stored, err := f.svc.Create(ctx, f.editor, f.ws, "open", keyed, site.SecretChanges{site.SecretPrivateKey: testutil.PrivateKey(t, "phrase"), site.SecretPassphrase: "phrase"})
	if err != nil || len(stored.Secrets) != 2 {
		t.Fatalf("Create(key) = %+v, %v", stored, err)
	}
	updated, err := f.svc.Update(ctx, f.editor, f.ws, stored.ID, "closed", with(keyed, "Keyed", "auth", "ask"), site.SecretChanges{})
	if err != nil || len(updated.Secrets) != 0 {
		t.Errorf("a site switched to ask = %v, %v; want no secrets", updated.Secrets, err)
	}

	billed := with(ftp("Billed"), "Billed", "auth", "account")
	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", billed, site.SecretChanges{site.SecretPassword: "pw", site.SecretAccount: "billing"})
	if err != nil || !slices.Contains(created.Secrets, site.SecretAccount) || !slices.Contains(created.Secrets, site.SecretPassword) {
		t.Fatalf("Create(account) = %v, %v", created.Secrets, err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", with(billed, "Billed", "auth", "password"), site.SecretChanges{}); err != nil {
		t.Fatal(err)
	}
	_, account := f.store.secrets[created.ID.String()+string(site.SecretAccount)]
	_, password := f.store.secrets[created.ID.String()+string(site.SecretPassword)]
	if account || !password {
		t.Errorf("a site switched to the normal logon kept the account %v and the password %v; want only the password", account, password)
	}

	moved, err := f.svc.Update(ctx, f.editor, f.ws, stored.ID, "open", with(valid(), "Keyed"), site.SecretChanges{site.SecretPassword: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	f.store.secrets[stored.ID.String()+string(site.SecretAccount)] = site.Sealed{Kind: site.SecretAccount}
	if moved, err = f.svc.Update(ctx, f.editor, f.ws, stored.ID, "open", with(valid(), "Keyed"), site.SecretChanges{}); err != nil || slices.Contains(moved.Secrets, site.SecretAccount) {
		t.Errorf("a secret the connector does not have was kept: %v, %v", moved.Secrets, err)
	}
}

// TestService_PrivateKeys covers how [site.Service.Create] and [site.Service.Update] read the private keys they save (4.8g).
//
// Use cases:
//   - a PuTTY key is stored converted to OpenSSH's format, which connecting reads with the same passphrase
//   - a key that is not encrypted drops the passphrase sent with it
//   - a public key, an encrypted key without its passphrase or with a wrong one, and text that is no key are refused
//     with a [*site.KeyError] that names the field of the key, the site's or the jump host's
//   - a key larger than any key file is refused as too large before it is read
//   - an update that leaves the key as it is does not read it, and one that replaces it reads the new one
func TestService_PrivateKeys(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	ppk, err := os.ReadFile("../sshkey/testdata/ppk3-rsa-argon2id.ppk")
	if err != nil {
		t.Fatal(err)
	}
	keyed := with(valid(), "PuTTY", "auth", "key")
	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", keyed, site.SecretChanges{site.SecretPrivateKey: string(ppk), site.SecretPassphrase: "correct horse"})
	if err != nil {
		t.Fatalf("Create() with a PuTTY key = %v", err)
	}
	stored, err := f.svc.OpenSecret(ctx, f.editor, f.ws, created.ID, "open", site.SecretPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ssh.ParsePrivateKeyWithPassphrase(stored, []byte("correct horse")); err != nil {
		t.Errorf("the stored PuTTY key does not open in OpenSSH's format: %v", err)
	}

	unencrypted, err := f.svc.Create(ctx, f.editor, f.ws, "open", with(keyed, "Plain"), site.SecretChanges{site.SecretPrivateKey: testutil.PrivateKey(t, ""), site.SecretPassphrase: "stale"})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(unencrypted.Secrets, site.SecretPassphrase) {
		t.Error("a key that is not encrypted kept the passphrase sent with it")
	}

	_, signer, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ssh.NewPublicKey(signer.Public())
	if err != nil {
		t.Fatal(err)
	}
	encrypted := testutil.PrivateKey(t, "phrase")
	withJump := with(keyed, "Jump", "jump_host", "public.example", "jump_username", "bastion", "jump_auth", "key")
	refusals := []struct {
		name    string
		in      site.Input
		changes site.SecretChanges
		field   string
		want    error
	}{
		{"public key", keyed, site.SecretChanges{site.SecretPrivateKey: string(ssh.MarshalAuthorizedKey(public))}, "private_key", sshkey.ErrPublicKey},
		{"no passphrase", keyed, site.SecretChanges{site.SecretPrivateKey: encrypted}, "private_key", sshkey.ErrPassphraseRequired},
		{"not a key", keyed, site.SecretChanges{site.SecretPrivateKey: "hello"}, "private_key", sshkey.ErrUnreadable},
		{"jump wrong passphrase", withJump, site.SecretChanges{site.SecretPrivateKey: encrypted, site.SecretPassphrase: "phrase",
			site.SecretJumpPrivateKey: encrypted, site.SecretJumpPassphrase: "wrong"}, "jump_private_key", sshkey.ErrWrongPassphrase},
	}
	for _, r := range refusals {
		_, err := f.svc.Create(ctx, f.editor, f.ws, "open", with(r.in, "Refused "+r.name), r.changes)
		var keyErr *site.KeyError
		if !errors.As(err, &keyErr) || keyErr.Field != r.field || !errors.Is(err, r.want) || !strings.HasPrefix(err.Error(), r.field+": ") {
			t.Errorf("Create(%s) = %v; want a key error of %s for %v", r.name, err, r.field, r.want)
		}
	}
	if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", keyed, site.SecretChanges{site.SecretPrivateKey: strings.Repeat("A", 64*1024+1)}); !errors.Is(err, site.ErrSecretTooLarge) {
		t.Errorf("Create() with a huge key = %v; want ErrSecretTooLarge", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", keyed, site.SecretChanges{site.SecretPassphrase: "changed"}); err != nil {
		t.Errorf("Update() keeping the key = %v", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", keyed, site.SecretChanges{site.SecretPrivateKey: "hello"}); !errors.Is(err, sshkey.ErrUnreadable) {
		t.Errorf("Update() with text that is no key = %v; want ErrUnreadable", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", keyed, site.SecretChanges{site.SecretPrivateKey: testutil.PrivateKey(t, "")}); err != nil {
		t.Errorf("Update() with a new key = %v", err)
	}
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
	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{site.SecretPassword: "x"})
	if err != nil {
		t.Fatal(err)
	}

	broken := site.NewService(f.store, roles{f.editor: workspace.RoleEditor}, keys{failure: errStore}, f.guard, connectors.Builtin(), testutil.NewFakeClock(time.Now()), rand.Reader, nil)
	if _, err := broken.Update(ctx, f.editor, f.ws, created.ID, "open", valid(), site.SecretChanges{site.SecretPassword: "y"}); !errors.Is(err, errStore) {
		t.Errorf("key failure while writing = %v; want the provider error", err)
	}
	if _, err := broken.OpenSecret(ctx, f.editor, f.ws, created.ID, "open", site.SecretPassword); !errors.Is(err, errStore) {
		t.Errorf("key failure while opening = %v; want the provider error", err)
	}

	noID := site.NewService(f.store, roles{f.editor: workspace.RoleEditor}, keys{}, f.guard, connectors.Builtin(), testutil.NewFakeClock(time.Now()), rand.Reader, func() (uuid.UUID, error) { return uuid.Nil, errStore })
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

// offering refuses the connectors it holds, as an instance that disabled them.
type offering map[string]bool

// CheckConnector implements [site.Offering].
func (o offering) CheckConnector(_ context.Context, kind string) error {
	if o[kind] {
		return connectors.ErrDisabled
	}
	return nil
}

// TestService_Offering verifies that sites follow the connectors the instance offers (ADR-0019).
//
// Use cases:
//   - a site of a disabled connector is not created
//   - an existing site of a disabled connector can still be edited, but no site moves to a disabled connector
func TestService_Offering(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.UseOffering(offering{"sftp": true, "ftp": true})
	if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", with(valid(), "Staging"), site.SecretChanges{}); !errors.Is(err, connectors.ErrDisabled) || len(f.store.sites) != 1 {
		t.Errorf("Create() of a disabled connector = %v with %d sites; want ErrDisabled and no new site", err, len(f.store.sites))
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", with(valid(), "Renamed"), site.SecretChanges{}); err != nil {
		t.Errorf("Update() of a site of a disabled connector = %v; want it edited", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", ftp("Moved"), site.SecretChanges{}); !errors.Is(err, connectors.ErrDisabled) {
		t.Errorf("Update() to a disabled connector = %v; want ErrDisabled", err)
	}
}

// TestService_MoveSite verifies moving a saved site between collections (4.14h).
//
// Scenarios:
//   - an editor moves a site into a collection and back to the top level; the other settings stay
//   - a site whose host no longer resolves can still be moved, since the host is not checked again
//   - a viewer is refused, and a missing collection or site is reported
//   - failures of the store while reading or writing are reported
func TestService_MoveSite(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	collection, err := f.svc.CreateCollection(ctx, f.editor, f.ws, "Clients", uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{site.SecretPassword: "pw"})
	if err != nil {
		t.Fatal(err)
	}

	moved, err := f.svc.MoveSite(ctx, f.editor, f.ws, created.ID, collection.ID)
	if err != nil || moved.CollectionID != collection.ID || moved.Setting("host") != created.Setting("host") {
		t.Fatalf("MoveSite() into a collection = %+v, %v", moved, err)
	}
	stored := f.store.sites[created.ID]
	stored.Config = map[string]string{"host": "gone.example", "username": "deploy"}
	f.store.sites[created.ID] = stored
	back, err := f.svc.MoveSite(ctx, f.editor, f.ws, created.ID, uuid.Nil)
	if err != nil || back.CollectionID != uuid.Nil || back.Setting("host") != "gone.example" {
		t.Errorf("MoveSite() to the top level = %+v, %v", back, err)
	}

	if _, err := f.svc.MoveSite(ctx, f.viewer, f.ws, created.ID, uuid.Nil); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer MoveSite() = %v", err)
	}
	if _, err := f.svc.MoveSite(ctx, f.editor, f.ws, created.ID, uuid.New()); !errors.Is(err, site.ErrCollectionNotFound) {
		t.Errorf("MoveSite() into a missing collection = %v", err)
	}
	if _, err := f.svc.MoveSite(ctx, f.editor, f.ws, uuid.New(), uuid.Nil); !errors.Is(err, site.ErrNotFound) {
		t.Errorf("MoveSite() of a missing site = %v", err)
	}
	f.store.failUpdate = errStore
	if _, err := f.svc.MoveSite(ctx, f.editor, f.ws, created.ID, uuid.Nil); !errors.Is(err, errStore) {
		t.Errorf("MoveSite() with a failing store = %v", err)
	}
	f.store.failUpdate = nil

	twin := valid()
	twin.CollectionID = collection.ID
	inside, err := f.svc.Create(ctx, f.editor, f.ws, "open", twin, site.SecretChanges{})
	if err != nil {
		t.Fatalf("Create() of a site with the same name in another collection = %v", err)
	}
	third, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{})
	if !errors.Is(err, site.ErrNameTaken) {
		t.Fatalf("Create() of a site with the same name in the same collection = %+v, %v", third, err)
	}
	moved, err = f.svc.MoveSite(ctx, f.editor, f.ws, created.ID, collection.ID)
	if err != nil || moved.Name != inside.Name+" (1)" || moved.CollectionID != collection.ID {
		t.Errorf("MoveSite() next to a site with its name = %+v, %v; want it renamed with (1)", moved, err)
	}
	for n := 0; n < 101; n++ {
		filler := valid()
		filler.Name = site.NumberedName(inside.Name, n+2)
		filler.CollectionID = collection.ID
		if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", filler, site.SecretChanges{}); err != nil {
			t.Fatal(err)
		}
	}
	other, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.MoveSite(ctx, f.editor, f.ws, other.ID, collection.ID); !errors.Is(err, site.ErrNameTaken) {
		t.Errorf("MoveSite() with every numbered name taken = %v; want ErrNameTaken", err)
	}
}

// TestNumberedName verifies the names sites get beside one with their name.
func TestNumberedName(t *testing.T) {
	if got := site.NumberedName("Prod", 1); got != "Prod (1)" {
		t.Errorf("NumberedName(Prod, 1) = %q", got)
	}
	long := strings.Repeat("ñ", 100)
	if got := site.NumberedName(long, 12); len([]rune(got)) != 100 || !strings.HasSuffix(got, " (12)") {
		t.Errorf("NumberedName(100 runes, 12) = %d runes %q", len([]rune(got)), got)
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

	noID := site.NewService(f.store, roles{f.editor: workspace.RoleEditor}, keys{}, f.guard, connectors.Builtin(), testutil.NewFakeClock(time.Now()), rand.Reader, func() (uuid.UUID, error) { return uuid.Nil, errStore })
	if _, err := noID.CreateCollection(ctx, f.editor, f.ws, "Y", uuid.Nil); !errors.Is(err, errStore) {
		t.Errorf("CreateCollection() with a failing generator = %v", err)
	}
}

// TestService_CanSeal verifies the check for a session that can store secrets.
//
// Scenarios:
//   - a session with an unlocked vault can seal, a locked one and a failing key provider cannot
func TestService_CanSeal(t *testing.T) {
	f := newFixture(t, &switchable{})
	if !f.svc.CanSeal("open") {
		t.Error("CanSeal() of an unlocked session = false")
	}
	if f.svc.CanSeal("locked") {
		t.Error("CanSeal() of a locked session = true")
	}
	broken := site.NewService(f.store, roles{f.editor: workspace.RoleEditor}, keys{failure: errStore}, f.guard, connectors.Builtin(), testutil.NewFakeClock(time.Now()), rand.Reader, nil)
	if broken.CanSeal("open") {
		t.Error("CanSeal() with a failing key provider = true")
	}
}

// quota is a site quota that answers with a fixed error and counts the questions.
type quota struct {
	err   error
	asked []uuid.UUID
}

// CheckSite records the workspace and returns the error.
func (q *quota) CheckSite(_ context.Context, workspaceID uuid.UUID) error {
	q.asked = append(q.asked, workspaceID)
	return q.err
}

// TestService_Quota verifies that a quota decides whether a workspace may get another site.
//
// Scenarios:
//   - without a quota, and with one that allows, a site is created
//   - a quota that refuses stops the creation before anything is stored, and it is asked about the workspace of the site
//   - people who may not manage sites are refused before the quota is asked
func TestService_Quota(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{}); err != nil {
		t.Fatalf("Create() without a quota = %v", err)
	}
	allowing := &quota{}
	f.svc.UseQuota(allowing)
	other := valid()
	other.Name = "Staging"
	if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", other, site.SecretChanges{}); err != nil || len(allowing.asked) != 1 || allowing.asked[0] != f.ws {
		t.Fatalf("Create() with a quota that allows = %v, asked %v", err, allowing.asked)
	}

	errFull := errors.New("full")
	f.svc.UseQuota(&quota{err: errFull})
	third := valid()
	third.Name = "Third"
	if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", third, site.SecretChanges{}); !errors.Is(err, errFull) || len(f.store.sites) != 2 {
		t.Errorf("Create() with a quota that refuses = %v, %d sites; want the refusal and no new site", err, len(f.store.sites))
	}
	refusing := &quota{err: errFull}
	f.svc.UseQuota(refusing)
	if _, err := f.svc.Create(ctx, f.viewer, f.ws, "open", third, site.SecretChanges{}); !errors.Is(err, workspace.ErrForbidden) || len(refusing.asked) != 0 {
		t.Errorf("a viewer creating = %v, quota asked %d times; want ErrForbidden and no question", err, len(refusing.asked))
	}
}

// TestService_Draft verifies the draft of a site form, tried before it is saved.
//
// Use cases:
//   - a new site gets the secrets typed and an identifier of its own; nothing is saved
//   - an edit keeps the stored secrets left blank, with the settings of the form, and takes the ones typed instead; stored secrets
//     whose field no longer applies are left out
//   - viewers, settings that break the schema, unknown secrets, private hosts, a missing site, sudo for an editor and a connector
//     the instance does not offer are refused as saving refuses them; a locked vault is ErrLocked when a stored secret is needed
func TestService_Draft(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	draft, secrets, err := f.svc.Draft(ctx, f.editor, f.ws, uuid.Nil, "open", valid(), site.SecretChanges{site.SecretPassword: "typed"})
	if err != nil || draft.ID == uuid.Nil || string(secrets[site.SecretPassword]) != "typed" || len(f.store.sites) != 0 {
		t.Fatalf("Draft(new) = %+v, %v, %v with %d sites saved", draft, secrets, err, len(f.store.sites))
	}

	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{site.SecretPassword: "stored"})
	if err != nil {
		t.Fatal(err)
	}
	moved := with(valid(), "Moved", "username", "other")
	draft, secrets, err = f.svc.Draft(ctx, f.editor, f.ws, created.ID, "open", moved, site.SecretChanges{})
	if err != nil || draft.ID != created.ID || draft.Setting("username") != "other" || string(secrets[site.SecretPassword]) != "stored" {
		t.Errorf("Draft(edit, blank password) = %+v, %v, %v; want the stored password with the new settings", draft, secrets, err)
	}
	if _, secrets, _ = f.svc.Draft(ctx, f.editor, f.ws, created.ID, "open", moved, site.SecretChanges{site.SecretPassword: "typed"}); string(secrets[site.SecretPassword]) != "typed" {
		t.Errorf("Draft(edit, typed password) = %v; want the typed one", secrets)
	}
	if _, secrets, _ = f.svc.Draft(ctx, f.editor, f.ws, created.ID, "open", with(valid(), "Ask", "auth", "ask"), site.SecretChanges{}); len(secrets) != 0 {
		t.Errorf("Draft(edit to ask) = %v; want no stored password", secrets)
	}
	if stored := f.store.sites[created.ID]; stored.Setting("username") != "deploy" {
		t.Error("a draft changed the saved site")
	}

	refusals := map[string]struct {
		actor   uuid.UUID
		id      uuid.UUID
		session string
		in      site.Input
		changes site.SecretChanges
		want    error
	}{
		"viewer":         {f.viewer, uuid.Nil, "open", valid(), nil, workspace.ErrForbidden},
		"invalid":        {f.editor, uuid.Nil, "open", with(valid(), "Bad", "host", ""), nil, connectors.ErrInvalidConfig},
		"unknown secret": {f.editor, uuid.Nil, "open", valid(), site.SecretChanges{"token": "x"}, connectors.ErrInvalidConfig},
		"private host":   {f.editor, uuid.Nil, "open", with(valid(), "Private", "host", "private.example"), nil, netguard.ErrBlocked},
		"missing":        {f.editor, uuid.New(), "open", valid(), nil, site.ErrNotFound},
		"sudo":           {f.editor, uuid.Nil, "open", with(valid(), "Root", "sudo", "true"), nil, site.ErrElevation},
		"locked":         {f.editor, created.ID, "closed", moved, site.SecretChanges{"passphrase": "typed"}, site.ErrLocked},
	}
	for name, c := range refusals {
		if _, _, err := f.svc.Draft(ctx, c.actor, f.ws, c.id, c.session, c.in, c.changes); !errors.Is(err, c.want) {
			t.Errorf("%s: Draft() = %v; want %v", name, err, c.want)
		}
	}
	proxied := with(valid(), "Proxied", "proxy_kind", "socks5", "proxy_host", "public.example", "proxy_port", "1080")
	both, err := f.svc.Create(ctx, f.editor, f.ws, "open", proxied, site.SecretChanges{site.SecretPassword: "stored", site.SecretProxyPassword: "proxy"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.Draft(ctx, f.editor, f.ws, both.ID, "closed", proxied, site.SecretChanges{site.SecretPassword: "typed"}); !errors.Is(err, site.ErrLocked) {
		t.Errorf("Draft() keeping a stored proxy password while locked = %v; want ErrLocked", err)
	}
	f.svc.UseOffering(offering{"ftp": true})
	if _, _, err := f.svc.Draft(ctx, f.editor, f.ws, created.ID, "open", ftp("Moved"), site.SecretChanges{}); !errors.Is(err, connectors.ErrDisabled) {
		t.Errorf("Draft() to a disabled connector = %v; want ErrDisabled", err)
	}
}
