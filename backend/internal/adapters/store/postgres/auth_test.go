package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/adapters/store/storetest"
	"github.com/fprambs/sentinel/internal/core/auth"
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
