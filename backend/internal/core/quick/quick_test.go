package quick_test

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/quick"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/sshkey"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/testutil"
)

// errBoom is the failure injected by the fakes.
var errBoom = errors.New("boom")

// roles answers authorization from a table of roles by user, using the real permission matrix.
type roles map[uuid.UUID]workspace.Role

// Authorize applies the permission matrix.
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

// saver settles connections with the real site rules and records what it was asked to save.
type saver struct {
	*site.Service
	input   site.Input
	changes site.SecretChanges
	calls   int
	err     error
}

// Create records the request and returns a site or the configured error.
func (s *saver) Create(_ context.Context, actor, ws uuid.UUID, _ string, in site.Input, changes site.SecretChanges) (site.Site, error) {
	s.calls++
	s.input, s.changes = in, changes
	if s.err != nil {
		return site.Site{}, s.err
	}
	return site.Site{ID: uuid.New(), WorkspaceID: ws, Name: in.Name, Connector: in.Connector, Config: in.Config, CollectionID: in.CollectionID}, nil
}

// newSaver returns a saver whose network guard allows the given public range only.
func newSaver(allowed ...string) *saver {
	prefixes := make([]netip.Prefix, len(allowed))
	for i, p := range allowed {
		prefixes[i] = netip.MustParsePrefix(p)
	}
	guard := netguard.NewPolicy(prefixes, nil)
	return &saver{Service: site.NewService(nil, nil, nil, guard, connectors.Builtin(), testutil.NewFakeClock(time.Now()), nil, nil)}
}

// fixture bundles a service with its fakes.
type fixture struct {
	svc    *quick.Service
	saver  *saver
	clock  *testutil.FakeClock
	ws     uuid.UUID
	editor uuid.UUID
	viewer uuid.UUID
}

// newFixture returns a service where the editor may create quick connections and the viewer may only list them.
func newFixture() *fixture {
	f := &fixture{saver: newSaver("203.0.113.0/24"), clock: testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), ws: uuid.New(), editor: uuid.New(), viewer: uuid.New()}
	f.svc = quick.NewService(roles{f.editor: workspace.RoleEditor, f.viewer: workspace.RoleViewer}, f.saver, f.clock, nil)
	return f
}

// input returns valid SFTP settings for a server, with some settings changed, given as name and value pairs.
func input(host string, settings ...string) site.Input {
	config := map[string]string{"host": host, "username": "deploy"}
	for i := 0; i+1 < len(settings); i += 2 {
		config[settings[i]] = settings[i+1]
	}
	return site.Input{Connector: "sftp", Config: config}
}

// secrets returns a fresh password, so tests can watch what happens to the memory behind it.
func secrets() quick.Secrets {
	return quick.Secrets{site.SecretPassword: []byte("hunter2-hunter2")}
}

// allZero reports whether every byte of a slice is zero.
func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// TestService_Create verifies starting quick connections.
//
// Scenarios:
//   - a valid connection is named after its host, gets the defaults and an identifier, and is kept
//   - viewers and strangers cannot create
//   - malformed settings, forbidden destinations and asking for the password or anonymous logon (neither offered for
//     quick connections) are refused with their errors and nothing is kept
//   - a user may keep five connections at once, others are unaffected, and closing one makes room
//   - a failing identifier generator is returned
func TestService_Create(t *testing.T) {
	ctx := context.Background()
	f := newFixture()

	c, err := f.svc.Create(ctx, f.editor, f.ws, input("203.0.113.5"), secrets())
	if err != nil || c.Site.Name != "203.0.113.5" || c.Site.Setting("port") != "22" || c.Site.Setting("auth") != "password" || c.Site.Connector != "sftp" ||
		c.Site.ID == uuid.Nil || c.UserID != f.editor || c.Site.WorkspaceID != f.ws {
		t.Fatalf("Create() = %+v, %v", c, err)
	}
	if _, err := f.svc.Create(ctx, f.viewer, f.ws, input("203.0.113.5"), secrets()); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer Create() = %v; want ErrForbidden", err)
	}
	if _, err := f.svc.Create(ctx, uuid.New(), f.ws, input("203.0.113.5"), secrets()); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger Create() = %v; want workspace.ErrNotFound", err)
	}

	bad := input("203.0.113.5")
	bad.Connector = "gopher"
	if _, err := f.svc.Create(ctx, f.editor, f.ws, bad, secrets()); !errors.Is(err, connectors.ErrInvalidConfig) {
		t.Errorf("unknown connector = %v; want ErrInvalidConfig", err)
	}
	if _, err := f.svc.Create(ctx, f.editor, f.ws, input("10.0.0.1"), secrets()); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("private host = %v; want ErrBlocked", err)
	}
	keyOverFTP := input("203.0.113.5", "auth", "key")
	keyOverFTP.Connector = "ftp"
	if _, err := f.svc.Create(ctx, f.editor, f.ws, keyOverFTP, secrets()); !errors.Is(err, connectors.ErrInvalidConfig) {
		t.Errorf("key over FTP = %v", err)
	}
	if _, err := f.svc.Create(ctx, f.editor, f.ws, input("203.0.113.5"), quick.Secrets{"token": []byte("x")}); !errors.Is(err, connectors.ErrInvalidConfig) {
		t.Errorf("unknown secret = %v; want ErrInvalidConfig", err)
	}
	for _, auth := range []string{"ask", "anonymous", "interactive"} {
		var field *connectors.FieldError
		if _, err := f.svc.Create(ctx, f.editor, f.ws, input("203.0.113.5", "auth", auth), secrets()); !errors.As(err, &field) || field.Field != "auth" {
			t.Errorf("%s quick connection = %v; want a field error of auth", auth, err)
		}
	}
	if list, _ := f.svc.List(ctx, f.editor, f.ws); len(list) != 1 {
		t.Errorf("%d connections kept after refusals; want 1", len(list))
	}

	for i := 0; i < 4; i++ {
		if _, err := f.svc.Create(ctx, f.editor, f.ws, input("203.0.113.6"), secrets()); err != nil {
			t.Fatalf("connection %d: %v", i+2, err)
		}
	}
	if _, err := f.svc.Create(ctx, f.editor, f.ws, input("203.0.113.7"), secrets()); !errors.Is(err, quick.ErrTooMany) {
		t.Errorf("sixth connection = %v; want ErrTooMany", err)
	}
	if err := f.svc.Close(f.editor, f.ws, c.Site.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Create(ctx, f.editor, f.ws, input("203.0.113.7"), secrets()); err != nil {
		t.Errorf("after closing one = %v", err)
	}

	broken := quick.NewService(roles{f.editor: workspace.RoleEditor}, newSaver(), f.clock, func() (uuid.UUID, error) { return uuid.Nil, errBoom })
	if _, err := broken.Create(ctx, f.editor, f.ws, input("8.8.8.8"), secrets()); !errors.Is(err, errBoom) {
		t.Errorf("failing generator = %v", err)
	}
}

// TestService_CreateProxyAndJump verifies that a quick connection's proxy and jump host, when given, are kept and
// checked against the network guard exactly like the connection's own host.
//
// Scenarios:
//   - a quick connection with a proxy is created with it, and one whose proxy is on a private host is refused
//   - a quick connection with a jump host is created with it, and one whose jump host is on a private host is refused
func TestService_CreateProxyAndJump(t *testing.T) {
	ctx := context.Background()
	f := newFixture()

	proxied := input("203.0.113.10", "proxy_kind", "socks5", "proxy_host", "203.0.113.11")
	if c, err := f.svc.Create(ctx, f.editor, f.ws, proxied, secrets()); err != nil || c.Site.Setting("proxy_host") != "203.0.113.11" || c.Site.Setting("proxy_port") != "1080" {
		t.Errorf("Create() with a proxy = %+v, %v", c, err)
	}
	blockedProxy := input("203.0.113.10", "proxy_kind", "socks5", "proxy_host", "10.0.0.2")
	if _, err := f.svc.Create(ctx, f.editor, f.ws, blockedProxy, secrets()); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("Create() with a proxy on a private host = %v; want ErrBlocked", err)
	}
	jumped := input("203.0.113.10", "jump_host", "203.0.113.12", "jump_username", "bastion")
	if c, err := f.svc.Create(ctx, f.editor, f.ws, jumped, secrets()); err != nil || c.Site.Setting("jump_host") != "203.0.113.12" {
		t.Errorf("Create() with a jump host = %+v, %v", c, err)
	}
	blockedJump := input("203.0.113.10", "jump_host", "10.0.0.3", "jump_username", "bastion")
	if _, err := f.svc.Create(ctx, f.editor, f.ws, blockedJump, secrets()); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("Create() with a jump host on a private host = %v; want ErrBlocked", err)
	}
}

// TestService_LookupListClose verifies reading and ending connections, and what happens to their secrets.
//
// Scenarios:
//   - Lookup returns the connection with copies of its secrets, so changing them does not change what is kept, and
//     Describe returns its settings without them
//   - other users, other workspaces and unknown identifiers are not found
//   - List returns the user's connections of the workspace, oldest first, without secrets, and needs permission
//   - Close wipes the secrets in memory, and closing twice or a foreign connection is ErrNotFound
//   - CloseUser ends and wipes every connection of the user and reports their identifiers, others stay
func TestService_LookupListClose(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	kept := secrets()
	first, err := f.svc.Create(ctx, f.editor, f.ws, input("203.0.113.5"), kept)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(time.Minute)
	second, err := f.svc.Create(ctx, f.editor, f.ws, input("203.0.113.6"), secrets())
	if err != nil {
		t.Fatal(err)
	}
	otherWS := uuid.New()
	otherUser := uuid.New()
	other := quick.NewService(roles{otherUser: workspace.RoleEditor}, newSaver("8.8.8.0/24"), f.clock, nil)
	theirs, err := other.Create(ctx, otherUser, otherWS, input("8.8.8.8"), secrets())
	if err != nil {
		t.Fatal(err)
	}

	got, ok := f.svc.Lookup(f.editor, f.ws, first.Site.ID)
	if !ok || string(got.Secrets[site.SecretPassword]) != "hunter2-hunter2" || got.Site.Setting("host") != "203.0.113.5" {
		t.Fatalf("Lookup() = %+v, %v", got, ok)
	}
	described, ok := f.svc.Describe(f.editor, f.ws, first.Site.ID)
	if !ok || described.Setting("host") != "203.0.113.5" {
		t.Errorf("Describe() = %+v, %v", described, ok)
	}
	if _, ok := f.svc.Describe(uuid.New(), f.ws, first.Site.ID); ok {
		t.Error("another user described the connection")
	}
	got.Secrets[site.SecretPassword][0] = 'X'
	if again, _ := f.svc.Lookup(f.editor, f.ws, first.Site.ID); string(again.Secrets[site.SecretPassword]) != "hunter2-hunter2" {
		t.Error("changing a looked up secret changed the stored one")
	}
	if _, ok := f.svc.Lookup(uuid.New(), f.ws, first.Site.ID); ok {
		t.Error("another user found the connection")
	}
	if _, ok := f.svc.Lookup(f.editor, otherWS, first.Site.ID); ok {
		t.Error("the connection was found through another workspace")
	}
	if _, ok := f.svc.Lookup(f.editor, f.ws, uuid.New()); ok {
		t.Error("an unknown identifier was found")
	}
	if _, ok := f.svc.Lookup(f.editor, f.ws, theirs.Site.ID); ok {
		t.Error("a connection of another user was found")
	}

	list, err := f.svc.List(ctx, f.editor, f.ws)
	if err != nil || len(list) != 2 || list[0].Site.ID != first.Site.ID || list[1].Site.ID != second.Site.ID {
		t.Fatalf("List() = %+v, %v; want the two connections, oldest first", list, err)
	}
	if len(list[0].Secrets) != 0 {
		t.Error("List() exposes secrets")
	}
	if none, _ := f.svc.List(ctx, f.viewer, f.ws); len(none) != 0 {
		t.Errorf("a viewer lists %d connections of others", len(none))
	}
	if _, err := f.svc.List(ctx, uuid.New(), f.ws); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger List() = %v", err)
	}

	if err := f.svc.Close(uuid.New(), f.ws, first.Site.ID); !errors.Is(err, quick.ErrNotFound) {
		t.Errorf("Close() by another user = %v; want ErrNotFound", err)
	}
	if err := f.svc.Close(f.editor, f.ws, first.Site.ID); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if !allZero(kept[site.SecretPassword]) {
		t.Error("closing left the secrets in memory")
	}
	if err := f.svc.Close(f.editor, f.ws, first.Site.ID); !errors.Is(err, quick.ErrNotFound) {
		t.Errorf("second Close() = %v; want ErrNotFound", err)
	}

	ids := f.svc.CloseUser(f.editor)
	if len(ids) != 1 || ids[0] != second.Site.ID {
		t.Errorf("CloseUser() = %v; want the second connection", ids)
	}
	if _, ok := f.svc.Lookup(f.editor, f.ws, second.Site.ID); ok {
		t.Error("a connection survived CloseUser")
	}
	if _, ok := other.Lookup(otherUser, otherWS, theirs.Site.ID); !ok {
		t.Error("CloseUser ended another user's connection")
	}
	if len(f.svc.CloseUser(uuid.New())) != 0 {
		t.Error("CloseUser() of a user without connections returned identifiers")
	}
}

// TestService_Sweep verifies expiry of unattended connections.
//
// Scenarios:
//   - connections unused for the idle time are ended and wiped, used ones survive
func TestService_Sweep(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	stale, used := secrets(), secrets()
	old, _ := f.svc.Create(ctx, f.editor, f.ws, input("203.0.113.5"), stale)
	busy, _ := f.svc.Create(ctx, f.editor, f.ws, input("203.0.113.6"), used)

	f.clock.Advance(20 * time.Minute)
	f.svc.Lookup(f.editor, f.ws, busy.Site.ID)
	f.clock.Advance(20 * time.Minute)
	if ended := f.svc.Sweep(30 * time.Minute); ended != 1 {
		t.Fatalf("Sweep() ended %d; want 1", ended)
	}
	if _, ok := f.svc.Lookup(f.editor, f.ws, old.Site.ID); ok || !allZero(stale[site.SecretPassword]) {
		t.Error("the unused connection survived or was not wiped")
	}
	if _, ok := f.svc.Lookup(f.editor, f.ws, busy.Site.ID); !ok || allZero(used[site.SecretPassword]) {
		t.Error("the used connection was ended")
	}
}

// TestService_Save verifies turning a quick connection into a site.
//
// Scenarios:
//   - saving passes the settings, the name, the collection and only the secrets that exist, then ends the quick connection
//   - a private key is read when the connection is made: one that is not encrypted drops its passphrase, whose bytes are wiped at once, and
//     text that is no key is refused with a key error
//   - a failure such as a locked vault keeps the quick connection for another try
//   - unknown and foreign connections are ErrNotFound
func TestService_Save(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	in := input("203.0.113.5", "port", "2222", "auth", "key")
	key := testutil.PrivateKey(t, "")
	stale := []byte("stale")
	c, err := f.svc.Create(ctx, f.editor, f.ws, in, quick.Secrets{site.SecretPrivateKey: []byte(key), site.SecretPassphrase: stale, site.SecretPassword: []byte{}})
	if err != nil {
		t.Fatal(err)
	}
	if !allZero(stale) {
		t.Error("the passphrase the key does not need was not wiped")
	}
	collection := uuid.New()
	var keyErr *site.KeyError
	if _, err := f.svc.Create(ctx, f.editor, f.ws, in, quick.Secrets{site.SecretPrivateKey: []byte("KEY")}); !errors.As(err, &keyErr) || !errors.Is(err, sshkey.ErrUnreadable) {
		t.Errorf("Create() with text that is no key = %v; want a key error", err)
	}

	f.saver.err = site.ErrLocked
	if _, err := f.svc.Save(ctx, f.editor, f.ws, c.Site.ID, "s", "Production", collection); !errors.Is(err, site.ErrLocked) {
		t.Fatalf("Save() with a locked vault = %v", err)
	}
	if _, ok := f.svc.Lookup(f.editor, f.ws, c.Site.ID); !ok {
		t.Fatal("a failed save ended the quick connection")
	}

	f.saver.err = nil
	saved, err := f.svc.Save(ctx, f.editor, f.ws, c.Site.ID, "s", "Production", collection)
	if err != nil || saved.Name != "Production" || saved.CollectionID != collection {
		t.Fatalf("Save() = %+v, %v", saved, err)
	}
	if f.saver.input.Config["host"] != "203.0.113.5" || f.saver.input.Config["port"] != "2222" || f.saver.input.Config["auth"] != "key" || f.saver.input.Name != "Production" {
		t.Errorf("saved input = %+v", f.saver.input)
	}
	ch := f.saver.changes
	if len(ch) != 1 || ch[site.SecretPrivateKey] != strings.TrimSpace(key) {
		t.Errorf("secret changes = %+v; want only the private key, without the passphrase it does not need", ch)
	}
	if _, ok := f.svc.Lookup(f.editor, f.ws, c.Site.ID); ok {
		t.Error("the quick connection survived being saved")
	}
	if _, err := f.svc.Save(ctx, f.editor, f.ws, c.Site.ID, "s", "Again", uuid.Nil); !errors.Is(err, quick.ErrNotFound) {
		t.Errorf("second Save() = %v; want ErrNotFound", err)
	}
	if _, err := f.svc.Save(ctx, uuid.New(), f.ws, c.Site.ID, "s", "X", uuid.Nil); !errors.Is(err, quick.ErrNotFound) {
		t.Errorf("Save() by another user = %v; want ErrNotFound", err)
	}
}
