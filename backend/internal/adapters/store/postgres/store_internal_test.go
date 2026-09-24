package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3/lock"

	"github.com/fprambs/sentinel/internal/testutil"
	pgmigrations "github.com/fprambs/sentinel/migrations/postgres"
)

// TestNewProvider_ReportsLockFailures verifies that a failure to create the migration lock is reported.
func TestNewProvider_ReportsLockFailures(t *testing.T) {
	original := newLocker
	newLocker = func(...lock.SessionLockerOption) (lock.SessionLocker, error) {
		return nil, errors.New("lock unavailable")
	}
	t.Cleanup(func() { newLocker = original })

	_, _, err := newProvider("postgres://u:p@127.0.0.1:1/db", fstest.MapFS{})

	if err == nil || !strings.Contains(err.Error(), "migration lock") {
		t.Errorf("newProvider() = %v, want a migration lock error", err)
	}
}

// TestNewProvider_RejectsBadInputs verifies the errors of newProvider.
//
// Scenarios:
//   - a malformed database URL
//   - a migrations file system without any migration
func TestNewProvider_RejectsBadInputs(t *testing.T) {
	tests := []struct {
		name string
		url  string
		fsys fstest.MapFS
	}{
		{"malformed database url", "not a url", fstest.MapFS{"1_x.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")}}},
		{"no migrations", "postgres://u:p@127.0.0.1:1/db", fstest.MapFS{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := newProvider(tt.url, tt.fsys); err == nil {
				t.Error("newProvider() = nil error, want error")
			}
		})
	}
}

// coreTables are the tables the core schema migration creates.
var coreTables = []string{
	"users", "user_vaults", "user_sessions", "workspaces", "memberships",
	"site_folders", "sites", "site_secrets", "known_hosts", "audit_events",
}

// tableCount returns how many of the core tables exist in the database.
func tableCount(t *testing.T, databaseURL string) int {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var count int
	err = conn.QueryRow(ctx,
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = ANY($1)",
		coreTables).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// TestCoreSchema_UpDownUp verifies that the core schema migration applies, rolls back completely and applies again.
//
// Scenarios:
//   - after Up every core table exists
//   - after rolling back to version 1 none of them exists
//   - applying the migrations again restores them
func TestCoreSchema_UpDownUp(t *testing.T) {
	dbURL := testutil.NewDatabase(t)
	ctx := context.Background()
	provider, closeDB, err := newProvider(dbURL, pgmigrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("Up() = %v", err)
	}
	if got := tableCount(t, dbURL); got != len(coreTables) {
		t.Errorf("tables after Up = %d, want %d", got, len(coreTables))
	}
	if _, err := provider.DownTo(ctx, 1); err != nil {
		t.Fatalf("DownTo(1) = %v", err)
	}
	if got := tableCount(t, dbURL); got != 0 {
		t.Errorf("tables after rollback = %d, want 0", got)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("second Up() = %v", err)
	}
	if got := tableCount(t, dbURL); got != len(coreTables) {
		t.Errorf("tables after the second Up = %d, want %d", got, len(coreTables))
	}
}
