package storetest

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/hostkey"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// HostKeyStore is the persistence an implementation offers to the hostkey package, plus what a key needs to exist.
type HostKeyStore interface {
	hostkey.Store
	workspace.Store
	auth.UserStore
}

// HostKeyFactory returns a new, empty store for one test.
type HostKeyFactory func(t *testing.T) HostKeyStore

// RunHostKey executes the host key store suite against stores built by newStore.
//
// Scenarios:
//   - a key round trips with UTC time and the user who trusted it
//   - keys are found per host and port, ordered by type, and listed per workspace ordered by host, port and type
//   - adding a key that exists is ignored and keeps the first one
//   - workspaces are isolated from each other
//   - deleting removes one key only, and deleting a missing key is ErrNotFound
//   - a key whose accepting user was deleted is still listed, with the nil identifier
func RunHostKey(t *testing.T, newStore HostKeyFactory) {
	t.Helper()
	ctx := t.Context()

	setup := func(t *testing.T) (HostKeyStore, uuid.UUID, uuid.UUID, auth.User) {
		t.Helper()
		store := newStore(t)
		var ws [2]uuid.UUID
		var alice auth.User
		for i, email := range []string{"alice@example.com", "bob@example.com"} {
			u := newUser(email)
			if err := store.CreateUser(ctx, u); err != nil {
				t.Fatal(err)
			}
			w := newWorkspace(u.ID, workspace.KindPersonal, "Personal")
			if err := store.CreateWorkspace(ctx, w); err != nil {
				t.Fatal(err)
			}
			ws[i] = w.ID
			if i == 0 {
				alice = u
			}
		}
		return store, ws[0], ws[1], alice
	}
	key := func(host string, port int, keyType, fp string, by uuid.UUID) hostkey.Key {
		return hostkey.Key{Host: host, Port: port, Type: keyType, Fingerprint: fp, FirstSeen: now, AcceptedBy: by}
	}

	t.Run("round trip and ordering", func(t *testing.T) {
		store, ws, _, alice := setup(t)
		for _, k := range []hostkey.Key{
			key("b.example", 22, "ssh-ed25519", "SHA256:b", alice.ID),
			key("a.example", 2222, "ssh-rsa", "SHA256:a2", alice.ID),
			key("a.example", 22, "ssh-rsa", "SHA256:a1", alice.ID),
			key("a.example", 22, "ecdsa-sha2-nistp256", "SHA256:a0", alice.ID),
		} {
			if err := store.Add(ctx, ws, k); err != nil {
				t.Fatalf("Add() = %v", err)
			}
		}
		got, err := store.Trusted(ctx, ws, "a.example", 22)
		if err != nil || len(got) != 2 || got[0].Type != "ecdsa-sha2-nistp256" || got[1].Type != "ssh-rsa" {
			t.Fatalf("Trusted() = %+v, %v", got, err)
		}
		if got[1] != key("a.example", 22, "ssh-rsa", "SHA256:a1", alice.ID) || got[1].FirstSeen.Location().String() != "UTC" {
			t.Errorf("round trip changed the key: %+v", got[1])
		}
		all, err := store.List(ctx, ws)
		if err != nil || len(all) != 4 || all[0].Port != 22 || all[1].Port != 22 || all[2].Port != 2222 || all[3].Host != "b.example" {
			t.Fatalf("List() = %+v, %v", all, err)
		}
		if none, err := store.Trusted(ctx, ws, "unknown.example", 22); err != nil || len(none) != 0 {
			t.Errorf("Trusted(unknown) = %v, %v; want empty", none, err)
		}
	})

	t.Run("duplicates and isolation", func(t *testing.T) {
		store, ws, other, alice := setup(t)
		first := key("a.example", 22, "ssh-ed25519", "SHA256:first", alice.ID)
		if err := store.Add(ctx, ws, first); err != nil {
			t.Fatal(err)
		}
		if err := store.Add(ctx, ws, key("a.example", 22, "ssh-ed25519", "SHA256:second", alice.ID)); err != nil {
			t.Fatalf("adding an existing key = %v; want it ignored", err)
		}
		if got, _ := store.Trusted(ctx, ws, "a.example", 22); len(got) != 1 || got[0].Fingerprint != "SHA256:first" {
			t.Errorf("Trusted() after a duplicate = %+v; want the first key", got)
		}
		if got, _ := store.List(ctx, other); len(got) != 0 {
			t.Errorf("another workspace sees %+v", got)
		}
		if err := store.Delete(ctx, other, "a.example", 22, "ssh-ed25519"); !errors.Is(err, hostkey.ErrNotFound) {
			t.Errorf("delete through another workspace = %v; want ErrNotFound", err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		store, ws, _, alice := setup(t)
		for _, typ := range []string{"ssh-ed25519", "ssh-rsa"} {
			if err := store.Add(ctx, ws, key("a.example", 22, typ, "SHA256:"+typ, alice.ID)); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Delete(ctx, ws, "a.example", 22, "ssh-rsa"); err != nil {
			t.Fatalf("Delete() = %v", err)
		}
		if got, _ := store.Trusted(ctx, ws, "a.example", 22); len(got) != 1 || got[0].Type != "ssh-ed25519" {
			t.Errorf("Trusted() after delete = %+v", got)
		}
		if err := store.Delete(ctx, ws, "a.example", 22, "ssh-rsa"); !errors.Is(err, hostkey.ErrNotFound) {
			t.Errorf("second delete = %v; want ErrNotFound", err)
		}
	})

	t.Run("without accepting user", func(t *testing.T) {
		store, ws, _, _ := setup(t)
		if err := store.Add(ctx, ws, key("a.example", 22, "ssh-ed25519", "SHA256:x", uuid.Nil)); err != nil {
			t.Fatalf("Add() without a user = %v", err)
		}
		if got, err := store.List(ctx, ws); err != nil || len(got) != 1 || got[0].AcceptedBy != uuid.Nil {
			t.Errorf("List() = %+v, %v", got, err)
		}
	})
}
