package vault_test

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/vault"
	"github.com/fprambs/transferly/internal/testutil"
)

// errStore is the failure injected by [memStore].
var errStore = errors.New("store failure")

// memStore is an in-memory vault.Store with switchable failures.
type memStore struct {
	vaults     map[uuid.UUID]vault.Enrollment
	failRead   bool
	failCreate error
	failUpdate bool
}

// CreateVault stores the enrollment unless a failure is injected or one exists.
func (m *memStore) CreateVault(_ context.Context, id uuid.UUID, e vault.Enrollment) error {
	if m.failCreate != nil {
		return m.failCreate
	}
	if _, ok := m.vaults[id]; ok {
		return vault.ErrVaultExists
	}
	m.vaults[id] = e
	return nil
}

// Vault returns the stored enrollment.
func (m *memStore) Vault(_ context.Context, id uuid.UUID) (vault.Enrollment, error) {
	if m.failRead {
		return vault.Enrollment{}, errStore
	}
	e, ok := m.vaults[id]
	if !ok {
		return vault.Enrollment{}, vault.ErrNoVault
	}
	return e, nil
}

// UpdateVault replaces the stored enrollment unless a failure is injected.
func (m *memStore) UpdateVault(_ context.Context, id uuid.UUID, e vault.Enrollment) error {
	if m.failUpdate {
		return errStore
	}
	m.vaults[id] = e
	return nil
}

// newVaultService returns a service over a fresh memory store and a fast key derivation.
func newVaultService(t *testing.T) (*vault.Service, *memStore, *vault.KeyRing) {
	t.Helper()
	v, err := vault.New(vault.NewKDF(2), vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	store := &memStore{vaults: map[uuid.UUID]vault.Enrollment{}}
	ring := vault.NewKeyRing(testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), time.Hour)
	return vault.NewService(v, store, ring), store, ring
}

const (
	master = "correct horse battery"
	other  = "another long password"
)

// TestService_Setup verifies vault creation.
//
// Scenarios:
//   - a short or oversized master password is ErrWeakPassword and nothing is stored
//   - setup stores the vault, unlocks the session and returns a recovery kit
//   - a second setup is ErrAlreadyInitialized
//   - a lost creation race is ErrAlreadyInitialized
//   - store failures on read and on create are returned
//   - status follows initialization and unlocking, and reports read failures
func TestService_Setup(t *testing.T) {
	ctx := context.Background()
	user := uuid.New()

	svc, store, _ := newVaultService(t)
	if st, err := svc.Status(ctx, user, "s1"); err != nil || st.Initialized || st.Unlocked {
		t.Fatalf("Status() before setup = %+v, %v", st, err)
	}
	for _, weak := range []string{"short", string(make([]byte, 1025))} {
		if _, err := svc.Setup(ctx, user, "s1", weak); !errors.Is(err, vault.ErrWeakPassword) {
			t.Errorf("Setup(weak) = %v; want ErrWeakPassword", err)
		}
	}
	if len(store.vaults) != 0 {
		t.Fatal("a rejected password stored a vault")
	}
	kit, err := svc.Setup(ctx, user, "s1", master)
	if err != nil || kit == "" {
		t.Fatalf("Setup() = %q, %v", kit, err)
	}
	if st, _ := svc.Status(ctx, user, "s1"); !st.Initialized || !st.Unlocked {
		t.Errorf("Status() after setup = %+v", st)
	}
	if st, _ := svc.Status(ctx, user, "other-session"); !st.Initialized || st.Unlocked {
		t.Errorf("Status() in another session = %+v; want initialized and locked", st)
	}
	if _, err := svc.Setup(ctx, user, "s1", master); !errors.Is(err, vault.ErrAlreadyInitialized) {
		t.Errorf("second Setup() = %v; want ErrAlreadyInitialized", err)
	}

	racing, racingStore, _ := newVaultService(t)
	racingStore.failCreate = vault.ErrVaultExists
	if _, err := racing.Setup(ctx, user, "s", master); !errors.Is(err, vault.ErrAlreadyInitialized) {
		t.Errorf("lost race = %v; want ErrAlreadyInitialized", err)
	}
	racingStore.failCreate = errStore
	if _, err := racing.Setup(ctx, user, "s", master); !errors.Is(err, errStore) {
		t.Errorf("create failure = %v; want the store error", err)
	}
	racingStore.failRead = true
	if _, err := racing.Setup(ctx, user, "s", master); !errors.Is(err, errStore) {
		t.Errorf("read failure = %v; want the store error", err)
	}
	if _, err := racing.Status(ctx, user, "s"); !errors.Is(err, errStore) {
		t.Errorf("Status() read failure = %v; want the store error", err)
	}
}

// TestService_UnlockAndLock verifies unlocking, locking and key access.
//
// Scenarios:
//   - an uninitialized vault is ErrNotInitialized
//   - a wrong password is ErrWrongSecret and leaves the session locked
//   - the right password unlocks and Key returns the key; locking removes it and Key is ErrLocked
//   - read failures are returned
func TestService_UnlockAndLock(t *testing.T) {
	ctx := context.Background()
	user := uuid.New()
	svc, store, _ := newVaultService(t)

	if err := svc.Unlock(ctx, user, "s", master); !errors.Is(err, vault.ErrNotInitialized) {
		t.Fatalf("Unlock() before setup = %v; want ErrNotInitialized", err)
	}
	if _, err := svc.Setup(ctx, user, "creator", master); err != nil {
		t.Fatal(err)
	}
	if err := svc.Unlock(ctx, user, "s", other); !errors.Is(err, vault.ErrWrongSecret) {
		t.Fatalf("Unlock(wrong) = %v; want ErrWrongSecret", err)
	}
	if _, err := svc.Key("s"); !errors.Is(err, vault.ErrLocked) {
		t.Fatalf("Key() after failed unlock = %v; want ErrLocked", err)
	}
	if err := svc.Unlock(ctx, user, "s", master); err != nil {
		t.Fatalf("Unlock() = %v", err)
	}
	if key, err := svc.Key("s"); err != nil || len(key) != vault.KeyLength {
		t.Fatalf("Key() = %d bytes, %v", len(key), err)
	}
	svc.Lock("s")
	svc.Lock("never-unlocked")
	if _, err := svc.Key("s"); !errors.Is(err, vault.ErrLocked) {
		t.Errorf("Key() after Lock = %v; want ErrLocked", err)
	}
	store.failRead = true
	if err := svc.Unlock(ctx, user, "s", master); !errors.Is(err, errStore) {
		t.Errorf("Unlock() read failure = %v; want the store error", err)
	}
}

// TestService_ChangePassword verifies master password changes.
//
// Scenarios:
//   - a weak new password, a wrong current password, a missing vault and read failures are refused
//   - a change makes the new password work and the old one fail, keeping the same key and the session unlocked
//   - an update failure is returned
func TestService_ChangePassword(t *testing.T) {
	ctx := context.Background()
	user := uuid.New()
	svc, store, _ := newVaultService(t)

	if err := svc.ChangePassword(ctx, user, "s", master, other); !errors.Is(err, vault.ErrNotInitialized) {
		t.Fatalf("missing vault = %v; want ErrNotInitialized", err)
	}
	if _, err := svc.Setup(ctx, user, "s", master); err != nil {
		t.Fatal(err)
	}
	before, _ := svc.Key("s")
	if err := svc.ChangePassword(ctx, user, "s", master, "short"); !errors.Is(err, vault.ErrWeakPassword) {
		t.Errorf("weak = %v; want ErrWeakPassword", err)
	}
	if err := svc.ChangePassword(ctx, user, "s", other, other); !errors.Is(err, vault.ErrWrongSecret) {
		t.Errorf("wrong current = %v; want ErrWrongSecret", err)
	}
	store.failUpdate = true
	if err := svc.ChangePassword(ctx, user, "s", master, other); !errors.Is(err, errStore) {
		t.Errorf("update failure = %v; want the store error", err)
	}
	store.failUpdate = false
	if err := svc.ChangePassword(ctx, user, "s", master, other); err != nil {
		t.Fatalf("ChangePassword() = %v", err)
	}
	if err := svc.Unlock(ctx, user, "fresh", master); !errors.Is(err, vault.ErrWrongSecret) {
		t.Errorf("old password after change = %v; want ErrWrongSecret", err)
	}
	if err := svc.Unlock(ctx, user, "fresh", other); err != nil {
		t.Fatalf("new password = %v", err)
	}
	if after, _ := svc.Key("fresh"); string(after) != string(before) {
		t.Error("changing the master password changed the key")
	}
}

// TestService_Recovery verifies recovery and kit regeneration.
//
// Scenarios:
//   - a weak new password, a missing vault, a wrong kit and read failures are refused
//   - recovery sets a new master password and unlocks the session, and the kit keeps working
//   - an update failure is returned by recovery and by regeneration
//   - regeneration needs the master password, returns a new kit, and the old kit stops working
//   - regeneration on a missing vault or with read failure is refused
func TestService_Recovery(t *testing.T) {
	ctx := context.Background()
	user := uuid.New()
	svc, store, _ := newVaultService(t)

	if err := svc.Recover(ctx, user, "s", "kit", other); !errors.Is(err, vault.ErrNotInitialized) {
		t.Fatalf("missing vault = %v; want ErrNotInitialized", err)
	}
	if _, err := svc.RegenerateRecovery(ctx, user, master); !errors.Is(err, vault.ErrNotInitialized) {
		t.Fatalf("regenerate on missing vault = %v; want ErrNotInitialized", err)
	}
	kit, err := svc.Setup(ctx, user, "s", master)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Recover(ctx, user, "r", kit, "short"); !errors.Is(err, vault.ErrWeakPassword) {
		t.Errorf("weak = %v; want ErrWeakPassword", err)
	}
	if err := svc.Recover(ctx, user, "r", "not a kit", other); !errors.Is(err, vault.ErrWrongSecret) {
		t.Errorf("wrong kit = %v; want ErrWrongSecret", err)
	}
	store.failUpdate = true
	if err := svc.Recover(ctx, user, "r", kit, other); !errors.Is(err, errStore) {
		t.Errorf("recover update failure = %v; want the store error", err)
	}
	if _, err := svc.RegenerateRecovery(ctx, user, master); !errors.Is(err, errStore) {
		t.Errorf("regenerate update failure = %v; want the store error", err)
	}
	store.failUpdate = false
	if err := svc.Recover(ctx, user, "r", kit, other); err != nil {
		t.Fatalf("Recover() = %v", err)
	}
	if _, err := svc.Key("r"); err != nil {
		t.Errorf("Key() after recovery = %v", err)
	}
	if err := svc.Unlock(ctx, user, "u", other); err != nil {
		t.Errorf("new password after recovery = %v", err)
	}
	if err := svc.Recover(ctx, user, "r2", kit, master); err != nil {
		t.Errorf("kit after recovery = %v; want it to keep working", err)
	}

	if _, err := svc.RegenerateRecovery(ctx, user, "wrong wrong wrong"); !errors.Is(err, vault.ErrWrongSecret) {
		t.Errorf("regenerate with wrong password = %v; want ErrWrongSecret", err)
	}
	fresh, err := svc.RegenerateRecovery(ctx, user, master)
	if err != nil || fresh == "" || fresh == kit {
		t.Fatalf("RegenerateRecovery() = %q, %v", fresh, err)
	}
	if err := svc.Recover(ctx, user, "r3", kit, other); !errors.Is(err, vault.ErrWrongSecret) {
		t.Errorf("old kit after regeneration = %v; want ErrWrongSecret", err)
	}
	if err := svc.Recover(ctx, user, "r3", fresh, other); err != nil {
		t.Errorf("new kit = %v", err)
	}
	store.failRead = true
	if err := svc.Recover(ctx, user, "r", kit, other); !errors.Is(err, errStore) {
		t.Errorf("recover read failure = %v; want the store error", err)
	}
}

// switchReader is a random source that can be made to fail.
type switchReader struct{ broken bool }

// Read fills p from the system source, or fails once broken.
func (r *switchReader) Read(p []byte) (int, error) {
	if r.broken {
		return 0, errStore
	}
	return rand.Read(p)
}

// TestService_RandomnessFailures verifies that a failing random source is reported by every operation that needs it.
//
// Scenarios:
//   - setup fails and stores nothing
//   - changing the password, recovering and regenerating the kit fail without touching the stored vault
func TestService_RandomnessFailures(t *testing.T) {
	ctx := context.Background()
	user := uuid.New()
	random := &switchReader{}
	v, err := vault.New(vault.NewKDF(2), vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}, random)
	if err != nil {
		t.Fatal(err)
	}
	store := &memStore{vaults: map[uuid.UUID]vault.Enrollment{}}
	svc := vault.NewService(v, store, vault.NewKeyRing(testutil.NewFakeClock(time.Now()), time.Hour))

	random.broken = true
	if _, err := svc.Setup(ctx, user, "s", master); !errors.Is(err, errStore) || len(store.vaults) != 0 {
		t.Fatalf("Setup() = %v with %d stored; want the random error and nothing stored", err, len(store.vaults))
	}
	random.broken = false
	kit, err := svc.Setup(ctx, user, "s", master)
	if err != nil {
		t.Fatal(err)
	}
	random.broken = true
	if err := svc.ChangePassword(ctx, user, "s", master, other); !errors.Is(err, errStore) {
		t.Errorf("ChangePassword() = %v; want the random error", err)
	}
	if err := svc.Recover(ctx, user, "s", kit, other); !errors.Is(err, errStore) {
		t.Errorf("Recover() = %v; want the random error", err)
	}
	if _, err := svc.RegenerateRecovery(ctx, user, master); !errors.Is(err, errStore) {
		t.Errorf("RegenerateRecovery() = %v; want the random error", err)
	}
	random.broken = false
	if err := svc.Unlock(ctx, user, "s", master); err != nil {
		t.Errorf("the original password stopped working: %v", err)
	}
}

// TestNewDefault verifies that the default vault works: its parameters are valid and a vault can be created and opened.
//
// Scenarios:
//   - DefaultParams are accepted by New
//   - a vault built by NewDefault sets up and unlocks with the default cost
func TestNewDefault(t *testing.T) {
	kdf := vault.NewKDF(1)
	if _, err := vault.New(kdf, vault.DefaultParams, rand.Reader); err != nil {
		t.Fatalf("New(DefaultParams) = %v", err)
	}
	v := vault.NewDefault(kdf, rand.Reader)
	e, dek, _, err := v.Setup(context.Background(), master)
	if err != nil {
		t.Fatal(err)
	}
	if e.Params != vault.DefaultParams {
		t.Errorf("Params = %+v; want the defaults", e.Params)
	}
	if opened, err := v.Unlock(context.Background(), master, e); err != nil || string(opened) != string(dek) {
		t.Errorf("Unlock() = %v", err)
	}
}
