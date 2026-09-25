package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// errStep is the failure injected by the fake transaction.
var errStep = errors.New("step failed")

// fakeTx is a transaction that fails at a chosen step of first-user or workspace creation.
type fakeTx struct {
	pgx.Tx
	failAt string
	users  int64
}

// Exec fails for the lock or an insert step when selected.
func (f fakeTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if (f.failAt == "lock" && strings.Contains(sql, "pg_advisory")) || (f.failAt == "insert" && strings.Contains(sql, "INSERT INTO users")) ||
		(f.failAt == "workspace" && strings.Contains(sql, "INSERT INTO workspaces")) || (f.failAt == "membership" && strings.Contains(sql, "INSERT INTO memberships")) {
		return pgconn.CommandTag{}, errStep
	}
	return pgconn.CommandTag{}, nil
}

// QueryRow returns the user count, or a failing row when the count step is selected.
func (f fakeTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if f.failAt == "count" {
		return scanRow{err: errStep}
	}
	return scanRow{n: f.users}
}

// Commit fails when the commit step is selected.
func (f fakeTx) Commit(context.Context) error {
	if f.failAt == "commit" {
		return errStep
	}
	return nil
}

// Rollback does nothing.
func (fakeTx) Rollback(context.Context) error { return nil }

// scanRow is a query result holding one integer or an error.
type scanRow struct {
	n   int64
	err error
}

// Scan copies the integer into the destination, or returns the injected error.
func (r scanRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int64)) = r.n
	return nil
}

// TestCreateFirstUser_ReportsFailuresInsideTheTransaction verifies that a failure at any step of the
// transaction, including starting it, is returned to the caller.
//
// Scenarios:
//   - the transaction cannot start
//   - taking the creation lock, counting the users, inserting the user and committing each fail
//   - a store that already has users refuses with ErrSetupComplete without inserting
func TestCreateFirstUser_ReportsFailuresInsideTheTransaction(t *testing.T) {
	for _, step := range []string{"lock", "count", "insert", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
		if err := store.CreateFirstUser(context.Background(), auth.User{}); !errors.Is(err, errStep) {
			t.Errorf("failure at %s = %v, want the injected error", step, err)
		}
	}

	failing := &Store{queries: db.New(nil)}
	failing.begin = func(context.Context) (pgx.Tx, error) { return nil, errStep }
	if err := failing.CreateFirstUser(context.Background(), auth.User{}); !errors.Is(err, errStep) {
		t.Errorf("failure to start = %v, want the injected error", err)
	}

	populated := &Store{queries: db.New(nil)}
	populated.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{users: 3}, nil }
	if err := populated.CreateFirstUser(context.Background(), auth.User{}); !errors.Is(err, auth.ErrSetupComplete) {
		t.Errorf("populated store = %v, want ErrSetupComplete", err)
	}
}

// TestCreateWorkspace_ReportsFailuresInsideTheTransaction verifies that a failure at any step of workspace creation
// is returned to the caller.
//
// Scenarios:
//   - the transaction cannot start
//   - inserting the workspace, inserting the owner membership and committing each fail
func TestCreateWorkspace_ReportsFailuresInsideTheTransaction(t *testing.T) {
	for _, step := range []string{"workspace", "membership", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
		if err := store.CreateWorkspace(context.Background(), workspace.Workspace{}); !errors.Is(err, errStep) {
			t.Errorf("failure at %s = %v, want the injected error", step, err)
		}
	}

	failing := &Store{queries: db.New(nil)}
	failing.begin = func(context.Context) (pgx.Tx, error) { return nil, errStep }
	if err := failing.CreateWorkspace(context.Background(), workspace.Workspace{}); !errors.Is(err, errStep) {
		t.Errorf("failure to start = %v, want the injected error", err)
	}
}
