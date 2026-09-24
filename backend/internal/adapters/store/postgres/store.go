// Package postgres is the storage adapter backed by PostgreSQL (pgx and goose).
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	pgmigrations "github.com/fprambs/sentinel/migrations/postgres"
)

// Store wraps the PostgreSQL connection pool and its generated queries.
type Store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// Open creates the connection pool and verifies connectivity.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("creating postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}
	return &Store{pool: pool, queries: db.New(pool)}, nil
}

// Ping implements [ports.ReadinessChecker].
func (s *Store) Ping(ctx context.Context) error {
	_, err := s.queries.Ping(ctx)
	return err
}

// Close releases the connection pool.
func (s *Store) Close() { s.pool.Close() }

// Migrate applies all pending migrations. It is idempotent.
func Migrate(ctx context.Context, databaseURL string) error {
	p, closeDB, err := newProvider(databaseURL, pgmigrations.FS)
	if err != nil {
		return err
	}
	defer closeDB()
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}

// MigrationStatus returns the current schema version.
func MigrationStatus(ctx context.Context, databaseURL string) (int64, error) {
	p, closeDB, err := newProvider(databaseURL, pgmigrations.FS)
	if err != nil {
		return 0, err
	}
	defer closeDB()
	v, err := p.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("reading schema version: %w", err)
	}
	return v, nil
}

const (
	lockRetrySeconds = 1
	lockAttempts     = 120
)

// newLocker creates the advisory lock that serializes migrations across instances.
var newLocker = lock.NewPostgresSessionLocker

// newProvider opens a database handle and builds a goose provider over fsys.
// The provider holds a Postgres advisory lock while migrating, so instances started together
// apply migrations one at a time. The returned function closes the handle.
func newProvider(databaseURL string, fsys fs.FS) (*goose.Provider, func(), error) {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid database URL: %w", err)
	}
	db := (*sql.DB)(stdlib.OpenDB(*cfg))
	closeDB := func() { _ = db.Close() }

	locker, err := newLocker(lock.WithLockTimeout(lockRetrySeconds, lockAttempts))
	if err != nil {
		closeDB()
		return nil, nil, fmt.Errorf("creating migration lock: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithSessionLocker(locker))
	if err != nil {
		closeDB()
		return nil, nil, fmt.Errorf("creating migration provider: %w", err)
	}
	return p, closeDB, nil
}
