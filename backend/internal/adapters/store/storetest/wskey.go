package storetest

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/core/wskey"
)

// KeyStore is the persistence an implementation offers to the wskey package, plus what the suite needs to set up.
type KeyStore interface {
	wskey.Store
	workspace.Store
	vault.Store
	auth.UserStore
	site.Store
}

// KeyFactory returns a new, empty store for one test.
type KeyFactory func(t *testing.T) KeyStore

// RunKeys executes the workspace key store suite against stores built by newStore.
//
// Scenarios:
//   - a user without a vault, or with a vault but no key pair, has no keys; a key pair is stored once and read back intact
//   - changing the vault of a user keeps their key pair
//   - creating a team stores the workspace, the owner membership and the first copy of its key together, and a failure leaves nothing
//   - a copy is read back, a missing copy is ErrPending, and adding a copy twice keeps the first
//   - the members that wait for a copy are those with a public key and no copy at that version, and the members with a copy are listed
//   - the key state of an unknown workspace is ErrNotFound
//   - removing a member deletes their copies of the key and marks the team as needing a new key, and removing the owner does neither
//   - rotating locks the workspace and makes the new version current with the copy of the member who did it, and a second rotation from the old version is ErrConflict
//   - the secrets sealed with older keys are listed, sealed again only while unchanged, and the copies of keys nothing uses are deleted
func RunKeys(t *testing.T, newStore KeyFactory) {
	t.Helper()
	ctx := t.Context()

	type world struct {
		store            KeyStore
		alice, bob, cara auth.User
	}
	setup := func(t *testing.T) world {
		t.Helper()
		w := world{store: newStore(t), alice: newUser("alice@example.com"), bob: newUser("bob@example.com"), cara: newUser("cara@example.com")}
		for i, u := range []auth.User{w.alice, w.bob, w.cara} {
			if err := w.store.CreateUser(ctx, u); err != nil {
				t.Fatalf("CreateUser() = %v", err)
			}
			if i < 2 {
				if err := w.store.CreateVault(ctx, u.ID, enrollment(byte(i*10))); err != nil {
					t.Fatalf("CreateVault() = %v", err)
				}
			}
		}
		return w
	}
	pair := func(tag byte) wskey.UserKeys {
		return wskey.UserKeys{Public: bytes.Repeat([]byte{tag}, 32), SealedPrivate: bytes.Repeat([]byte{tag + 1}, 60)}
	}

	t.Run("user keys", func(t *testing.T) {
		w := setup(t)
		for name, user := range map[string]uuid.UUID{"without a vault": w.cara.ID, "with an empty vault": w.alice.ID} {
			if _, err := w.store.UserKeys(ctx, user); !errors.Is(err, wskey.ErrNoKeys) {
				t.Fatalf("UserKeys() %s = %v; want ErrNoKeys", name, err)
			}
		}
		if stored, err := w.store.SetUserKeys(ctx, w.alice.ID, pair(5)); err != nil || !stored {
			t.Fatalf("SetUserKeys() = %v, %v; want it stored", stored, err)
		}
		if stored, err := w.store.SetUserKeys(ctx, w.alice.ID, pair(6)); err != nil || stored {
			t.Fatalf("second SetUserKeys() = %v, %v; want the first pair kept", stored, err)
		}
		if stored, err := w.store.SetUserKeys(ctx, w.cara.ID, pair(7)); err != nil || stored {
			t.Fatalf("SetUserKeys() without a vault = %v, %v; want nothing stored", stored, err)
		}
		got, err := w.store.UserKeys(ctx, w.alice.ID)
		if err != nil || !bytes.Equal(got.Public, pair(5).Public) || !bytes.Equal(got.SealedPrivate, pair(5).SealedPrivate) {
			t.Fatalf("UserKeys() = %+v, %v; want the first pair", got, err)
		}
		if err := w.store.UpdateVault(ctx, w.alice.ID, enrollment(40)); err != nil {
			t.Fatalf("UpdateVault() = %v", err)
		}
		if got, err := w.store.UserKeys(ctx, w.alice.ID); err != nil || !bytes.Equal(got.Public, pair(5).Public) {
			t.Fatalf("UserKeys() after UpdateVault = %+v, %v; want the pair kept", got, err)
		}
	})

	t.Run("create a team", func(t *testing.T) {
		w := setup(t)
		team := newWorkspace(w.alice.ID, workspace.KindTeam, "Ops")
		if err := w.store.CreateTeam(ctx, team, []byte("sealed-1")); err != nil {
			t.Fatalf("CreateTeam() = %v", err)
		}
		if role, err := w.store.RoleOf(ctx, team.ID, w.alice.ID); err != nil || role != workspace.RoleOwner {
			t.Fatalf("RoleOf() = %q, %v; want owner", role, err)
		}
		if version, pending, err := w.store.KeyState(ctx, team.ID); err != nil || version != 1 || pending {
			t.Fatalf("KeyState() = %d, %v, %v; want version 1 and nothing pending", version, pending, err)
		}
		if got, err := w.store.Grant(ctx, team.ID, w.alice.ID, 1); err != nil || string(got) != "sealed-1" {
			t.Fatalf("Grant() = %q, %v", got, err)
		}
		clash := newWorkspace(w.alice.ID, workspace.KindTeam, "Ops")
		clash.ID = team.ID
		if err := w.store.CreateTeam(ctx, clash, []byte("other")); err == nil {
			t.Fatal("a second team with the same identifier was created")
		}
		if got, _ := w.store.Grant(ctx, team.ID, w.alice.ID, 1); string(got) != "sealed-1" {
			t.Fatalf("the failed creation changed the first copy to %q", got)
		}
		if _, _, err := w.store.KeyState(ctx, uuid.New()); !errors.Is(err, workspace.ErrNotFound) {
			t.Fatalf("KeyState(unknown) = %v; want ErrNotFound", err)
		}
	})

	t.Run("copies and waiting members", func(t *testing.T) {
		w := setup(t)
		team := newWorkspace(w.alice.ID, workspace.KindTeam, "Ops")
		if err := w.store.CreateTeam(ctx, team, []byte("alice-1")); err != nil {
			t.Fatal(err)
		}
		for _, u := range []auth.User{w.bob, w.cara} {
			if err := w.store.AddMember(ctx, team.ID, u.ID, workspace.RoleEditor); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := w.store.Grant(ctx, team.ID, w.bob.ID, 1); !errors.Is(err, wskey.ErrPending) {
			t.Fatalf("Grant(missing) = %v; want ErrPending", err)
		}
		if _, err := w.store.SetUserKeys(ctx, w.alice.ID, pair(1)); err != nil {
			t.Fatal(err)
		}
		if _, err := w.store.SetUserKeys(ctx, w.bob.ID, pair(2)); err != nil {
			t.Fatal(err)
		}
		waiting, err := w.store.Ungranted(ctx, team.ID, 1)
		if err != nil || len(waiting) != 1 || waiting[0].UserID != w.bob.ID || !bytes.Equal(waiting[0].Public, pair(2).Public) {
			t.Fatalf("Ungranted() = %+v, %v; want bob only: alice has a copy and cara has no public key", waiting, err)
		}
		if err := w.store.AddGrant(ctx, team.ID, w.bob.ID, 1, []byte("bob-1")); err != nil {
			t.Fatalf("AddGrant() = %v", err)
		}
		if err := w.store.AddGrant(ctx, team.ID, w.bob.ID, 1, []byte("again")); err != nil {
			t.Fatalf("second AddGrant() = %v", err)
		}
		if got, _ := w.store.Grant(ctx, team.ID, w.bob.ID, 1); string(got) != "bob-1" {
			t.Fatalf("Grant() after adding twice = %q; want the first", got)
		}
		if waiting, _ := w.store.Ungranted(ctx, team.ID, 1); len(waiting) != 0 {
			t.Fatalf("Ungranted() after granting = %+v; want none", waiting)
		}
		if again, _ := w.store.Ungranted(ctx, team.ID, 2); len(again) != 2 {
			t.Fatalf("Ungranted() at version 2 = %+v; want alice and bob", again)
		}
		granted, err := w.store.Granted(ctx, team.ID, 1)
		if err != nil || len(granted) != 2 {
			t.Fatalf("Granted() = %v, %v; want alice and bob", granted, err)
		}
	})

	t.Run("removing a member", func(t *testing.T) {
		w := setup(t)
		team := newWorkspace(w.alice.ID, workspace.KindTeam, "Ops")
		if err := w.store.CreateTeam(ctx, team, []byte("alice-1")); err != nil {
			t.Fatal(err)
		}
		if err := w.store.AddMember(ctx, team.ID, w.bob.ID, workspace.RoleEditor); err != nil {
			t.Fatal(err)
		}
		if err := w.store.AddGrant(ctx, team.ID, w.bob.ID, 1, []byte("bob-1")); err != nil {
			t.Fatal(err)
		}
		if err := w.store.RemoveMember(ctx, team.ID, w.alice.ID); !errors.Is(err, workspace.ErrMemberNotFound) {
			t.Fatalf("RemoveMember(owner) = %v; want ErrMemberNotFound", err)
		}
		if _, pending, _ := w.store.KeyState(ctx, team.ID); pending {
			t.Fatal("refusing to remove the owner marked the key for rotation")
		}
		if err := w.store.RemoveMember(ctx, team.ID, w.bob.ID); err != nil {
			t.Fatalf("RemoveMember() = %v", err)
		}
		if _, err := w.store.Grant(ctx, team.ID, w.bob.ID, 1); !errors.Is(err, wskey.ErrPending) {
			t.Fatalf("Grant() of the removed member = %v; want ErrPending", err)
		}
		if got, err := w.store.Grant(ctx, team.ID, w.alice.ID, 1); err != nil || string(got) != "alice-1" {
			t.Fatalf("Grant() of the owner = %q, %v; want it kept", got, err)
		}
		if version, pending, err := w.store.KeyState(ctx, team.ID); err != nil || version != 1 || !pending {
			t.Fatalf("KeyState() = %d, %v, %v; want version 1 with a rotation waiting", version, pending, err)
		}
	})

	t.Run("rotation and old secrets", func(t *testing.T) {
		w := setup(t)
		team := newWorkspace(w.alice.ID, workspace.KindTeam, "Ops")
		if err := w.store.CreateTeam(ctx, team, []byte("alice-1")); err != nil {
			t.Fatal(err)
		}
		if err := w.store.AddMember(ctx, team.ID, w.bob.ID, workspace.RoleEditor); err != nil {
			t.Fatal(err)
		}
		if err := w.store.RemoveMember(ctx, team.ID, w.bob.ID); err != nil {
			t.Fatal(err)
		}
		host := newSite(team.ID, "Production")
		sealed := func(kind site.SecretKind, version int, tag byte) site.Sealed {
			return site.Sealed{Kind: kind, DEKVersion: version, Data: bytes.Repeat([]byte{tag}, 40)}
		}
		if err := w.store.CreateSite(ctx, host, []site.Sealed{sealed(site.SecretPassword, 1, 1), sealed(site.SecretPassphrase, 1, 2)}); err != nil {
			t.Fatal(err)
		}

		if err := w.store.Rotate(ctx, team.ID, 5, 6, w.alice.ID, []byte("stale")); !errors.Is(err, wskey.ErrConflict) {
			t.Fatalf("Rotate() from the wrong version = %v; want ErrConflict", err)
		}
		if err := w.store.Rotate(ctx, uuid.New(), 1, 2, w.alice.ID, []byte("x")); !errors.Is(err, workspace.ErrNotFound) {
			t.Fatalf("Rotate(unknown) = %v; want ErrNotFound", err)
		}
		if err := w.store.Rotate(ctx, team.ID, 1, 2, w.alice.ID, []byte("alice-2")); err != nil {
			t.Fatalf("Rotate() = %v", err)
		}
		if version, pending, _ := w.store.KeyState(ctx, team.ID); version != 2 || pending {
			t.Fatalf("KeyState() after Rotate = %d, %v; want version 2 with nothing waiting", version, pending)
		}
		if err := w.store.Rotate(ctx, team.ID, 1, 2, w.alice.ID, []byte("again")); !errors.Is(err, wskey.ErrConflict) {
			t.Fatalf("second Rotate() from version 1 = %v; want ErrConflict", err)
		}
		if got, _ := w.store.Grant(ctx, team.ID, w.alice.ID, 2); string(got) != "alice-2" {
			t.Fatalf("Grant() at version 2 = %q", got)
		}

		stale, err := w.store.Secrets(ctx, team.ID, 2)
		if err != nil || len(stale) != 2 || stale[0].Site != host.ID || stale[0].Kind != "passphrase" || stale[0].Version != 1 || !bytes.Equal(stale[0].Sealed, bytes.Repeat([]byte{2}, 40)) {
			t.Fatalf("Secrets() = %+v, %v; want the two secrets at version 1", stale, err)
		}
		if none, _ := w.store.Secrets(ctx, team.ID, 1); len(none) != 0 {
			t.Fatalf("Secrets() below version 1 = %+v; want none", none)
		}
		if other, _ := w.store.Secrets(ctx, uuid.New(), 2); len(other) != 0 {
			t.Fatalf("Secrets() of another workspace = %+v; want none", other)
		}

		if err := w.store.UpdateSite(ctx, host, []site.Sealed{sealed(site.SecretPassword, 2, 9)}, nil); err != nil {
			t.Fatal(err)
		}
		moved := []wskey.Resealed{
			{StoredSecret: stale[1], NewVersion: 2, NewSealed: bytes.Repeat([]byte{7}, 40)},
			{StoredSecret: stale[0], NewVersion: 2, NewSealed: bytes.Repeat([]byte{8}, 40)},
		}
		if err := w.store.Reseal(ctx, team.ID, moved); err != nil {
			t.Fatalf("Reseal() = %v", err)
		}
		password, _ := w.store.Secret(ctx, team.ID, host.ID, site.SecretPassword)
		passphrase, _ := w.store.Secret(ctx, team.ID, host.ID, site.SecretPassphrase)
		if password.DEKVersion != 2 || !bytes.Equal(password.Data, bytes.Repeat([]byte{9}, 40)) {
			t.Fatalf("a secret rewritten meanwhile = %+v; want the newer value kept", password)
		}
		if passphrase.DEKVersion != 2 || !bytes.Equal(passphrase.Data, bytes.Repeat([]byte{8}, 40)) {
			t.Fatalf("a secret sealed again = %+v", passphrase)
		}
		if _, err := w.store.Grant(ctx, team.ID, w.alice.ID, 1); !errors.Is(err, wskey.ErrPending) {
			t.Fatalf("Grant() at version 1 = %v; want it deleted, no secret uses that key", err)
		}
		if got, err := w.store.Grant(ctx, team.ID, w.alice.ID, 2); err != nil || string(got) != "alice-2" {
			t.Fatalf("Grant() at version 2 = %q, %v; want it kept", got, err)
		}
	})
}
