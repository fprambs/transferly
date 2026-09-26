package wskey_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"testing/iotest"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/core/wskey"
)

// errBoom is the failure injected by the fakes.
var errBoom = errors.New("boom")

// grantKey identifies a copy of a workspace key.
type grantKey struct {
	ws, user uuid.UUID
	version  int
}

// fakeStore keeps key pairs and grants in memory.
type fakeStore struct {
	users   map[uuid.UUID]wskey.UserKeys
	version map[uuid.UUID]int
	grants  map[grantKey][]byte
	members map[uuid.UUID][]uuid.UUID
	teams   []workspace.Workspace
	fail    map[string]error
	lose    bool
}

// failure returns the error injected for an operation.
func (f *fakeStore) failure(op string) error { return f.fail[op] }

// UserKeys returns the key pair of a user.
func (f *fakeStore) UserKeys(_ context.Context, userID uuid.UUID) (wskey.UserKeys, error) {
	if err := f.failure("userKeys"); err != nil {
		return wskey.UserKeys{}, err
	}
	keys, ok := f.users[userID]
	if !ok {
		return wskey.UserKeys{}, wskey.ErrNoKeys
	}
	return keys, nil
}

// SetUserKeys stores a key pair unless one exists; with lose set, another writer wins the race.
func (f *fakeStore) SetUserKeys(_ context.Context, userID uuid.UUID, keys wskey.UserKeys) (bool, error) {
	if err := f.failure("setUserKeys"); err != nil {
		return false, err
	}
	if f.lose {
		f.users[userID] = wskey.UserKeys{Public: []byte("winner"), SealedPrivate: []byte("x")}
		return false, nil
	}
	f.users[userID] = keys
	return true, nil
}

// KeyState returns the version of a workspace.
func (f *fakeStore) KeyState(_ context.Context, workspaceID uuid.UUID) (int, bool, error) {
	if err := f.failure("keyState"); err != nil {
		return 0, false, err
	}
	return f.version[workspaceID], false, nil
}

// Grant returns a copy of a workspace key.
func (f *fakeStore) Grant(_ context.Context, workspaceID, userID uuid.UUID, version int) ([]byte, error) {
	if err := f.failure("grant"); err != nil {
		return nil, err
	}
	sealed, ok := f.grants[grantKey{workspaceID, userID, version}]
	if !ok {
		return nil, wskey.ErrPending
	}
	return sealed, nil
}

// AddGrant stores a copy of a workspace key.
func (f *fakeStore) AddGrant(_ context.Context, workspaceID, userID uuid.UUID, version int, sealed []byte) error {
	if err := f.failure("addGrant"); err != nil {
		return err
	}
	f.grants[grantKey{workspaceID, userID, version}] = sealed
	return nil
}

// Ungranted lists the members with a public key and no copy.
func (f *fakeStore) Ungranted(_ context.Context, workspaceID uuid.UUID, version int) ([]wskey.Recipient, error) {
	if err := f.failure("ungranted"); err != nil {
		return nil, err
	}
	var out []wskey.Recipient
	for _, user := range f.members[workspaceID] {
		keys, ok := f.users[user]
		if _, granted := f.grants[grantKey{workspaceID, user, version}]; ok && !granted {
			out = append(out, wskey.Recipient{UserID: user, Public: keys.Public})
		}
	}
	return out, nil
}

// Granted lists the members with a copy.
func (f *fakeStore) Granted(_ context.Context, workspaceID uuid.UUID, version int) ([]uuid.UUID, error) {
	if err := f.failure("granted"); err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, user := range f.members[workspaceID] {
		if _, ok := f.grants[grantKey{workspaceID, user, version}]; ok {
			out = append(out, user)
		}
	}
	return out, nil
}

// CreateTeam stores a team with its owner and the first grant.
func (f *fakeStore) CreateTeam(_ context.Context, w workspace.Workspace, sealed []byte) error {
	if err := f.failure("createTeam"); err != nil {
		return err
	}
	f.teams = append(f.teams, w)
	f.version[w.ID] = 1
	f.members[w.ID] = append(f.members[w.ID], w.OwnerID)
	f.grants[grantKey{w.ID, w.OwnerID, 1}] = sealed
	return nil
}

// fakeVault holds the DEK of each session.
type fakeVault struct {
	deks map[string][]byte
	fail error
}

// Key returns a copy of the DEK of a session, or [vault.ErrLocked].
func (v fakeVault) Key(session string) ([]byte, error) {
	if v.fail != nil {
		return nil, v.fail
	}
	dek, ok := v.deks[session]
	if !ok {
		return nil, vault.ErrLocked
	}
	return bytes.Clone(dek), nil
}

// fakeWorkspaces knows the workspaces and who belongs to them.
type fakeWorkspaces struct {
	store    *fakeStore
	kinds    map[uuid.UUID]workspace.Kind
	fail     error
	listFail error
}

// Get returns a workspace for a member.
func (f *fakeWorkspaces) Get(_ context.Context, actor, workspaceID uuid.UUID) (workspace.Summary, error) {
	if f.fail != nil {
		return workspace.Summary{}, f.fail
	}
	kind, ok := f.kinds[workspaceID]
	if !ok {
		return workspace.Summary{}, workspace.ErrNotFound
	}
	for _, user := range f.store.members[workspaceID] {
		if user == actor {
			return workspace.Summary{Workspace: workspace.Workspace{ID: workspaceID, Kind: kind}, Role: workspace.RoleAdmin}, nil
		}
	}
	return workspace.Summary{}, workspace.ErrNotFound
}

// List returns the workspaces a user belongs to.
func (f *fakeWorkspaces) List(_ context.Context, userID uuid.UUID) ([]workspace.Summary, error) {
	if f.listFail != nil {
		return nil, f.listFail
	}
	var out []workspace.Summary
	for id, kind := range f.kinds {
		for _, user := range f.store.members[id] {
			if user == userID {
				out = append(out, workspace.Summary{Workspace: workspace.Workspace{ID: id, Kind: kind}, Role: workspace.RoleAdmin})
			}
		}
	}
	return out, nil
}

// NewTeam prepares a team workspace.
func (f *fakeWorkspaces) NewTeam(actor uuid.UUID, name string) (workspace.Workspace, error) {
	if name == "" {
		return workspace.Workspace{}, workspace.ErrInvalidName
	}
	w := workspace.Workspace{ID: uuid.New(), Name: name, Kind: workspace.KindTeam, OwnerID: actor}
	f.kinds[w.ID] = workspace.KindTeam
	return w, nil
}

// env is a service over fakes with three users whose vaults are unlocked in their own sessions.
type env struct {
	svc               *wskey.Service
	store             *fakeStore
	vault             fakeVault
	workspaces        *fakeWorkspaces
	alice, bob, carol uuid.UUID
	personal          uuid.UUID
}

// newEnv builds the environment.
func newEnv(t *testing.T) *env {
	t.Helper()
	store := &fakeStore{users: map[uuid.UUID]wskey.UserKeys{}, version: map[uuid.UUID]int{}, grants: map[grantKey][]byte{}, members: map[uuid.UUID][]uuid.UUID{}, fail: map[string]error{}}
	e := &env{store: store, alice: uuid.New(), bob: uuid.New(), carol: uuid.New(), personal: uuid.New()}
	e.vault = fakeVault{deks: map[string][]byte{"alice": bytes.Repeat([]byte{1}, 32), "bob": bytes.Repeat([]byte{2}, 32), "carol": bytes.Repeat([]byte{3}, 32)}}
	e.workspaces = &fakeWorkspaces{store: store, kinds: map[uuid.UUID]workspace.Kind{e.personal: workspace.KindPersonal}}
	store.members[e.personal] = []uuid.UUID{e.alice}
	e.svc = wskey.NewService(store, e.vault, e.workspaces, rand.Reader)
	return e
}

// team creates a team owned by alice and gives keys to bob, and returns its identifier.
func (e *env) team(t *testing.T) uuid.UUID {
	t.Helper()
	w, err := e.svc.CreateTeam(context.Background(), e.alice, "alice", "Acme")
	if err != nil {
		t.Fatalf("CreateTeam() = %v", err)
	}
	return w.ID
}

// TestService_Ensure verifies creating key pairs.
//
// Scenarios:
//   - the first call creates a key pair sealed with the DEK of the session, and the second call keeps it
//   - a locked vault, a failing store, a failing random source and a lost race are handled
func TestService_Ensure(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if err := e.svc.Ensure(ctx, e.alice, "alice"); err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	first := e.store.users[e.alice]
	private, err := vault.OpenPrivateKey(e.vault.deks["alice"], e.alice.String(), first.SealedPrivate)
	if err != nil || len(first.Public) != 32 || len(private) != 32 {
		t.Fatalf("the stored pair does not open: %v", err)
	}
	if err := e.svc.Ensure(ctx, e.alice, "alice"); err != nil || !bytes.Equal(e.store.users[e.alice].Public, first.Public) {
		t.Errorf("second Ensure() = %v; want the pair kept", err)
	}

	if err := e.svc.Ensure(ctx, e.bob, "nobody"); !errors.Is(err, vault.ErrLocked) {
		t.Errorf("locked = %v; want ErrLocked", err)
	}
	e.store.fail["userKeys"] = errBoom
	if err := e.svc.Ensure(ctx, e.bob, "bob"); !errors.Is(err, errBoom) {
		t.Errorf("lookup failure = %v", err)
	}
	delete(e.store.fail, "userKeys")
	e.store.fail["setUserKeys"] = errBoom
	if err := e.svc.Ensure(ctx, e.bob, "bob"); !errors.Is(err, errBoom) {
		t.Errorf("write failure = %v", err)
	}
	delete(e.store.fail, "setUserKeys")

	noRandom := wskey.NewService(e.store, e.vault, e.workspaces, iotest.ErrReader(errBoom))
	if err := noRandom.Ensure(ctx, e.bob, "bob"); err == nil {
		t.Error("a failing random source created a key pair")
	}
	short := wskey.NewService(e.store, e.vault, e.workspaces, &limited{left: 32})
	if err := short.Ensure(ctx, e.bob, "bob"); err == nil {
		t.Error("a random source that runs out before the nonce created a key pair")
	}

	e.store.lose = true
	if err := e.svc.Ensure(ctx, e.carol, "carol"); err != nil || string(e.store.users[e.carol].Public) != "winner" {
		t.Errorf("lost race = %v; want the winner's pair", err)
	}
}

// limited serves a number of random bytes and then fails.
type limited struct{ left int }

// Read serves zeros until the budget is spent.
func (l *limited) Read(p []byte) (int, error) {
	if l.left == 0 {
		return 0, errBoom
	}
	n := min(len(p), l.left)
	l.left -= n
	clear(p[:n])
	return n, nil
}

// TestService_CreateTeam verifies creating a team with its key.
//
// Scenarios:
//   - the team is stored with a key the owner opens with their session, at version 1
//   - an invalid name is refused before any key is made, and a locked vault without a key pair is refused
//   - failures of the key pair, the random source, the sealing and the store are returned
func TestService_CreateTeam(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.team(t)
	key, version, err := e.svc.Current(ctx, e.alice, id, "alice")
	if err != nil || version != 1 || len(key) != 32 {
		t.Fatalf("Current() = %d bytes, version %d, %v", len(key), version, err)
	}
	if _, err := e.svc.CreateTeam(ctx, e.alice, "alice", ""); !errors.Is(err, workspace.ErrInvalidName) {
		t.Errorf("empty name = %v", err)
	}
	if _, err := e.svc.CreateTeam(ctx, e.bob, "nobody", "Locked"); !errors.Is(err, vault.ErrLocked) {
		t.Errorf("locked = %v; want ErrLocked", err)
	}
	e.store.fail["createTeam"] = errBoom
	if _, err := e.svc.CreateTeam(ctx, e.alice, "alice", "Fails"); !errors.Is(err, errBoom) {
		t.Errorf("store failure = %v", err)
	}
	delete(e.store.fail, "createTeam")

	own := newEnv(t)
	own.store.users[own.alice] = wskey.UserKeys{Public: bytes.Repeat([]byte{9}, 32), SealedPrivate: []byte("x")}
	for name, reader := range map[string]*limited{"no random for the key": {left: 0}, "no random for the ephemeral key": {left: 32}, "no random for the nonce": {left: 64}} {
		failing := wskey.NewService(own.store, own.vault, own.workspaces, reader)
		if _, err := failing.CreateTeam(ctx, own.alice, "alice", "Fails"); err == nil {
			t.Errorf("%s: the team was created", name)
		}
	}
	badKey := newEnv(t)
	badKey.store.users[badKey.alice] = wskey.UserKeys{Public: []byte("short"), SealedPrivate: []byte("x")}
	if _, err := badKey.svc.CreateTeam(ctx, badKey.alice, "alice", "Fails"); err == nil {
		t.Error("a malformed public key sealed a team key")
	}
}

// TestService_Keys verifies which key each workspace and member gets.
//
// Scenarios:
//   - a personal workspace uses the DEK of the session at version 1, and a locked session gets ErrLocked
//   - a team member with a copy gets the workspace key at the current version and at a given version
//   - a member without a copy gets ErrPending, and a stranger gets ErrNotFound
//   - a copy that was tampered with, a missing private key and failures of the workspace lookup and the key state are returned
func TestService_Keys(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	key, version, err := e.svc.Current(ctx, e.alice, e.personal, "alice")
	if err != nil || version != 1 || !bytes.Equal(key, e.vault.deks["alice"]) {
		t.Fatalf("personal Current() = %x, %d, %v", key, version, err)
	}
	if got, err := e.svc.Version(ctx, e.alice, e.personal, "alice", 1); err != nil || !bytes.Equal(got, e.vault.deks["alice"]) {
		t.Fatalf("personal Version() = %x, %v", got, err)
	}
	if _, _, err := e.svc.Current(ctx, e.alice, e.personal, "nobody"); !errors.Is(err, vault.ErrLocked) {
		t.Errorf("locked personal = %v", err)
	}

	id := e.team(t)
	current, version, err := e.svc.Current(ctx, e.alice, id, "alice")
	if err != nil || version != 1 {
		t.Fatal(err)
	}
	if got, err := e.svc.Version(ctx, e.alice, id, "alice", 1); err != nil || !bytes.Equal(got, current) {
		t.Fatalf("Version() = %x, %v; want the current key", got, err)
	}
	e.store.members[id] = append(e.store.members[id], e.bob)
	if _, _, err := e.svc.Current(ctx, e.bob, id, "bob"); !errors.Is(err, wskey.ErrPending) {
		t.Errorf("member without a copy = %v; want ErrPending", err)
	}
	if _, err := e.svc.Version(ctx, e.bob, id, "bob", 1); !errors.Is(err, wskey.ErrPending) {
		t.Errorf("member without a copy, Version = %v; want ErrPending", err)
	}
	if _, _, err := e.svc.Current(ctx, e.carol, id, "carol"); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger = %v; want ErrNotFound", err)
	}
	if _, err := e.svc.Version(ctx, e.carol, id, "carol", 1); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger, Version = %v; want ErrNotFound", err)
	}
	if _, _, err := e.svc.Current(ctx, e.alice, id, "nobody"); !errors.Is(err, vault.ErrLocked) {
		t.Errorf("locked team = %v; want ErrLocked", err)
	}

	tampered := e.store.grants[grantKey{id, e.alice, 1}]
	tampered[len(tampered)-1] ^= 1
	if _, _, err := e.svc.Current(ctx, e.alice, id, "alice"); !errors.Is(err, vault.ErrCorrupt) {
		t.Errorf("tampered copy = %v; want ErrCorrupt", err)
	}
	tampered[len(tampered)-1] ^= 1
	delete(e.store.users, e.alice)
	if _, _, err := e.svc.Current(ctx, e.alice, id, "alice"); !errors.Is(err, wskey.ErrNoKeys) {
		t.Errorf("missing private key = %v; want ErrNoKeys", err)
	}

	e.store.fail["keyState"] = errBoom
	if _, _, err := e.svc.Current(ctx, e.alice, id, "alice"); !errors.Is(err, errBoom) {
		t.Errorf("key state failure = %v", err)
	}
	delete(e.store.fail, "keyState")
	e.workspaces.fail = errBoom
	if _, _, err := e.svc.Current(ctx, e.alice, id, "alice"); !errors.Is(err, errBoom) {
		t.Errorf("workspace failure = %v", err)
	}
	if _, err := e.svc.Version(ctx, e.alice, id, "alice", 1); !errors.Is(err, errBoom) {
		t.Errorf("workspace failure, Version = %v", err)
	}
}

// TestService_Complete verifies sealing the key for members that wait for it.
//
// Scenarios:
//   - a member who set up their vault after joining receives a copy the next time a member with the key completes the workspace
//   - members without a public key are left waiting, and completing twice changes nothing
//   - a member without the key cannot complete, a locked vault cannot complete, and personal workspaces need nothing
//   - failures of the lookups, the sealing and the store are returned
func TestService_Complete(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.team(t)
	e.store.members[id] = append(e.store.members[id], e.bob, e.carol)
	if err := e.svc.Ensure(ctx, e.bob, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Complete(ctx, e.alice, id, "alice"); err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	fromAlice, _, _ := e.svc.Current(ctx, e.alice, id, "alice")
	fromBob, _, err := e.svc.Current(ctx, e.bob, id, "bob")
	if err != nil || !bytes.Equal(fromAlice, fromBob) {
		t.Fatalf("bob's key = %x, %v; want alice's", fromBob, err)
	}
	if _, ok := e.store.grants[grantKey{id, e.carol, 1}]; ok {
		t.Error("a member without a public key received a copy")
	}
	before := len(e.store.grants)
	if err := e.svc.Complete(ctx, e.alice, id, "alice"); err != nil || len(e.store.grants) != before {
		t.Errorf("second Complete() = %v with %d grants; want no change", err, len(e.store.grants))
	}

	if err := e.svc.Ensure(ctx, e.carol, "carol"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Complete(ctx, e.alice, e.personal, "alice"); err != nil {
		t.Errorf("personal Complete() = %v", err)
	}
	if err := e.svc.Complete(ctx, e.alice, id, "nobody"); !errors.Is(err, vault.ErrLocked) {
		t.Errorf("locked = %v; want ErrLocked", err)
	}
	e.store.members[id] = append(e.store.members[id], uuid.New())
	if err := e.svc.Complete(ctx, uuid.New(), id, "alice"); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger = %v; want ErrNotFound", err)
	}
	waiting := newEnv(t)
	wid := waiting.team(t)
	waiting.store.members[wid] = append(waiting.store.members[wid], waiting.bob, waiting.carol)
	if err := waiting.svc.Ensure(ctx, waiting.carol, "carol"); err != nil {
		t.Fatal(err)
	}
	if err := waiting.svc.Complete(ctx, waiting.bob, wid, "bob"); !errors.Is(err, wskey.ErrPending) {
		t.Errorf("member without the key = %v; want ErrPending", err)
	}

	for _, op := range []string{"keyState", "ungranted", "addGrant"} {
		failing := newEnv(t)
		fid := failing.team(t)
		failing.store.members[fid] = append(failing.store.members[fid], failing.carol)
		if err := failing.svc.Ensure(ctx, failing.carol, "carol"); err != nil {
			t.Fatal(err)
		}
		failing.store.fail[op] = errBoom
		if err := failing.svc.Complete(ctx, failing.alice, fid, "alice"); !errors.Is(err, errBoom) {
			t.Errorf("%s failure = %v", op, err)
		}
	}
	failing := newEnv(t)
	fid := failing.team(t)
	failing.workspaces.fail = errBoom
	if err := failing.svc.Complete(ctx, failing.alice, fid, "alice"); !errors.Is(err, errBoom) {
		t.Errorf("workspace failure = %v", err)
	}

	badKey := newEnv(t)
	bid := badKey.team(t)
	badKey.store.members[bid] = append(badKey.store.members[bid], badKey.bob)
	badKey.store.users[badKey.bob] = wskey.UserKeys{Public: []byte("short"), SealedPrivate: []byte("x")}
	if err := badKey.svc.Complete(ctx, badKey.alice, bid, "alice"); err == nil {
		t.Error("a malformed public key received a copy")
	}
}

// TestService_Sealed verifies listing who holds the key.
//
// Scenarios:
//   - the owner of a new team is listed and members without a copy are not
//   - strangers and failures of the workspace lookup, the key state and the grant list are returned
func TestService_Sealed(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.team(t)
	e.store.members[id] = append(e.store.members[id], e.bob)
	got, err := e.svc.Sealed(ctx, e.alice, id)
	if err != nil || !got[e.alice] || got[e.bob] || len(got) != 1 {
		t.Fatalf("Sealed() = %v, %v", got, err)
	}
	if _, err := e.svc.Sealed(ctx, e.carol, id); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger = %v", err)
	}
	for _, op := range []string{"keyState", "granted"} {
		e.store.fail[op] = errBoom
		if _, err := e.svc.Sealed(ctx, e.alice, id); !errors.Is(err, errBoom) {
			t.Errorf("%s failure = %v", op, err)
		}
		delete(e.store.fail, op)
	}
}

// TestService_Unlocked verifies telling whether a session can use keys.
//
// Scenarios:
//   - a session with a DEK is unlocked, and an unknown session is not
func TestService_Unlocked(t *testing.T) {
	e := newEnv(t)
	if !e.svc.Unlocked("alice") || e.svc.Unlocked("nobody") {
		t.Error("Unlocked() does not follow the vault")
	}
}

// TestService_Refresh verifies preparing the keys after unlocking.
//
// Scenarios:
//   - the key pair is created and the teams of the user are completed, so a member who joined before their vault existed gets a copy
//     when somebody with the key unlocks
//   - teams whose key the user has not received are skipped, and personal workspaces need nothing
//   - failures of the key pair, the workspace list and the completion are returned
func TestService_Refresh(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.team(t)
	e.store.members[id] = append(e.store.members[id], e.bob)
	if err := e.svc.Ensure(ctx, e.bob, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Refresh(ctx, e.alice, "alice"); err != nil {
		t.Fatalf("Refresh() = %v", err)
	}
	if _, ok := e.store.grants[grantKey{id, e.bob, 1}]; !ok {
		t.Error("bob did not receive a copy")
	}

	waiting := newEnv(t)
	wid := waiting.team(t)
	waiting.store.members[wid] = append(waiting.store.members[wid], waiting.bob, waiting.carol)
	if err := waiting.svc.Ensure(ctx, waiting.carol, "carol"); err != nil {
		t.Fatal(err)
	}
	if err := waiting.svc.Refresh(ctx, waiting.bob, "bob"); err != nil {
		t.Errorf("a team whose key the user lacks = %v; want it skipped", err)
	}

	if err := e.svc.Refresh(ctx, e.carol, "nobody"); !errors.Is(err, vault.ErrLocked) {
		t.Errorf("locked = %v; want ErrLocked", err)
	}
	e.workspaces.listFail = errBoom
	if err := e.svc.Refresh(ctx, e.alice, "alice"); !errors.Is(err, errBoom) {
		t.Errorf("list failure = %v", err)
	}
	e.workspaces.listFail = nil
	e.store.fail["keyState"] = errBoom
	if err := e.svc.Refresh(ctx, e.alice, "alice"); !errors.Is(err, errBoom) {
		t.Errorf("completion failure = %v", err)
	}
}
