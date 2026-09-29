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
	site.CollectionStore
	workspace.Store
	auth.UserStore
}

// SiteFactory returns a new, empty store for one test.
type SiteFactory func(t *testing.T) SiteStore

// newSite returns a valid site of a workspace.
func newSite(workspaceID uuid.UUID, name string) site.Site {
	return site.Site{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: name, Protocol: site.SFTP, Host: "files.example.com", Port: 2222,
		Username: "deploy", AuthMethod: site.AuthKey, ListTimezone: "America/Santiago", Charset: "koi8-r", SpeedLimit: 65536, RemoteDir: "/srv/www",
		Proxy:     site.Proxy{Kind: site.ProxySOCKS5, Host: "proxy.example.com", Port: 1080, Username: "proxyuser"},
		Jump:      site.Jump{Host: "bastion.example.com", Port: 2222, Username: "bastion", AuthMethod: site.AuthKey},
		CreatedAt: now, UpdatedAt: now,
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
			got.SpeedLimit != want.SpeedLimit || got.Proxy != want.Proxy || got.Jump != want.Jump ||
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
		changed.Name, changed.Host, changed.Port, changed.Protocol, changed.AuthMethod = "Prod", "other.example.com", 22, site.FTP, site.AuthAnonymous
		changed.FTPEncryption = site.EncryptionImplicit
		changed.SpeedLimit = 4096
		changed.Proxy = site.Proxy{Kind: site.ProxyHTTP, Host: "other-proxy.example.com", Port: 8080, Username: "other"}
		changed.Jump = site.Jump{Host: "other-bastion.example.com", Port: 22, Username: "other", AuthMethod: site.AuthPassword}
		changed.UpdatedAt = now.Add(time.Second)
		changed.CreatedAt = now.Add(time.Hour)
		if err := store.UpdateSite(ctx, changed, []site.Sealed{secret(site.SecretPassword, 7)}, []site.SecretKind{site.SecretPassphrase}); err != nil {
			t.Fatalf("UpdateSite() = %v", err)
		}
		got, err := store.Site(ctx, ws, s.ID)
		if err != nil || got.Name != "Prod" || got.Host != "other.example.com" || got.Port != 22 || got.Protocol != site.FTP || got.AuthMethod != site.AuthAnonymous || got.FTPEncryption != site.EncryptionImplicit ||
			got.SpeedLimit != 4096 || got.Proxy != changed.Proxy || got.Jump != changed.Jump ||
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

// RunCollection executes the collection store suite against stores built by newStore.
//
// Scenarios:
//   - a collection round trips, at the top level and inside another one
//   - names are unique among siblings regardless of case, and reusable under another parent or at the top level
//   - renaming and moving update the collection, and clashes and missing collections are reported
//   - listing returns the workspace's collections by name and no others
//   - a site keeps its collection through creation and update, and moves back to the top level
//   - deleting works for empty collections only, whether they hold a site or another collection
//   - other workspaces cannot read, change or delete a collection
func RunCollection(t *testing.T, newStore SiteFactory) {
	t.Helper()
	ctx := t.Context()

	setup := func(t *testing.T) (SiteStore, uuid.UUID, uuid.UUID) {
		t.Helper()
		store := newStore(t)
		var ids [2]uuid.UUID
		for i, email := range []string{"alice@example.com", "bob@example.com"} {
			u := newUser(email)
			if err := store.CreateUser(ctx, u); err != nil {
				t.Fatal(err)
			}
			w := newWorkspace(u.ID, workspace.KindPersonal, "Personal")
			if err := store.CreateWorkspace(ctx, w); err != nil {
				t.Fatal(err)
			}
			ids[i] = w.ID
		}
		return store, ids[0], ids[1]
	}
	collection := func(ws, parent uuid.UUID, name string) site.Collection {
		return site.Collection{ID: uuid.New(), WorkspaceID: ws, ParentID: parent, Name: name}
	}

	t.Run("round trip", func(t *testing.T) {
		store, ws, _ := setup(t)
		top := collection(ws, uuid.Nil, "Acme")
		inner := collection(ws, top.ID, "Production")
		for _, c := range []site.Collection{top, inner} {
			if err := store.CreateCollection(ctx, c); err != nil {
				t.Fatalf("CreateCollection() = %v", err)
			}
		}
		for _, want := range []site.Collection{top, inner} {
			if got, err := store.Collection(ctx, ws, want.ID); err != nil || got != want {
				t.Errorf("Collection() = %+v, %v; want %+v", got, err, want)
			}
		}
	})

	t.Run("unique names", func(t *testing.T) {
		store, ws, other := setup(t)
		acme, clients := collection(ws, uuid.Nil, "Acme"), collection(ws, uuid.Nil, "Clients")
		for _, c := range []site.Collection{acme, clients, collection(ws, acme.ID, "Prod")} {
			if err := store.CreateCollection(ctx, c); err != nil {
				t.Fatal(err)
			}
		}
		for _, dup := range []site.Collection{collection(ws, uuid.Nil, "ACME"), collection(ws, acme.ID, "prod")} {
			if err := store.CreateCollection(ctx, dup); !errors.Is(err, site.ErrCollectionNameTaken) {
				t.Errorf("duplicate %q = %v; want ErrCollectionNameTaken", dup.Name, err)
			}
		}
		for _, ok := range []site.Collection{collection(ws, clients.ID, "Prod"), collection(ws, uuid.Nil, "Prod"), collection(other, uuid.Nil, "Acme")} {
			if err := store.CreateCollection(ctx, ok); err != nil {
				t.Errorf("reusing the name %q elsewhere = %v", ok.Name, err)
			}
		}
	})

	t.Run("update", func(t *testing.T) {
		store, ws, other := setup(t)
		a, b, c := collection(ws, uuid.Nil, "A"), collection(ws, uuid.Nil, "B"), collection(ws, uuid.Nil, "C")
		for _, x := range []site.Collection{a, b, c} {
			if err := store.CreateCollection(ctx, x); err != nil {
				t.Fatal(err)
			}
		}
		a.Name, a.ParentID = "Renamed", b.ID
		if err := store.UpdateCollection(ctx, a); err != nil {
			t.Fatalf("UpdateCollection() = %v", err)
		}
		if got, _ := store.Collection(ctx, ws, a.ID); got != a {
			t.Errorf("after update = %+v; want %+v", got, a)
		}
		a.ParentID = uuid.Nil
		if err := store.UpdateCollection(ctx, a); err != nil {
			t.Fatalf("moving to the top level = %v", err)
		}
		c.Name = "b"
		if err := store.UpdateCollection(ctx, c); !errors.Is(err, site.ErrCollectionNameTaken) {
			t.Errorf("rename onto a sibling = %v; want ErrCollectionNameTaken", err)
		}
		ghost := collection(ws, uuid.Nil, "Ghost")
		if err := store.UpdateCollection(ctx, ghost); !errors.Is(err, site.ErrCollectionNotFound) {
			t.Errorf("update of a missing collection = %v; want ErrCollectionNotFound", err)
		}
		foreign := a
		foreign.WorkspaceID = other
		if err := store.UpdateCollection(ctx, foreign); !errors.Is(err, site.ErrCollectionNotFound) {
			t.Errorf("update through another workspace = %v; want ErrCollectionNotFound", err)
		}
	})

	t.Run("listing and privacy", func(t *testing.T) {
		store, ws, other := setup(t)
		zeta, alpha, foreign := collection(ws, uuid.Nil, "zeta"), collection(ws, uuid.Nil, "Alpha"), collection(other, uuid.Nil, "Foreign")
		for _, x := range []site.Collection{zeta, alpha, foreign} {
			if err := store.CreateCollection(ctx, x); err != nil {
				t.Fatal(err)
			}
		}
		list, err := store.Collections(ctx, ws)
		if err != nil || len(list) != 2 || list[0].ID != alpha.ID || list[1].ID != zeta.ID {
			t.Fatalf("Collections() = %+v, %v; want Alpha then zeta", list, err)
		}
		if _, err := store.Collection(ctx, other, alpha.ID); !errors.Is(err, site.ErrCollectionNotFound) {
			t.Errorf("Collection() through another workspace = %v", err)
		}
		if err := store.DeleteCollection(ctx, other, alpha.ID); !errors.Is(err, site.ErrCollectionNotFound) {
			t.Errorf("DeleteCollection() through another workspace = %v", err)
		}
		if none, _ := store.Collections(ctx, uuid.New()); len(none) != 0 {
			t.Errorf("Collections(unknown) = %v", none)
		}
	})

	t.Run("sites and deletion", func(t *testing.T) {
		store, ws, _ := setup(t)
		top, inner, empty := collection(ws, uuid.Nil, "Top"), collection(ws, uuid.Nil, "Inner"), collection(ws, uuid.Nil, "Empty")
		nested := collection(ws, top.ID, "Nested")
		for _, x := range []site.Collection{top, inner, empty, nested} {
			if err := store.CreateCollection(ctx, x); err != nil {
				t.Fatal(err)
			}
		}
		s := newSite(ws, "Files")
		s.CollectionID = inner.ID
		if err := store.CreateSite(ctx, s, nil); err != nil {
			t.Fatal(err)
		}
		if got, _ := store.Site(ctx, ws, s.ID); got.CollectionID != inner.ID {
			t.Errorf("site collection = %v; want %v", got.CollectionID, inner.ID)
		}
		if list, _ := store.Sites(ctx, ws); len(list) != 1 || list[0].CollectionID != inner.ID {
			t.Errorf("listed site collection = %+v", list)
		}
		if err := store.DeleteCollection(ctx, ws, inner.ID); !errors.Is(err, site.ErrCollectionNotEmpty) {
			t.Errorf("deleting a collection with a site = %v; want ErrCollectionNotEmpty", err)
		}
		if err := store.DeleteCollection(ctx, ws, top.ID); !errors.Is(err, site.ErrCollectionNotEmpty) {
			t.Errorf("deleting a collection with a collection = %v; want ErrCollectionNotEmpty", err)
		}
		s.CollectionID = uuid.Nil
		if err := store.UpdateSite(ctx, s, nil, nil); err != nil {
			t.Fatal(err)
		}
		if got, _ := store.Site(ctx, ws, s.ID); got.CollectionID != uuid.Nil {
			t.Errorf("site moved to the top level has collection %v", got.CollectionID)
		}
		for _, id := range []uuid.UUID{inner.ID, empty.ID, nested.ID, top.ID} {
			if err := store.DeleteCollection(ctx, ws, id); err != nil {
				t.Errorf("DeleteCollection() of an empty collection = %v", err)
			}
		}
		if err := store.DeleteCollection(ctx, ws, empty.ID); !errors.Is(err, site.ErrCollectionNotFound) {
			t.Errorf("second delete = %v; want ErrCollectionNotFound", err)
		}
	})
}
