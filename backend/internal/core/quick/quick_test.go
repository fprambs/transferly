package quick_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/quick"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/testutil"
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

// saver records what it was asked to save.
type saver struct {
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
	return site.Site{ID: uuid.New(), WorkspaceID: ws, Name: in.Name, Host: in.Host, CollectionID: in.CollectionID}, nil
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
	f := &fixture{saver: &saver{}, clock: testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), ws: uuid.New(), editor: uuid.New(), viewer: uuid.New()}
	guard := netguard.NewPolicy([]netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}, nil)
	f.svc = quick.NewService(roles{f.editor: workspace.RoleEditor, f.viewer: workspace.RoleViewer}, guard, f.saver, f.clock, nil)
	return f
}

// input returns valid settings for a server.
func input(host string) site.Input {
	return site.Input{Protocol: site.SFTP, Host: host, Username: "deploy"}
}

// secrets returns fresh credentials, so tests can watch what happens to the memory behind them.
func secrets() quick.Secrets {
	return quick.Secrets{Password: []byte("hunter2-hunter2"), PrivateKey: []byte("KEY"), Passphrase: []byte("phrase")}
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
	if err != nil || c.Site.Name != "203.0.113.5" || c.Site.Port != 22 || c.Site.AuthMethod != site.AuthPassword || c.Site.ListTimezone != "UTC" ||
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
	bad.Protocol = "gopher"
	if _, err := f.svc.Create(ctx, f.editor, f.ws, bad, secrets()); !errors.Is(err, site.ErrInvalidProtocol) {
		t.Errorf("bad protocol = %v", err)
	}
	if _, err := f.svc.Create(ctx, f.editor, f.ws, input("10.0.0.1"), secrets()); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("private host = %v; want ErrBlocked", err)
	}
	keyOverFTP := input("203.0.113.5")
	keyOverFTP.Protocol, keyOverFTP.AuthMethod = site.FTP, site.AuthKey
	if _, err := f.svc.Create(ctx, f.editor, f.ws, keyOverFTP, secrets()); !errors.Is(err, site.ErrInvalidAuthMethod) {
		t.Errorf("key over FTP = %v", err)
	}
	asking := input("203.0.113.5")
	asking.AuthMethod = site.AuthAsk
	if _, err := f.svc.Create(ctx, f.editor, f.ws, asking, secrets()); !errors.Is(err, site.ErrInvalidAuthMethod) {
		t.Errorf("ask for password on a quick connection = %v; want ErrInvalidAuthMethod", err)
	}
	asking.AuthMethod = site.AuthAnonymous
	if _, err := f.svc.Create(ctx, f.editor, f.ws, asking, secrets()); !errors.Is(err, site.ErrInvalidAuthMethod) {
		t.Errorf("anonymous quick connection = %v; want ErrInvalidAuthMethod", err)
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

	broken := quick.NewService(roles{f.editor: workspace.RoleEditor}, netguard.NewPolicy(nil, nil), f.saver, f.clock, func() (uuid.UUID, error) { return uuid.Nil, errBoom })
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

	proxied := input("203.0.113.10")
	proxied.Proxy = site.Proxy{Kind: site.ProxySOCKS5, Host: "203.0.113.11", Port: 1080}
	if c, err := f.svc.Create(ctx, f.editor, f.ws, proxied, secrets()); err != nil || c.Site.Proxy != proxied.Proxy {
		t.Errorf("Create() with a proxy = %+v, %v", c, err)
	}
	blockedProxy := input("203.0.113.10")
	blockedProxy.Proxy = site.Proxy{Kind: site.ProxySOCKS5, Host: "10.0.0.2", Port: 1080}
	if _, err := f.svc.Create(ctx, f.editor, f.ws, blockedProxy, secrets()); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("Create() with a proxy on a private host = %v; want ErrBlocked", err)
	}
	jumped := input("203.0.113.10")
	jumped.Jump = site.Jump{Host: "203.0.113.12", Username: "bastion"}
	if c, err := f.svc.Create(ctx, f.editor, f.ws, jumped, secrets()); err != nil || c.Site.Jump.Host != "203.0.113.12" {
		t.Errorf("Create() with a jump host = %+v, %v", c, err)
	}
	blockedJump := input("203.0.113.10")
	blockedJump.Jump = site.Jump{Host: "10.0.0.3", Username: "bastion"}
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
	other := quick.NewService(roles{otherUser: workspace.RoleEditor}, netguard.NewPolicy(nil, nil), f.saver, f.clock, nil)
	theirs, err := other.Create(ctx, otherUser, otherWS, input("8.8.8.8"), secrets())
	if err != nil {
		t.Fatal(err)
	}

	got, ok := f.svc.Lookup(f.editor, f.ws, first.Site.ID)
	if !ok || string(got.Secrets.Password) != "hunter2-hunter2" || got.Site.Host != "203.0.113.5" {
		t.Fatalf("Lookup() = %+v, %v", got, ok)
	}
	described, ok := f.svc.Describe(f.editor, f.ws, first.Site.ID)
	if !ok || described.Host != "203.0.113.5" {
		t.Errorf("Describe() = %+v, %v", described, ok)
	}
	if _, ok := f.svc.Describe(uuid.New(), f.ws, first.Site.ID); ok {
		t.Error("another user described the connection")
	}
	got.Secrets.Password[0] = 'X'
	if again, _ := f.svc.Lookup(f.editor, f.ws, first.Site.ID); string(again.Secrets.Password) != "hunter2-hunter2" {
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
	if len(list[0].Secrets.Password) != 0 || len(list[0].Secrets.PrivateKey) != 0 {
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
	if !allZero(kept.Password) || !allZero(kept.PrivateKey) || !allZero(kept.Passphrase) {
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
	if _, ok := f.svc.Lookup(f.editor, f.ws, old.Site.ID); ok || !allZero(stale.Password) {
		t.Error("the unused connection survived or was not wiped")
	}
	if _, ok := f.svc.Lookup(f.editor, f.ws, busy.Site.ID); !ok || allZero(used.Password) {
		t.Error("the used connection was ended")
	}
}

// TestService_Save verifies turning a quick connection into a site.
//
// Scenarios:
//   - saving passes the settings, the name, the collection and only the secrets that exist, then ends the quick connection
//   - a failure such as a locked vault keeps the quick connection for another try
//   - unknown and foreign connections are ErrNotFound
func TestService_Save(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	in := input("203.0.113.5")
	in.Port, in.AuthMethod = 2222, site.AuthKey
	c, err := f.svc.Create(ctx, f.editor, f.ws, in, quick.Secrets{PrivateKey: []byte("KEY")})
	if err != nil {
		t.Fatal(err)
	}
	collection := uuid.New()

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
	if f.saver.input.Host != "203.0.113.5" || f.saver.input.Port != 2222 || f.saver.input.AuthMethod != site.AuthKey || f.saver.input.Username != "deploy" || f.saver.input.Name != "Production" {
		t.Errorf("saved input = %+v", f.saver.input)
	}
	ch := f.saver.changes
	if ch.PrivateKey == nil || *ch.PrivateKey != "KEY" || ch.Password != nil || ch.Passphrase != nil {
		t.Errorf("secret changes = %+v; want only the private key", ch)
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
