package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/adapters/store/storetest"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/hostkey"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/testutil"
)

// openMigrated returns a store over a fresh, migrated database.
func openMigrated(t *testing.T) *postgres.Store {
	t.Helper()
	dbURL := testutil.NewDatabase(t)
	if err := postgres.Migrate(t.Context(), dbURL); err != nil {
		t.Fatalf("Migrate() = %v", err)
	}
	store, err := postgres.Open(t.Context(), dbURL)
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

// TestAuthStore_Conformance runs the shared authentication store suite against Postgres.
func TestAuthStore_Conformance(t *testing.T) {
	storetest.RunAuth(t, func(t *testing.T) storetest.AuthStore { return openMigrated(t) })
}

// TestAuthStore_ReportsDatabaseFailures verifies that every operation returns the database error
// instead of hiding it, using a store whose connection pool is closed.
func TestAuthStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id, at := uuid.New(), time.Now()
	user := auth.User{ID: id, Email: "x@example.com", Role: auth.RoleUser, CreatedAt: at}
	session := auth.Session{ID: id, UserID: id, TokenHash: []byte("h"), CreatedAt: at, LastSeenAt: at, ExpiresAt: at}

	calls := map[string]error{
		"CreateFirstUser":       store.CreateFirstUser(ctx, user),
		"CreateUser":            store.CreateUser(ctx, user),
		"UpdatePasswordHash":    store.UpdatePasswordHash(ctx, id, "h"),
		"UpdatePreferences":     store.UpdatePreferences(ctx, id, "en", "UTC"),
		"CreateSession":         store.CreateSession(ctx, session),
		"TouchSession":          store.TouchSession(ctx, id, at),
		"DeleteSession":         store.DeleteSession(ctx, id),
		"UserByEmail":           second(store.UserByEmail(ctx, "x@example.com")),
		"UserByID":              second(store.UserByID(ctx, id)),
		"CountUsers":            second(store.CountUsers(ctx)),
		"SessionByTokenHash":    second(store.SessionByTokenHash(ctx, []byte("h"))),
		"DeleteUserSessions":    second(store.DeleteUserSessions(ctx, id)),
		"DeleteExpiredSessions": second(store.DeleteExpiredSessions(ctx, at, at)),
	}
	for name, err := range calls {
		if err == nil {
			t.Errorf("%s on a closed pool = nil error, want the database error", name)
		}
	}
}

// TestCreateFirstUser_WaitsForTheCreationLock verifies deterministically that first-user creation is serialized
// through the advisory lock: while another transaction holds the lock the call blocks, and once the lock is
// released it completes. A statistical race test cannot detect a missing lock because the window is tiny.
func TestCreateFirstUser_WaitsForTheCreationLock(t *testing.T) {
	dbURL := testutil.NewDatabase(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, dbURL); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	holder, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close(ctx) }()
	tx, err := holder.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(4097083627)"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- store.CreateFirstUser(ctx, auth.User{ID: uuid.New(), Email: "lock@example.com", Role: auth.RoleAdmin, CreatedAt: time.Now()})
	}()
	select {
	case err := <-done:
		t.Fatalf("CreateFirstUser() returned %v while the lock was held, want it to wait", err)
	case <-time.After(300 * time.Millisecond):
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("CreateFirstUser() after the lock was released = %v, want success", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CreateFirstUser() did not finish after the lock was released")
	}
}

// second returns the error of a call that also returns a value.
func second[T any](_ T, err error) error { return err }

// TestWorkspaceStore_Conformance runs the shared workspace store suite against Postgres.
func TestWorkspaceStore_Conformance(t *testing.T) {
	storetest.RunWorkspace(t, func(t *testing.T) storetest.WorkspaceStore { return openMigrated(t) })
}

// TestWorkspaceStore_ReportsDatabaseFailures verifies that every workspace operation returns the database
// error instead of hiding it, using a store whose connection pool is closed.
func TestWorkspaceStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]error{
		"CreateWorkspace":   store.CreateWorkspace(ctx, workspace.Workspace{ID: id, OwnerID: id}),
		"PersonalWorkspace": second(store.PersonalWorkspace(ctx, id)),
		"WorkspacesForUser": second(store.WorkspacesForUser(ctx, id)),
		"RoleOf":            second(store.RoleOf(ctx, id, id)),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, workspace.ErrNotFound) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestVaultStore_Conformance runs the shared vault store suite against Postgres.
func TestVaultStore_Conformance(t *testing.T) {
	storetest.RunVault(t, func(t *testing.T) storetest.VaultStore { return openMigrated(t) })
}

// TestVaultStore_ReportsDatabaseFailures verifies that every vault operation returns the database error
// instead of hiding it, using a store whose connection pool is closed.
func TestVaultStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]error{
		"CreateVault": store.CreateVault(ctx, id, vault.Enrollment{}),
		"Vault":       second(store.Vault(ctx, id)),
		"UpdateVault": store.UpdateVault(ctx, id, vault.Enrollment{}),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, vault.ErrNoVault) || errors.Is(err, vault.ErrVaultExists) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestSiteStore_Conformance runs the shared site store suite against Postgres.
func TestSiteStore_Conformance(t *testing.T) {
	storetest.RunSite(t, func(t *testing.T) storetest.SiteStore { return openMigrated(t) })
}

// TestSiteStore_ReportsDatabaseFailures verifies that every site operation returns the database error
// instead of hiding it, using a store whose connection pool is closed.
func TestSiteStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()
	s := site.Site{ID: id, WorkspaceID: id}

	calls := map[string]error{
		"CreateSite": store.CreateSite(ctx, s, nil),
		"UpdateSite": store.UpdateSite(ctx, s, nil, nil),
		"Site":       second(store.Site(ctx, id, id)),
		"Sites":      second(store.Sites(ctx, id)),
		"DeleteSite": store.DeleteSite(ctx, id, id),
		"Secret":     second(store.Secret(ctx, id, id, site.SecretPassword)),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, site.ErrNotFound) || errors.Is(err, site.ErrNoSecret) || errors.Is(err, site.ErrNameTaken) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestHostKeyStore_Conformance runs the shared host key store suite against Postgres.
func TestHostKeyStore_Conformance(t *testing.T) {
	storetest.RunHostKey(t, func(t *testing.T) storetest.HostKeyStore { return openMigrated(t) })
}

// TestHostKeyStore_ReportsDatabaseFailures verifies that every host key operation returns the database error
// instead of hiding it, using a store whose connection pool is closed.
func TestHostKeyStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]error{
		"Trusted": second(store.Trusted(ctx, id, "h", 22)),
		"List":    second(store.List(ctx, id)),
		"Add":     store.Add(ctx, id, hostkey.Key{}),
		"Delete":  store.Delete(ctx, id, "h", 22, "t"),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, hostkey.ErrNotFound) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}
