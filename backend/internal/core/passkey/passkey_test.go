package passkey_test

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/passkey"
	"github.com/fprambs/transferly/internal/core/passkey/passkeytest"
	"github.com/fprambs/transferly/internal/testutil"
)

const (
	rpID   = "transferly.example"
	origin = "https://transferly.example"
)

// errBoom is the failure injected by the fake store.
var errBoom = errors.New("boom")

// fakeStore keeps passkeys in memory.
type fakeStore struct {
	byUser map[uuid.UUID][]passkey.Passkey
	fail   map[string]error
}

// failure returns the error injected for an operation.
func (f *fakeStore) failure(op string) error { return f.fail[op] }

// Passkeys lists the passkeys of a user.
func (f *fakeStore) Passkeys(_ context.Context, id uuid.UUID) ([]passkey.Passkey, error) {
	if err := f.failure("list"); err != nil {
		return nil, err
	}
	return append([]passkey.Passkey(nil), f.byUser[id]...), nil
}

// AddPasskey stores a passkey.
func (f *fakeStore) AddPasskey(_ context.Context, id uuid.UUID, p passkey.Passkey) error {
	if err := f.failure("add"); err != nil {
		return err
	}
	f.byUser[id] = append(f.byUser[id], p)
	return nil
}

// DeletePasskey removes a passkey.
func (f *fakeStore) DeletePasskey(_ context.Context, userID, id uuid.UUID) error {
	if err := f.failure("delete"); err != nil {
		return err
	}
	for i, p := range f.byUser[userID] {
		if p.ID == id {
			f.byUser[userID] = append(f.byUser[userID][:i], f.byUser[userID][i+1:]...)
			return nil
		}
	}
	return passkey.ErrNotFound
}

// TouchPasskey stores the credential and the time of use.
func (f *fakeStore) TouchPasskey(_ context.Context, userID, id uuid.UUID, credential webauthn.Credential, at time.Time) error {
	if err := f.failure("touch"); err != nil {
		return err
	}
	for i, p := range f.byUser[userID] {
		if p.ID == id {
			f.byUser[userID][i].Credential, f.byUser[userID][i].LastUsedAt = credential, at
			return nil
		}
	}
	return passkey.ErrNotFound
}

// env is a service over the fake store with a controllable clock.
type env struct {
	svc     *passkey.Service
	store   *fakeStore
	clock   *testutil.FakeClock
	account passkey.Account
}

// newEnv builds the environment.
func newEnv(t *testing.T) *env {
	t.Helper()
	store := &fakeStore{byUser: map[uuid.UUID][]passkey.Passkey{}, fail: map[string]error{}}
	clock := testutil.NewFakeClock(time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
	svc, err := passkey.NewService(store, passkey.Config{RPID: rpID, DisplayName: "Transferly", Origins: []string{origin}}, clock, rand.Reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &env{svc: svc, store: store, clock: clock, account: passkey.Account{ID: uuid.New(), Email: "ana@example.com"}}
}

// register adds a passkey named name with a new authenticator and returns it.
func (e *env) register(t *testing.T, name string) (*passkeytest.Authenticator, passkey.Passkey) {
	t.Helper()
	ctx := context.Background()
	device := passkeytest.New(rpID, origin)
	options, token, err := e.svc.BeginRegistration(ctx, e.account)
	if err != nil {
		t.Fatalf("BeginRegistration() = %v", err)
	}
	created, err := e.svc.FinishRegistration(ctx, e.account, token, name, device.Register(options))
	if err != nil {
		t.Fatalf("FinishRegistration() = %v", err)
	}
	return device, created
}

// TestService_Registration verifies adding passkeys.
//
// Scenarios:
//   - a registration answered by an authenticator stores a passkey with its name, time and credential, and lists it
//   - the options name the relying party and the account, and exclude the passkeys the account already has
//   - a wrong origin, a garbage answer, a challenge used twice, a challenge of another account, a challenge of a sign-in and an expired
//     challenge are refused
//   - bad names are refused, and an account holds at most ten passkeys
//   - failures of the store, of the random source and of the identifier are returned
func TestService_Registration(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	_, first := e.register(t, "  Laptop ")
	if first.Name != "Laptop" || first.CreatedAt.IsZero() || len(first.Credential.ID) == 0 {
		t.Fatalf("passkey = %+v", first)
	}
	if list, err := e.svc.List(ctx, e.account.ID); err != nil || len(list) != 1 || list[0].ID != first.ID {
		t.Fatalf("List() = %+v, %v", list, err)
	}

	options, token, err := e.svc.BeginRegistration(ctx, e.account)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"id":"transferly.example"`, `"name":"ana@example.com"`, `"excludeCredentials"`} {
		if !strings.Contains(string(options), part) {
			t.Errorf("options %s do not hold %s", options, part)
		}
	}
	wrongPlace := passkeytest.New(rpID, "https://evil.example")
	if _, err := e.svc.FinishRegistration(ctx, e.account, token, "x", wrongPlace.Register(options)); !errors.Is(err, passkey.ErrInvalid) {
		t.Errorf("a wrong origin = %v; want ErrInvalid", err)
	}
	if _, err := e.svc.FinishRegistration(ctx, e.account, token, "x", passkeytest.New(rpID, origin).Register(options)); !errors.Is(err, passkey.ErrNoCeremony) {
		t.Errorf("a challenge used twice = %v; want ErrNoCeremony", err)
	}

	_, token, _ = e.svc.BeginRegistration(ctx, e.account)
	if _, err := e.svc.FinishRegistration(ctx, e.account, token, "x", []byte("not json")); !errors.Is(err, passkey.ErrInvalid) {
		t.Errorf("a garbage answer = %v; want ErrInvalid", err)
	}
	options, token, _ = e.svc.BeginRegistration(ctx, e.account)
	other := passkey.Account{ID: uuid.New(), Email: "bob@example.com"}
	if _, err := e.svc.FinishRegistration(ctx, other, token, "x", passkeytest.New(rpID, origin).Register(options)); !errors.Is(err, passkey.ErrNoCeremony) {
		t.Errorf("a challenge of another account = %v; want ErrNoCeremony", err)
	}
	options, token, _ = e.svc.BeginRegistration(ctx, e.account)
	e.clock.Advance(passkey.CeremonyTTL)
	if _, err := e.svc.FinishRegistration(ctx, e.account, token, "x", passkeytest.New(rpID, origin).Register(options)); !errors.Is(err, passkey.ErrNoCeremony) {
		t.Errorf("an expired challenge = %v; want ErrNoCeremony", err)
	}

	loginOptions, loginToken, _ := e.svc.BeginLogin(ctx, e.account)
	if _, err := e.svc.FinishRegistration(ctx, e.account, loginToken, "x", passkeytest.New(rpID, origin).Register(loginOptions)); !errors.Is(err, passkey.ErrNoCeremony) {
		t.Errorf("the challenge of a sign-in = %v; want ErrNoCeremony", err)
	}
	options, token, _ = e.svc.BeginRegistration(ctx, e.account)
	device := passkeytest.New(rpID, origin)
	for _, name := range []string{"", "  ", strings.Repeat("x", 61), "bad\x00name"} {
		if _, err := e.svc.FinishRegistration(ctx, e.account, token, name, device.Register(options)); !errors.Is(err, passkey.ErrInvalidName) {
			t.Errorf("name %q = %v; want ErrInvalidName", name, err)
		}
	}
	if _, err := e.svc.FinishRegistration(ctx, e.account, token, strings.Repeat("é", 60), device.Register(options)); err != nil {
		t.Errorf("a name of sixty characters = %v", err)
	}

	full := newEnv(t)
	for i := range passkey.MaxPasskeys {
		full.register(t, "key")
		_ = i
	}
	if _, _, err := full.svc.BeginRegistration(ctx, full.account); !errors.Is(err, passkey.ErrTooMany) {
		t.Errorf("an eleventh passkey = %v; want ErrTooMany", err)
	}
}

// TestService_RegistrationFailures verifies the failures inside a registration.
//
// Scenarios:
//   - a failing store, a failing random source and a failing identifier generator are returned at the step they happen
//   - a full table of pending challenges refuses new ones
func TestService_RegistrationFailures(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.store.fail["list"] = errBoom
	if _, _, err := e.svc.BeginRegistration(ctx, e.account); !errors.Is(err, errBoom) {
		t.Errorf("list failure on begin = %v", err)
	}
	e.store.fail = map[string]error{}
	options, token, _ := e.svc.BeginRegistration(ctx, e.account)
	body := passkeytest.New(rpID, origin).Register(options)
	e.store.fail["list"] = errBoom
	if _, err := e.svc.FinishRegistration(ctx, e.account, token, "x", body); !errors.Is(err, errBoom) {
		t.Errorf("list failure on finish = %v", err)
	}
	e.store.fail = map[string]error{"add": errBoom}
	options, token, _ = e.svc.BeginRegistration(ctx, e.account)
	if _, err := e.svc.FinishRegistration(ctx, e.account, token, "x", passkeytest.New(rpID, origin).Register(options)); !errors.Is(err, errBoom) {
		t.Errorf("add failure = %v", err)
	}

	noRandom, _ := passkey.NewService(e.store, passkey.Config{RPID: rpID, DisplayName: "S", Origins: []string{origin}}, e.clock, zeros{}, nil)
	if _, _, err := noRandom.BeginRegistration(ctx, e.account); err == nil {
		t.Error("a failing random source began a registration")
	}
	noID, _ := passkey.NewService(e.store, passkey.Config{RPID: rpID, DisplayName: "S", Origins: []string{origin}}, e.clock, rand.Reader, func() (uuid.UUID, error) { return uuid.Nil, errBoom })
	e.store.fail = map[string]error{}
	options, token, _ = noID.BeginRegistration(ctx, e.account)
	if _, err := noID.FinishRegistration(ctx, e.account, token, "x", passkeytest.New(rpID, origin).Register(options)); !errors.Is(err, errBoom) {
		t.Errorf("id failure = %v", err)
	}
	if _, err := passkey.NewService(e.store, passkey.Config{}, e.clock, rand.Reader, nil); err == nil {
		t.Error("a service without a relying party was created")
	}

	crowded := newEnv(t)
	for range 10_000 {
		if _, _, err := crowded.svc.BeginRegistration(ctx, crowded.account); err != nil {
			t.Fatalf("challenge: %v", err)
		}
	}
	if _, _, err := crowded.svc.BeginRegistration(ctx, crowded.account); !errors.Is(err, passkey.ErrNoCeremony) {
		t.Errorf("too many pending challenges = %v; want ErrNoCeremony", err)
	}
	crowded.clock.Advance(passkey.CeremonyTTL)
	if _, _, err := crowded.svc.BeginRegistration(ctx, crowded.account); err != nil {
		t.Errorf("after the challenges expired = %v", err)
	}
}

// zeros is a random source that always fails.
type zeros struct{}

// Read fails.
func (zeros) Read([]byte) (int, error) { return 0, errBoom }

// TestService_Login verifies using a passkey as the second factor.
//
// Scenarios:
//   - an account without passkeys cannot begin, and one with a passkey signs in with its authenticator, which records the use and the counter
//   - an answer of another authenticator, from another origin, garbled, with a used, expired or foreign challenge is refused
//   - failures of the store are returned at the step they happen
func TestService_Login(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if _, _, err := e.svc.BeginLogin(ctx, e.account); !errors.Is(err, passkey.ErrNoPasskeys) {
		t.Fatalf("BeginLogin() without passkeys = %v; want ErrNoPasskeys", err)
	}
	device, created := e.register(t, "Laptop")

	options, token, err := e.svc.BeginLogin(ctx, e.account)
	if err != nil {
		t.Fatalf("BeginLogin() = %v", err)
	}
	if err := e.svc.FinishLogin(ctx, e.account, token, device.Assert(options)); err != nil {
		t.Fatalf("FinishLogin() = %v", err)
	}
	stored := e.store.byUser[e.account.ID][0]
	if stored.ID != created.ID || stored.LastUsedAt.IsZero() || stored.Credential.Authenticator.SignCount != 1 {
		t.Errorf("stored = %+v; want the use and the counter recorded", stored)
	}

	tryWith := func(name string, body func(options []byte) []byte, want error) {
		t.Helper()
		options, token, _ := e.svc.BeginLogin(ctx, e.account)
		if err := e.svc.FinishLogin(ctx, e.account, token, body(options)); !errors.Is(err, want) {
			t.Errorf("%s = %v; want %v", name, err, want)
		}
	}
	tryWith("another authenticator", passkeytest.New(rpID, origin).Assert, passkey.ErrInvalid)
	wrongPlace := passkeytest.New(rpID, "https://evil.example")
	tryWith("another origin", wrongPlace.Assert, passkey.ErrInvalid)
	tryWith("garbage", func([]byte) []byte { return []byte("nope") }, passkey.ErrInvalid)

	options, token, _ = e.svc.BeginLogin(ctx, e.account)
	body := device.Assert(options)
	if err := e.svc.FinishLogin(ctx, e.account, token, body); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.FinishLogin(ctx, e.account, token, body); !errors.Is(err, passkey.ErrNoCeremony) {
		t.Errorf("a challenge used twice = %v; want ErrNoCeremony", err)
	}
	options, token, _ = e.svc.BeginLogin(ctx, e.account)
	e.clock.Advance(passkey.CeremonyTTL)
	if err := e.svc.FinishLogin(ctx, e.account, token, device.Assert(options)); !errors.Is(err, passkey.ErrNoCeremony) {
		t.Errorf("an expired challenge = %v; want ErrNoCeremony", err)
	}
	options, token, _ = e.svc.BeginLogin(ctx, e.account)
	if err := e.svc.FinishLogin(ctx, passkey.Account{ID: uuid.New()}, token, device.Assert(options)); !errors.Is(err, passkey.ErrNoCeremony) {
		t.Errorf("a challenge of another account = %v; want ErrNoCeremony", err)
	}

	e.store.fail["list"] = errBoom
	if _, _, err := e.svc.BeginLogin(ctx, e.account); !errors.Is(err, errBoom) {
		t.Errorf("list failure on begin = %v", err)
	}
	e.store.fail = map[string]error{}
	options, token, _ = e.svc.BeginLogin(ctx, e.account)
	body = device.Assert(options)
	e.store.fail["list"] = errBoom
	if err := e.svc.FinishLogin(ctx, e.account, token, body); !errors.Is(err, errBoom) {
		t.Errorf("list failure on finish = %v", err)
	}
	e.store.fail = map[string]error{"touch": errBoom}
	options, token, _ = e.svc.BeginLogin(ctx, e.account)
	if err := e.svc.FinishLogin(ctx, e.account, token, device.Assert(options)); !errors.Is(err, errBoom) {
		t.Errorf("touch failure = %v", err)
	}
	e.store.fail = map[string]error{}
	noRandom, _ := passkey.NewService(e.store, passkey.Config{RPID: rpID, DisplayName: "S", Origins: []string{origin}}, e.clock, zeros{}, nil)
	if _, _, err := noRandom.BeginLogin(ctx, e.account); err == nil {
		t.Error("a failing random source began a sign-in")
	}
}

// TestService_Delete verifies removing passkeys.
//
// Scenarios:
//   - a passkey is removed, one that does not exist or belongs to another account is ErrNotFound, and store failures are returned
func TestService_Delete(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	_, created := e.register(t, "Laptop")
	if err := e.svc.Delete(ctx, uuid.New(), created.ID); !errors.Is(err, passkey.ErrNotFound) {
		t.Errorf("another account = %v; want ErrNotFound", err)
	}
	if err := e.svc.Delete(ctx, e.account.ID, created.ID); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if list, _ := e.svc.List(ctx, e.account.ID); len(list) != 0 {
		t.Errorf("List() = %+v; want none", list)
	}
	e.store.fail["delete"] = errBoom
	if err := e.svc.Delete(ctx, e.account.ID, created.ID); !errors.Is(err, errBoom) {
		t.Errorf("delete failure = %v", err)
	}
}
