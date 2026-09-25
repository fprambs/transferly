package storetest

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// SiteStore is the persistence an implementation offers to the site package, plus what a site needs to exist.
type SiteStore interface {
	site.Store
	workspace.Store
	auth.UserStore
}

// SiteFactory returns a new, empty store for one test.
type SiteFactory func(t *testing.T) SiteStore

// newSite returns a valid site of a workspace.
func newSite(workspaceID uuid.UUID, name string) site.Site {
	return site.Site{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: name, Protocol: site.SFTP, Host: "files.example.com", Port: 2222,
		Username: "deploy", AuthMethod: site.AuthKey, ListTimezone: "America/Santiago", CreatedAt: now, UpdatedAt: now,
	}
}

// RunSite executes the site store suite against stores built by newStore.
//
// Scenarios:
//   - a site round trips with every field and its secret kinds, with UTC times
//   - names are unique per workspace regardless of case, and reusable in another workspace
//   - update replaces settings, writes and removes secrets, and keeps the creation time
//   - updating or reading a site through another workspace is ErrNotFound
//   - listing returns the workspace's sites by name, each with its own secret kinds
//   - deleting removes the site and its secrets, and other workspaces cannot delete it
//   - secrets are read back intact, missing kinds are ErrNoSecret and other workspaces cannot read them
//   - a failed creation stores neither the site nor its secrets
func RunSite(t *testing.T, newStore SiteFactory) {
	t.Helper()
	ctx := t.Context()

	setup := func(t *testing.T) (SiteStore, uuid.UUID, uuid.UUID) {
		t.Helper()
		store := newStore(t)
		var ids [2]uuid.UUID
		for i, email := range []string{"alice@example.com", "bob@example.com"} {
			u := newUser(email)
			if err := store.CreateUser(ctx, u); err != nil {
				t.Fatalf("CreateUser() = %v", err)
			}
			w := newWorkspace(u.ID, workspace.KindPersonal, "Personal")
			if err := store.CreateWorkspace(ctx, w); err != nil {
				t.Fatalf("CreateWorkspace() = %v", err)
			}
			ids[i] = w.ID
		}
		return store, ids[0], ids[1]
	}
	secret := func(kind site.SecretKind, fill byte) site.Sealed {
		return site.Sealed{Kind: kind, DEKVersion: 1, Data: bytes.Repeat([]byte{fill}, 40)}
	}

	t.Run("round trip", func(t *testing.T) {
		store, ws, _ := setup(t)
		want := newSite(ws, "Production")
		if err := store.CreateSite(ctx, want, []site.Sealed{secret(site.SecretPrivateKey, 1), secret(site.SecretPassphrase, 2)}); err != nil {
			t.Fatalf("CreateSite() = %v", err)
		}
		got, err := store.Site(ctx, ws, want.ID)
		if err != nil {
			t.Fatalf("Site() = %v", err)
		}
		want.Secrets = []site.SecretKind{site.SecretPassphrase, site.SecretPrivateKey}
		if got.ID != want.ID || got.Name != want.Name || got.Protocol != want.Protocol || got.Host != want.Host || got.Port != want.Port ||
			got.Username != want.Username || got.AuthMethod != want.AuthMethod || got.ListTimezone != want.ListTimezone ||
			!got.CreatedAt.Equal(now) || got.CreatedAt.Location().String() != "UTC" || len(got.Secrets) != 2 ||
			got.Secrets[0] != want.Secrets[0] || got.Secrets[1] != want.Secrets[1] {
			t.Fatalf("Site() = %+v; want %+v", got, want)
		}
	})

	t.Run("unique names", func(t *testing.T) {
		store, ws, other := setup(t)
		if err := store.CreateSite(ctx, newSite(ws, "Production"), nil); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateSite(ctx, newSite(ws, "PRODUCTION"), nil); !errors.Is(err, site.ErrNameTaken) {
			t.Fatalf("duplicate name = %v; want ErrNameTaken", err)
		}
		if err := store.CreateSite(ctx, newSite(other, "Production"), nil); err != nil {
			t.Fatalf("same name in another workspace = %v", err)
		}
		staging := newSite(ws, "Staging")
		if err := store.CreateSite(ctx, staging, nil); err != nil {
			t.Fatal(err)
		}
		staging.Name = "production"
		if err := store.UpdateSite(ctx, staging, nil, nil); !errors.Is(err, site.ErrNameTaken) {
			t.Fatalf("rename onto an existing name = %v; want ErrNameTaken", err)
		}
	})

	t.Run("failed creation is atomic", func(t *testing.T) {
		store, ws, _ := setup(t)
		if err := store.CreateSite(ctx, newSite(ws, "One"), nil); err != nil {
			t.Fatal(err)
		}
		dup := newSite(ws, "one")
		if err := store.CreateSite(ctx, dup, []site.Sealed{secret(site.SecretPassword, 9)}); !errors.Is(err, site.ErrNameTaken) {
			t.Fatalf("CreateSite() = %v; want ErrNameTaken", err)
		}
		if _, err := store.Secret(ctx, ws, dup.ID, site.SecretPassword); !errors.Is(err, site.ErrNoSecret) {
			t.Fatalf("a secret of a failed creation was stored: %v", err)
		}
	})

	t.Run("update", func(t *testing.T) {
		store, ws, other := setup(t)
		s := newSite(ws, "Production")
		if err := store.CreateSite(ctx, s, []site.Sealed{secret(site.SecretPassword, 1), secret(site.SecretPassphrase, 2)}); err != nil {
			t.Fatal(err)
		}
		changed := s
		changed.Name, changed.Host, changed.Port, changed.Protocol, changed.AuthMethod = "Prod", "other.example.com", 22, site.FTPS, site.AuthPassword
		changed.UpdatedAt = now.Add(time.Second)
		changed.CreatedAt = now.Add(time.Hour)
		if err := store.UpdateSite(ctx, changed, []site.Sealed{secret(site.SecretPassword, 7)}, []site.SecretKind{site.SecretPassphrase}); err != nil {
			t.Fatalf("UpdateSite() = %v", err)
		}
		got, err := store.Site(ctx, ws, s.ID)
		if err != nil || got.Name != "Prod" || got.Host != "other.example.com" || got.Port != 22 || got.Protocol != site.FTPS ||
			!got.CreatedAt.Equal(now) || !got.UpdatedAt.Equal(now.Add(time.Second)) || len(got.Secrets) != 1 || got.Secrets[0] != site.SecretPassword {
			t.Fatalf("Site() after update = %+v, %v", got, err)
		}
		if sealed, err := store.Secret(ctx, ws, s.ID, site.SecretPassword); err != nil || sealed.Data[0] != 7 {
			t.Fatalf("replaced secret = %+v, %v", sealed, err)
		}
		if err := store.UpdateSite(ctx, changed, nil, nil); err != nil {
			t.Fatalf("UpdateSite() without secret changes = %v", err)
		}

		foreign := changed
		foreign.WorkspaceID = other
		if err := store.UpdateSite(ctx, foreign, nil, nil); !errors.Is(err, site.ErrNotFound) {
			t.Fatalf("update through another workspace = %v; want ErrNotFound", err)
		}
		if err := store.UpdateSite(ctx, newSite(ws, "Ghost"), nil, nil); !errors.Is(err, site.ErrNotFound) {
			t.Fatalf("update of a missing site = %v; want ErrNotFound", err)
		}
	})

	t.Run("listing", func(t *testing.T) {
		store, ws, other := setup(t)
		b, a := newSite(ws, "bravo"), newSite(ws, "Alpha")
		for _, s := range []site.Site{b, a, newSite(other, "Foreign")} {
			var secrets []site.Sealed
			if s.Name == "bravo" {
				secrets = []site.Sealed{secret(site.SecretPassword, 1)}
			}
			if err := store.CreateSite(ctx, s, secrets); err != nil {
				t.Fatal(err)
			}
		}
		list, err := store.Sites(ctx, ws)
		if err != nil || len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
			t.Fatalf("Sites() = %+v, %v; want Alpha then bravo", list, err)
		}
		if len(list[0].Secrets) != 0 || len(list[1].Secrets) != 1 || list[1].Secrets[0] != site.SecretPassword {
			t.Errorf("secret kinds = %v and %v", list[0].Secrets, list[1].Secrets)
		}
		if empty, err := store.Sites(ctx, uuid.New()); err != nil || len(empty) != 0 {
			t.Errorf("Sites(unknown) = %v, %v; want empty", empty, err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		store, ws, other := setup(t)
		s := newSite(ws, "Production")
		if err := store.CreateSite(ctx, s, []site.Sealed{secret(site.SecretPassword, 1)}); err != nil {
			t.Fatal(err)
		}
		if err := store.DeleteSite(ctx, other, s.ID); !errors.Is(err, site.ErrNotFound) {
			t.Fatalf("delete through another workspace = %v; want ErrNotFound", err)
		}
		if err := store.DeleteSite(ctx, ws, s.ID); err != nil {
			t.Fatalf("DeleteSite() = %v", err)
		}
		if _, err := store.Site(ctx, ws, s.ID); !errors.Is(err, site.ErrNotFound) {
			t.Errorf("Site() after delete = %v; want ErrNotFound", err)
		}
		if _, err := store.Secret(ctx, ws, s.ID, site.SecretPassword); !errors.Is(err, site.ErrNoSecret) {
			t.Errorf("Secret() after delete = %v; want ErrNoSecret", err)
		}
		if err := store.DeleteSite(ctx, ws, s.ID); !errors.Is(err, site.ErrNotFound) {
			t.Errorf("second delete = %v; want ErrNotFound", err)
		}
	})

	t.Run("secrets", func(t *testing.T) {
		store, ws, other := setup(t)
		s := newSite(ws, "Production")
		if err := store.CreateSite(ctx, s, []site.Sealed{secret(site.SecretPassword, 5)}); err != nil {
			t.Fatal(err)
		}
		got, err := store.Secret(ctx, ws, s.ID, site.SecretPassword)
		if err != nil || got.Kind != site.SecretPassword || got.DEKVersion != 1 || !bytes.Equal(got.Data, secret(site.SecretPassword, 5).Data) {
			t.Fatalf("Secret() = %+v, %v", got, err)
		}
		if _, err := store.Secret(ctx, ws, s.ID, site.SecretPrivateKey); !errors.Is(err, site.ErrNoSecret) {
			t.Errorf("missing kind = %v; want ErrNoSecret", err)
		}
		if _, err := store.Secret(ctx, other, s.ID, site.SecretPassword); !errors.Is(err, site.ErrNoSecret) {
			t.Errorf("read through another workspace = %v; want ErrNoSecret", err)
		}
	})
}
