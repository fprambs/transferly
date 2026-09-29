package hostkey_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/hostkey"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/testutil"
)

// errStore is the failure injected by the fake store.
var errStore = errors.New("store failure")

// memStore is an in-memory hostkey.Store with switchable failures.
type memStore struct {
	keys       []hostkey.Key
	failGet    bool
	failAdd    bool
	failDelete bool
}

// Trusted returns the keys of a host and port.
func (m *memStore) Trusted(_ context.Context, _ uuid.UUID, host string, port int) ([]hostkey.Key, error) {
	if m.failGet {
		return nil, errStore
	}
	var out []hostkey.Key
	for _, k := range m.keys {
		if k.Host == host && k.Port == port {
			out = append(out, k)
		}
	}
	return out, nil
}

// List returns every key.
func (m *memStore) List(context.Context, uuid.UUID) ([]hostkey.Key, error) {
	if m.failGet {
		return nil, errStore
	}
	return m.keys, nil
}

// Add stores a key.
func (m *memStore) Add(_ context.Context, _ uuid.UUID, k hostkey.Key) error {
	if m.failAdd {
		return errStore
	}
	m.keys = append(m.keys, k)
	return nil
}

// Delete removes a key.
func (m *memStore) Delete(_ context.Context, _ uuid.UUID, host string, port int, keyType, fingerprint string) error {
	if m.failDelete {
		return errStore
	}
	kept := m.keys[:0]
	for _, k := range m.keys {
		if k.Host != host || k.Port != port || k.Type != keyType || (fingerprint != "" && k.Fingerprint != fingerprint) {
			kept = append(kept, k)
		}
	}
	if len(kept) == len(m.keys) {
		return hostkey.ErrNotFound
	}
	m.keys = kept
	return nil
}

// roles is a fake authorizer answering from a table of roles by user, using the real permission matrix.
type roles map[uuid.UUID]workspace.Role

// Authorize applies the permission matrix to the configured roles.
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

const (
	fpA = "SHA256:uU0nuZNNPgilLlLX2n2r+sSE7+N6U4DukIj3rOLvzek"
	fpB = "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8"
	fpC = "SHA256:Cccccccccccccccccccccccccccccccccccccccccc0"
)

// fixture bundles a service with its store and actors.
type fixture struct {
	svc    *hostkey.Service
	store  *memStore
	ws     uuid.UUID
	editor uuid.UUID
	viewer uuid.UUID
}

// newFixture returns a service where the editor may trust keys and the viewer may only list them.
func newFixture() *fixture {
	f := &fixture{store: &memStore{}, ws: uuid.New(), editor: uuid.New(), viewer: uuid.New()}
	f.svc = hostkey.NewService(f.store, roles{f.editor: workspace.RoleEditor, f.viewer: workspace.RoleViewer}, testutil.NewFakeClock(time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)))
	return f
}

// TestFingerprint verifies the OpenSSH notation of fingerprints.
//
// Scenarios:
//   - the digest is base64 without padding behind the SHA256 prefix, and the result passes validation
func TestFingerprint(t *testing.T) {
	got := hostkey.Fingerprint(sha256.Sum256([]byte("key blob")))
	if len(got) != len("SHA256:")+43 || got[:7] != "SHA256:" {
		t.Fatalf("Fingerprint() = %q", got)
	}
	f := newFixture()
	if _, err := f.svc.Trust(context.Background(), f.editor, f.ws, "a.example", 22, "ssh-ed25519", got, hostkey.TrustNew); err != nil {
		t.Errorf("a generated fingerprint was refused: %v", err)
	}
}

// TestService_TrustCheckForget verifies trust on first use.
//
// Scenarios:
//   - an unseen host is Unknown, trusting its key makes it Trusted, and the trusted types are reported
//   - trusting the same key again succeeds without storing it twice
//   - a different key of the same type is Changed, and trusting it is ErrChanged until the old key is forgotten
//   - a key of another type is Unknown, since connections insist on the trusted types
//   - malformed hosts, ports, key types and fingerprints are ErrInvalid
//   - viewers may list but not trust or forget, strangers get workspace.ErrNotFound
//   - forgetting a missing key is ErrNotFound
//   - store failures are returned by every operation
func TestService_TrustCheckForget(t *testing.T) {
	ctx := context.Background()
	f := newFixture()

	if v, _, err := f.svc.Check(ctx, f.ws, "a.example", 22, "ssh-ed25519", fpA); err != nil || v != hostkey.Unknown {
		t.Fatalf("Check(unseen) = %v, %v", v, err)
	}
	key, err := f.svc.Trust(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", fpA, hostkey.TrustNew)
	if err != nil || key.AcceptedBy != f.editor || key.FirstSeen.IsZero() {
		t.Fatalf("Trust() = %+v, %v", key, err)
	}
	if v, _, _ := f.svc.Check(ctx, f.ws, "a.example", 22, "ssh-ed25519", fpA); v != hostkey.Trusted {
		t.Errorf("Check(trusted) = %v", v)
	}
	if types, err := f.svc.Algorithms(ctx, f.ws, "a.example", 22); err != nil || len(types) != 1 || types[0] != "ssh-ed25519" {
		t.Errorf("Algorithms() = %v, %v", types, err)
	}
	if _, err := f.svc.Trust(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", fpA, hostkey.TrustNew); err != nil || len(f.store.keys) != 1 {
		t.Errorf("trusting twice = %v with %d stored", err, len(f.store.keys))
	}

	if v, _, _ := f.svc.Check(ctx, f.ws, "a.example", 22, "ssh-ed25519", fpB); v != hostkey.Changed {
		t.Errorf("Check(changed) = %v; want Changed", v)
	}
	if _, err := f.svc.Trust(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", fpB, hostkey.TrustNew); !errors.Is(err, hostkey.ErrChanged) {
		t.Errorf("Trust(changed) = %v; want ErrChanged", err)
	}
	if v, _, _ := f.svc.Check(ctx, f.ws, "a.example", 22, "ssh-rsa", fpB); v != hostkey.Unknown {
		t.Errorf("Check(other type) = %v; want Unknown", v)
	}
	if v, _, _ := f.svc.Check(ctx, f.ws, "a.example", 2222, "ssh-ed25519", fpB); v != hostkey.Unknown {
		t.Errorf("Check(other port) = %v; want Unknown", v)
	}

	for name, args := range map[string][4]any{
		"host":        {"bad host", 22, "ssh-ed25519", fpA},
		"port":        {"a.example", 0, "ssh-ed25519", fpA},
		"big port":    {"a.example", 65536, "ssh-ed25519", fpA},
		"type":        {"a.example", 22, "Bad Type", fpA},
		"fingerprint": {"a.example", 22, "ssh-ed25519", "MD5:aa:bb"},
	} {
		if _, err := f.svc.Trust(ctx, f.editor, f.ws, args[0].(string), args[1].(int), args[2].(string), args[3].(string), hostkey.TrustNew); !errors.Is(err, hostkey.ErrInvalid) {
			t.Errorf("invalid %s = %v; want ErrInvalid", name, err)
		}
	}

	stranger := uuid.New()
	if list, err := f.svc.List(ctx, f.viewer, f.ws); err != nil || len(list) != 1 {
		t.Errorf("viewer List() = %v, %v", list, err)
	}
	if _, err := f.svc.Trust(ctx, f.viewer, f.ws, "b.example", 22, "ssh-ed25519", fpA, hostkey.TrustNew); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer Trust() = %v; want ErrForbidden", err)
	}
	if err := f.svc.Forget(ctx, f.viewer, f.ws, "a.example", 22, "ssh-ed25519", ""); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("viewer Forget() = %v; want ErrForbidden", err)
	}
	if _, err := f.svc.List(ctx, stranger, f.ws); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("stranger List() = %v; want ErrNotFound", err)
	}

	if err := f.svc.Forget(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", ""); err != nil {
		t.Fatalf("Forget() = %v", err)
	}
	if err := f.svc.Forget(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", ""); !errors.Is(err, hostkey.ErrNotFound) {
		t.Errorf("second Forget() = %v; want ErrNotFound", err)
	}
	if _, err := f.svc.Trust(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", fpB, hostkey.TrustNew); err != nil {
		t.Errorf("trusting the new key after forgetting the old one = %v", err)
	}

	f.store.failAdd = true
	if _, err := f.svc.Trust(ctx, f.editor, f.ws, "c.example", 22, "ssh-ed25519", fpA, hostkey.TrustNew); !errors.Is(err, errStore) {
		t.Errorf("Trust() add failure = %v", err)
	}
	f.store.failGet = true
	if _, _, err := f.svc.Check(ctx, f.ws, "a.example", 22, "ssh-ed25519", fpA); !errors.Is(err, errStore) {
		t.Errorf("Check() failure = %v", err)
	}
	if _, err := f.svc.Algorithms(ctx, f.ws, "a.example", 22); !errors.Is(err, errStore) {
		t.Errorf("Algorithms() failure = %v", err)
	}
	if _, err := f.svc.Trust(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", fpA, hostkey.TrustNew); !errors.Is(err, errStore) {
		t.Errorf("Trust() read failure = %v", err)
	}
	if _, err := f.svc.List(ctx, f.editor, f.ws); !errors.Is(err, errStore) {
		t.Errorf("List() failure = %v", err)
	}
}

// TestService_SeveralKeys covers servers that present one of several keys (4.13b).
//
// Use cases:
//   - a key of a trusted type that is not trusted is changed, and the check names the keys trusted for the type
//   - adding it keeps the old key trusted too, so either passes; replacing drops the other keys of the type
//   - forgetting one key keeps the others of its type
//   - an unknown mode is invalid, and a store that fails while replacing fails the trust
func TestService_SeveralKeys(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	if _, err := f.svc.Trust(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", fpA, hostkey.TrustNew); err != nil {
		t.Fatal(err)
	}
	if v, trusted, err := f.svc.Check(ctx, f.ws, "a.example", 22, "ssh-ed25519", fpB); err != nil || v != hostkey.Changed || len(trusted) != 1 || trusted[0] != fpA {
		t.Fatalf("Check() of another key = %v, %v, %v", v, trusted, err)
	}

	if _, err := f.svc.Trust(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", fpB, hostkey.TrustAdd); err != nil {
		t.Fatalf("Trust(add) = %v", err)
	}
	for _, fp := range []string{fpA, fpB} {
		if v, _, _ := f.svc.Check(ctx, f.ws, "a.example", 22, "ssh-ed25519", fp); v != hostkey.Trusted {
			t.Errorf("Check(%s) after adding = %v; want trusted", fp, v)
		}
	}
	if _, trusted, _ := f.svc.Check(ctx, f.ws, "a.example", 22, "ssh-ed25519", fpC); len(trusted) != 2 {
		t.Errorf("trusted keys of the type = %v; want both", trusted)
	}

	if _, err := f.svc.Trust(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", fpC, hostkey.TrustReplace); err != nil {
		t.Fatalf("Trust(replace) = %v", err)
	}
	if v, _, _ := f.svc.Check(ctx, f.ws, "a.example", 22, "ssh-ed25519", fpA); v != hostkey.Changed {
		t.Errorf("an old key after replacing = %v; want changed", v)
	}
	if _, err := f.svc.Trust(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", fpA, hostkey.TrustAdd); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Forget(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", fpA); err != nil {
		t.Fatalf("Forget() of one key = %v", err)
	}
	if v, _, _ := f.svc.Check(ctx, f.ws, "a.example", 22, "ssh-ed25519", fpC); v != hostkey.Trusted {
		t.Errorf("the other key after forgetting one = %v; want trusted", v)
	}

	if _, err := f.svc.Trust(ctx, f.editor, f.ws, "b.example", 22, "ssh-ed25519", fpA, "merge"); !errors.Is(err, hostkey.ErrInvalid) {
		t.Errorf("Trust() with an unknown mode = %v; want ErrInvalid", err)
	}
	f.store.failDelete = true
	if _, err := f.svc.Trust(ctx, f.editor, f.ws, "a.example", 22, "ssh-ed25519", fpB, hostkey.TrustReplace); !errors.Is(err, errStore) {
		t.Errorf("Trust(replace) with a failing store = %v; want the store error", err)
	}
}
