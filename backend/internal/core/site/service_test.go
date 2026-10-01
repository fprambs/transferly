package site_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

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
//   - the default remote folder is stored cleaned, the root is stored empty, and a relative, too long, control-character or invalid UTF-8
//     one is refused with ErrInvalidRemoteDir
//   - a known label color is stored and an unknown one is refused with ErrInvalidColor
//   - empty or oversized names, unknown protocols, malformed hosts, ports out of range, empty user names,
//     unknown or protocol-incompatible authentication methods and unknown time zones are each refused with their error
//   - the character set defaults to UTF-8, is case insensitive, and only UTF-8 is accepted for SFTP
//   - key authentication is accepted for SFTP only
//   - hosts that resolve to private addresses are refused with ErrBlocked, unresolvable ones with ErrUnresolvable
//   - a proxy and a jump host are trimmed and validated the same way, refused on an invalid kind, host, port or
//     username, a jump host is refused outside SFTP, and both are checked against the network guard like any host
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
	if created.Charset != "utf-8" {
		t.Errorf("default charset = %q", created.Charset)
	}
	if legacy, err := f.svc.Create(ctx, f.editor, f.ws, "open", site.Input{Name: "Legacy", Protocol: site.FTP, Host: "public.example", Username: "me", Charset: " ISO-8859-1 "}, site.SecretChanges{}); err != nil || legacy.Charset != "iso-8859-1" {
		t.Errorf("legacy charset = %+v, %v", legacy, err)
	}
	if sftp, err := f.svc.Create(ctx, f.editor, f.ws, "open", func() site.Input { in := valid(); in.Name = "Key"; in.AuthMethod = site.AuthKey; return in }(), site.SecretChanges{}); err != nil || sftp.Port != 22 {
		t.Errorf("SFTP with key = %+v, %v", sftp, err)
	}
	if capped, err := f.svc.Create(ctx, f.editor, f.ws, "open", func() site.Input { in := valid(); in.Name = "Capped"; in.SpeedLimit = 65536; return in }(), site.SecretChanges{}); err != nil || capped.SpeedLimit != 65536 {
		t.Errorf("site with a speed limit = %+v, %v", capped, err)
	}
	if labeled, err := f.svc.Create(ctx, f.editor, f.ws, "open", func() site.Input { in := valid(); in.Name = "Labeled"; in.Color = "red"; return in }(), site.SecretChanges{}); err != nil || labeled.Color != "red" {
		t.Errorf("site with a color = %+v, %v", labeled, err)
	}
	for dir, want := range map[string]string{" /var/www/./html/ ": "/var/www/html", "/": "", "": ""} {
		in := valid()
		in.Name, in.RemoteDir = "Dir "+uuid.NewString(), dir
		if got, err := f.svc.Create(ctx, f.editor, f.ws, "open", in, site.SecretChanges{}); err != nil || got.RemoteDir != want {
			t.Errorf("remote folder %q = %q, %v; want %q", dir, got.RemoteDir, err, want)
		}
	}
	proxied, err := f.svc.Create(ctx, f.editor, f.ws, "open", func() site.Input {
		in := valid()
		in.Name = "Proxied"
		in.Proxy = site.Proxy{Kind: site.ProxySOCKS5, Host: " public.example ", Port: 1080, Username: " proxyuser "}
		return in
	}(), site.SecretChanges{})
	if err != nil || proxied.Proxy != (site.Proxy{Kind: site.ProxySOCKS5, Host: "public.example", Port: 1080, Username: "proxyuser"}) {
		t.Errorf("site with a proxy = %+v, %v", proxied, err)
	}
	jumped, err := f.svc.Create(ctx, f.editor, f.ws, "open", func() site.Input {
		in := valid()
		in.Name = "Jumped"
		in.Jump = site.Jump{Host: " public.example ", Username: " bastion "}
		return in
	}(), site.SecretChanges{})
	if err != nil || jumped.Jump != (site.Jump{Host: "public.example", Port: 22, Username: "bastion", AuthMethod: site.AuthPassword}) {
		t.Errorf("site with a jump host = %+v, %v", jumped, err)
	}

	tests := []struct {
		name   string
		mutate func(*site.Input)
		want   error
	}{
		{"empty name", func(in *site.Input) { in.Name = "  " }, site.ErrInvalidName},
		{"long name", func(in *site.Input) { in.Name = strings.Repeat("n", 101) }, site.ErrInvalidName},
		{"unknown protocol", func(in *site.Input) { in.Protocol = "gopher" }, site.ErrInvalidProtocol},
		{"unknown charset", func(in *site.Input) { in.Protocol = site.FTP; in.Charset = "klingon" }, site.ErrInvalidCharset},
		{"legacy charset on SFTP", func(in *site.Input) { in.Charset = "iso-8859-1" }, site.ErrInvalidCharset},
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
		{"negative speed limit", func(in *site.Input) { in.SpeedLimit = -1 }, site.ErrInvalidSpeedLimit},
		{"speed limit below the smallest cap", func(in *site.Input) { in.SpeedLimit = 1023 }, site.ErrInvalidSpeedLimit},
		{"speed limit above the largest cap", func(in *site.Input) { in.SpeedLimit = 1<<40 + 1 }, site.ErrInvalidSpeedLimit},
		{"relative remote folder", func(in *site.Input) { in.RemoteDir = "var/www" }, site.ErrInvalidRemoteDir},
		{"unknown color", func(in *site.Input) { in.Color = "purple" }, site.ErrInvalidColor},
		{"remote folder with a control character", func(in *site.Input) { in.RemoteDir = "/var/\nwww" }, site.ErrInvalidRemoteDir},
		{"remote folder too long", func(in *site.Input) { in.RemoteDir = "/" + strings.Repeat("d", 4096) }, site.ErrInvalidRemoteDir},
		{"remote folder not UTF-8", func(in *site.Input) { in.RemoteDir = "/bad\xff" }, site.ErrInvalidRemoteDir},
		{"private host", func(in *site.Input) { in.Host = "private.example" }, netguard.ErrBlocked},
		{"private address", func(in *site.Input) { in.Host = "192.168.1.1" }, netguard.ErrBlocked},
		{"unresolvable host", func(in *site.Input) { in.Host = "nowhere.example" }, netguard.ErrUnresolvable},
		{"unknown proxy kind", func(in *site.Input) { in.Proxy = site.Proxy{Kind: "wireguard", Host: "public.example", Port: 1080} }, site.ErrInvalidProxy},
		{"proxy with a bad host", func(in *site.Input) { in.Proxy = site.Proxy{Kind: site.ProxyHTTP, Host: "proxy://x", Port: 8080} }, site.ErrInvalidProxy},
		{"proxy with a bad port", func(in *site.Input) { in.Proxy = site.Proxy{Kind: site.ProxyHTTP, Host: "public.example", Port: 0} }, site.ErrInvalidProxy},
		{"proxy on a private host", func(in *site.Input) {
			in.Proxy = site.Proxy{Kind: site.ProxySOCKS5, Host: "private.example", Port: 1080}
		}, netguard.ErrBlocked},
		{"jump host over FTP", func(in *site.Input) {
			in.Protocol, in.Jump = site.FTP, site.Jump{Host: "public.example", Username: "bastion"}
		}, site.ErrInvalidJump},
		{"jump host with a bad host", func(in *site.Input) { in.Jump = site.Jump{Host: "ssh://x", Username: "bastion"} }, site.ErrInvalidJump},
		{"jump host with a bad port", func(in *site.Input) { in.Jump = site.Jump{Host: "public.example", Port: -1, Username: "bastion"} }, site.ErrInvalidJump},
		{"jump host with no username", func(in *site.Input) { in.Jump = site.Jump{Host: "public.example"} }, site.ErrInvalidJump},
		{"jump host with an unknown auth method", func(in *site.Input) {
			in.Jump = site.Jump{Host: "public.example", Username: "bastion", AuthMethod: "token"}
		}, site.ErrInvalidJump},
		{"jump host on a private host", func(in *site.Input) { in.Jump = site.Jump{Host: "private.example", Username: "bastion"} }, netguard.ErrBlocked},
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

// TestService_AskForPassword covers the [site.AuthAsk] logon type of [site.Service.Create] and [site.Service.Update].
//
// Use cases:
//   - every protocol may ask for its password when connecting
//   - a site that asks never stores a password, even when the request sends one, and needs no unlocked vault for it
//   - switching a saved site to ask removes the password it had
func TestService_AskForPassword(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	for _, protocol := range []site.Protocol{site.SFTP, site.FTP, site.FTPS} {
		in := valid()
		in.Name, in.Protocol, in.AuthMethod = "Ask "+string(protocol), protocol, site.AuthAsk
		created, err := f.svc.Create(ctx, f.editor, f.ws, "closed", in, site.SecretChanges{Password: text("typed")})
		if err != nil {
			t.Fatalf("Create(%s, ask) = %v", protocol, err)
		}
		if created.AuthMethod != site.AuthAsk || len(created.Secrets) != 0 {
			t.Errorf("Create(%s, ask) = %s with secrets %v; want ask with none", protocol, created.AuthMethod, created.Secrets)
		}
	}

	stored, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{Password: text("kept")})
	if err != nil {
		t.Fatal(err)
	}
	switched := valid()
	switched.AuthMethod = site.AuthAsk
	updated, err := f.svc.Update(ctx, f.editor, f.ws, stored.ID, "closed", switched, site.SecretChanges{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Secrets) != 0 {
		t.Errorf("a site switched to ask kept %v", updated.Secrets)
	}
	if _, ok := f.store.secrets[stored.ID.String()+string(site.SecretPassword)]; ok {
		t.Error("the password of a site switched to ask is still stored")
	}
}

// TestService_Anonymous covers the [site.AuthAnonymous] logon type of [site.Service.Create] and [site.Service.Update].
//
// Use cases:
//   - SFTP and FTP sites may log in anonymously, always as the anonymous user whatever name the request sends
//   - an anonymous site stores no secret, even when the request sends some, and needs no unlocked vault for it
//   - switching a site that logs in with a key to anonymous removes its private key and passphrase
func TestService_Anonymous(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	for _, protocol := range []site.Protocol{site.SFTP, site.FTP} {
		in := valid()
		in.Name, in.Protocol, in.Username, in.AuthMethod = "Anonymous "+string(protocol), protocol, "someone", site.AuthAnonymous
		created, err := f.svc.Create(ctx, f.editor, f.ws, "closed", in, site.SecretChanges{Password: text("typed")})
		if err != nil {
			t.Fatalf("Create(%s, anonymous) = %v", protocol, err)
		}
		if created.AuthMethod != site.AuthAnonymous || created.Username != site.AnonymousUser || len(created.Secrets) != 0 {
			t.Errorf("Create(%s, anonymous) = %s as %q with secrets %v; want anonymous as anonymous with none", protocol, created.AuthMethod, created.Username, created.Secrets)
		}
	}

	keyed := valid()
	keyed.Name, keyed.AuthMethod = "Keyed", site.AuthKey
	stored, err := f.svc.Create(ctx, f.editor, f.ws, "open", keyed, site.SecretChanges{PrivateKey: text(testutil.PrivateKey(t, "phrase")), Passphrase: text("phrase")})
	if err != nil {
		t.Fatal(err)
	}
	keyed.AuthMethod = site.AuthAnonymous
	updated, err := f.svc.Update(ctx, f.editor, f.ws, stored.ID, "closed", keyed, site.SecretChanges{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Secrets) != 0 {
		t.Errorf("a site switched to anonymous kept %v", updated.Secrets)
	}
}

// TestService_Interactive covers the [site.AuthInteractive] logon type of [site.Service.Create] and [site.Service.Update]
// (ADR-0016).
//
// Use cases:
//   - SFTP and FTP sites may ask whatever the server asks when connecting, keeping the username the request sends
//   - an interactive site stores no secret, even when the request sends some, and needs no unlocked vault for it
//   - switching a site with a stored password to interactive removes the password
func TestService_Interactive(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	for _, protocol := range []site.Protocol{site.SFTP, site.FTP} {
		in := valid()
		in.Name, in.Protocol, in.Username, in.AuthMethod = "Interactive "+string(protocol), protocol, "someone", site.AuthInteractive
		created, err := f.svc.Create(ctx, f.editor, f.ws, "closed", in, site.SecretChanges{Password: text("typed")})
		if err != nil {
			t.Fatalf("Create(%s, interactive) = %v", protocol, err)
		}
		if created.AuthMethod != site.AuthInteractive || created.Username != "someone" || len(created.Secrets) != 0 {
			t.Errorf("Create(%s, interactive) = %s as %q with secrets %v", protocol, created.AuthMethod, created.Username, created.Secrets)
		}
	}

	stored, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{Password: text("secret")})
	if err != nil {
		t.Fatal(err)
	}
	switched := valid()
	switched.AuthMethod = site.AuthInteractive
	updated, err := f.svc.Update(ctx, f.editor, f.ws, stored.ID, "closed", switched, site.SecretChanges{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Secrets) != 0 {
		t.Errorf("a site switched to interactive kept %v", updated.Secrets)
	}
}

// TestService_PrivateKeys covers how [site.Service.Create] and [site.Service.Update] read the private keys they save (4.8g).
//
// Use cases:
//   - a PuTTY key is stored converted to OpenSSH's format, which connecting reads with the same passphrase
//   - a key that is not encrypted drops the passphrase sent with it
//   - a public key, an encrypted key without its passphrase or with a wrong one, and text that is no key are refused
//     with a [*site.KeyError] that says which key, the site's or the jump host's
//   - a key larger than any key file is refused as too large before it is read
//   - an update that leaves the key as it is does not read it, and one that replaces it reads the new one
func TestService_PrivateKeys(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	ppk, err := os.ReadFile("../sshkey/testdata/ppk3-rsa-argon2id.ppk")
	if err != nil {
		t.Fatal(err)
	}
	keyed := valid()
	keyed.Name, keyed.AuthMethod = "PuTTY", site.AuthKey
	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", keyed, site.SecretChanges{PrivateKey: text(string(ppk)), Passphrase: text("correct horse")})
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

	plain := keyed
	plain.Name = "Plain"
	unencrypted, err := f.svc.Create(ctx, f.editor, f.ws, "open", plain, site.SecretChanges{PrivateKey: text(testutil.PrivateKey(t, "")), Passphrase: text("stale")})
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
	withJump := keyed
	withJump.Name, withJump.Jump = "Jump", site.Jump{Host: "bastion.example", Port: 22, Username: "bastion", AuthMethod: site.AuthKey}
	refusals := []struct {
		name    string
		in      site.Input
		changes site.SecretChanges
		jump    bool
		want    error
	}{
		{"public key", keyed, site.SecretChanges{PrivateKey: text(string(ssh.MarshalAuthorizedKey(public)))}, false, sshkey.ErrPublicKey},
		{"no passphrase", keyed, site.SecretChanges{PrivateKey: text(encrypted)}, false, sshkey.ErrPassphraseRequired},
		{"not a key", keyed, site.SecretChanges{PrivateKey: text("hello")}, false, sshkey.ErrUnreadable},
		{"jump wrong passphrase", withJump, site.SecretChanges{PrivateKey: text(encrypted), Passphrase: text("phrase"),
			JumpPrivateKey: text(encrypted), JumpPassphrase: text("wrong")}, true, sshkey.ErrWrongPassphrase},
	}
	for _, r := range refusals {
		r.in.Name = "Refused " + r.name
		_, err := f.svc.Create(ctx, f.editor, f.ws, "open", r.in, r.changes)
		var keyErr *site.KeyError
		prefix := "private key: "
		if r.jump {
			prefix = "jump host private key: "
		}
		if !errors.As(err, &keyErr) || keyErr.Jump != r.jump || !errors.Is(err, r.want) || !strings.HasPrefix(err.Error(), prefix) {
			t.Errorf("Create(%s) = %v; want a key error of the jump host %v for %v", r.name, err, r.jump, r.want)
		}
	}
	huge := strings.Repeat("A", 64*1024+1)
	if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", keyed, site.SecretChanges{PrivateKey: &huge}); !errors.Is(err, site.ErrSecretTooLarge) {
		t.Errorf("Create() with a huge key = %v; want ErrSecretTooLarge", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", keyed, site.SecretChanges{Passphrase: text("changed")}); err != nil {
		t.Errorf("Update() keeping the key = %v", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", keyed, site.SecretChanges{PrivateKey: text("hello")}); !errors.Is(err, sshkey.ErrUnreadable) {
		t.Errorf("Update() with text that is no key = %v; want ErrUnreadable", err)
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
	broken := site.NewService(f.store, roles{f.editor: workspace.RoleEditor}, keys{failure: errStore}, f.guard, testutil.NewFakeClock(time.Now()), rand.Reader, nil)
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

// TestNormalize_FTPEncryption covers how [site.Normalize] settles the FTP encryption of a site (ADR-0012).
//
// Use cases:
//   - SFTP takes no encryption and refuses one
//   - the legacy FTPS protocol becomes FTP that requires TLS: implicit on port 990, explicit on any other, and it refuses an
//     encryption that does not require TLS
//   - FTP without an encryption keeps clear text, takes any known one, and refuses an unknown one
//   - an implicit site without a port gets port 990, any other FTP site port 21
func TestNormalize_FTPEncryption(t *testing.T) {
	tests := []struct {
		name       string
		protocol   site.Protocol
		port       int
		encryption site.FTPEncryption
		want       site.FTPEncryption
		wantPort   int
		err        error
	}{
		{"sftp", site.SFTP, 0, "", "", 22, nil},
		{"sftp with an encryption", site.SFTP, 0, site.EncryptionExplicit, "", 0, site.ErrInvalidEncryption},
		{"legacy ftps on 990", site.FTPS, 990, "", site.EncryptionImplicit, 990, nil},
		{"legacy ftps elsewhere", site.FTPS, 0, "", site.EncryptionExplicit, 21, nil},
		{"legacy ftps asked for implicit", site.FTPS, 0, site.EncryptionImplicit, site.EncryptionImplicit, 990, nil},
		{"legacy ftps asked for plain", site.FTPS, 0, site.EncryptionPlain, "", 0, site.ErrInvalidEncryption},
		{"ftp without one", site.FTP, 0, "", site.EncryptionPlain, 21, nil},
		{"ftp optional", site.FTP, 0, site.EncryptionOptional, site.EncryptionOptional, 21, nil},
		{"ftp implicit on its own port", site.FTP, 2990, site.EncryptionImplicit, site.EncryptionImplicit, 2990, nil},
		{"ftp unknown", site.FTP, 0, "tls", "", 0, site.ErrInvalidEncryption},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid()
			in.Protocol, in.Port, in.FTPEncryption = tt.protocol, tt.port, tt.encryption
			got, err := site.Normalize(in)
			if !errors.Is(err, tt.err) {
				t.Fatalf("Normalize() = %v; want %v", err, tt.err)
			}
			if err != nil {
				return
			}
			if got.FTPEncryption != tt.want || got.Port != tt.wantPort || (tt.protocol != site.SFTP && got.Protocol != site.FTP) {
				t.Errorf("Normalize() = %s %s port %d; want %s port %d", got.Protocol, got.FTPEncryption, got.Port, tt.want, tt.wantPort)
			}
		})
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
	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", valid(), site.SecretChanges{Password: text("pw")})
	if err != nil {
		t.Fatal(err)
	}

	moved, err := f.svc.MoveSite(ctx, f.editor, f.ws, created.ID, collection.ID)
	if err != nil || moved.CollectionID != collection.ID || moved.Host != created.Host {
		t.Fatalf("MoveSite() into a collection = %+v, %v", moved, err)
	}
	stored := f.store.sites[created.ID]
	stored.Host = "gone.example"
	f.store.sites[created.ID] = stored
	back, err := f.svc.MoveSite(ctx, f.editor, f.ws, created.ID, uuid.Nil)
	if err != nil || back.CollectionID != uuid.Nil || back.Host != "gone.example" {
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
}

// TestService_Account verifies the account logon type of FTP sites (4.8e).
//
// Scenarios:
//   - an FTP site keeps its password and its account, sealed like every secret
//   - the account logon type is refused for SFTP
//   - a site switched to another logon type drops its account
func TestService_Account(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &switchable{})
	in := valid()
	in.Name, in.Protocol, in.AuthMethod = "Billed", site.FTP, site.AuthAccount
	created, err := f.svc.Create(ctx, f.editor, f.ws, "open", in, site.SecretChanges{Password: text("pw"), Account: text("billing")})
	if err != nil {
		t.Fatalf("Create(account) = %v", err)
	}
	if created.AuthMethod != site.AuthAccount || !slices.Contains(created.Secrets, site.SecretAccount) || !slices.Contains(created.Secrets, site.SecretPassword) {
		t.Errorf("Create(account) = %s with secrets %v", created.AuthMethod, created.Secrets)
	}

	sftp := valid()
	sftp.Name, sftp.AuthMethod = "Secure", site.AuthAccount
	if _, err := f.svc.Create(ctx, f.editor, f.ws, "open", sftp, site.SecretChanges{}); !errors.Is(err, site.ErrInvalidAuthMethod) {
		t.Errorf("Create(SFTP, account) = %v; want ErrInvalidAuthMethod", err)
	}

	in.AuthMethod = site.AuthPassword
	if _, err := f.svc.Update(ctx, f.editor, f.ws, created.ID, "open", in, site.SecretChanges{}); err != nil {
		t.Fatal(err)
	}
	_, account := f.store.secrets[created.ID.String()+string(site.SecretAccount)]
	_, password := f.store.secrets[created.ID.String()+string(site.SecretPassword)]
	if account || !password {
		t.Errorf("a site switched to the normal logon kept the account %v and the password %v; want only the password", account, password)
	}
}
