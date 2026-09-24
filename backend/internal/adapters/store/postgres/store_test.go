package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/testutil"
)

// unreachableURL points at a loopback port where nothing listens.
const unreachableURL = "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1"

// TestMigrate_FromScratch verifies that migrations apply on an empty database
// and leave a schema version of at least 1.
func TestMigrate_FromScratch(t *testing.T) {
	dbURL := testutil.NewDatabase(t)
	ctx := context.Background()

	if err := postgres.Migrate(ctx, dbURL); err != nil {
		t.Fatalf("Migrate() = %v", err)
	}
	v, err := postgres.MigrationStatus(ctx, dbURL)
	if err != nil {
		t.Fatalf("MigrationStatus() = %v", err)
	}
	if v < 1 {
		t.Errorf("schema version = %d, want >= 1", v)
	}
}

// TestMigrate_IsIdempotent verifies that running migrations repeatedly succeeds without changes.
func TestMigrate_IsIdempotent(t *testing.T) {
	dbURL := testutil.NewDatabase(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		if err := postgres.Migrate(ctx, dbURL); err != nil {
			t.Fatalf("Migrate() run %d = %v", i, err)
		}
	}
}

// TestMigrate_ConcurrentRunsAreSafe verifies that several instances migrating the same database at once all succeed.
func TestMigrate_ConcurrentRunsAreSafe(t *testing.T) {
	dbURL := testutil.NewDatabase(t)
	const runs = 6
	errs := make(chan error, runs)

	for range runs {
		go func() { errs <- postgres.Migrate(context.Background(), dbURL) }()
	}
	for range runs {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Migrate() = %v", err)
		}
	}
	if v, err := postgres.MigrationStatus(context.Background(), dbURL); err != nil || v < 1 {
		t.Errorf("MigrationStatus() = %d, %v, want version >= 1", v, err)
	}
}

// TestMigrate_InvalidURL verifies that a malformed database URL is rejected before connecting.
func TestMigrate_InvalidURL(t *testing.T) {
	if err := postgres.Migrate(context.Background(), "not a url"); err == nil {
		t.Error("Migrate() with invalid URL = nil, want error")
	}
}

// TestMigrationStatus verifies the schema version reporting.
//
// Scenarios:
//   - after migrating, the version is at least 1
//   - a malformed database URL is rejected
//   - an unreachable server is reported as an error
func TestMigrationStatus(t *testing.T) {
	ctx := context.Background()

	dbURL := testutil.NewDatabase(t)
	if err := postgres.Migrate(ctx, dbURL); err != nil {
		t.Fatalf("Migrate() = %v", err)
	}
	if v, err := postgres.MigrationStatus(ctx, dbURL); err != nil || v < 1 {
		t.Errorf("MigrationStatus() = %d, %v, want version >= 1", v, err)
	}
	if _, err := postgres.MigrationStatus(ctx, "not a url"); err == nil {
		t.Error("MigrationStatus() with malformed URL = nil error, want error")
	}
	if _, err := postgres.MigrationStatus(ctx, unreachableURL); err == nil {
		t.Error("MigrationStatus() with unreachable server = nil error, want error")
	}
}

// TestMigrate_UnreachableServer verifies that applying migrations fails when the server cannot be reached.
func TestMigrate_UnreachableServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := postgres.Migrate(ctx, unreachableURL); err == nil {
		t.Error("Migrate() with unreachable server = nil error, want error")
	}
}

// TestOpen_Ping verifies that a pool opened against a reachable database answers Ping.
func TestOpen_Ping(t *testing.T) {
	ctx := context.Background()
	s, err := postgres.Open(ctx, testutil.DatabaseURL(t))
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer s.Close()
	if err := s.Ping(ctx); err != nil {
		t.Errorf("Ping() = %v", err)
	}
}

// TestOpen_InvalidURL verifies that Open rejects a malformed database URL.
func TestOpen_InvalidURL(t *testing.T) {
	if _, err := postgres.Open(context.Background(), "not a url"); err == nil {
		t.Error("Open() with invalid URL = nil, want error")
	}
}

// TestOpen_UnreachableServer verifies that Open fails fast when nothing listens on the server address.
func TestOpen_UnreachableServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := postgres.Open(ctx, unreachableURL); err == nil {
		t.Error("Open() with unreachable server = nil, want error")
	}
}
