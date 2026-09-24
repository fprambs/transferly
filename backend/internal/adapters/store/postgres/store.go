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

	pgmigrations "github.com/fprambs/sentinel/migrations/postgres"
)

// Store wraps the PostgreSQL connection pool.
type Store struct {
	pool *pgxpool.Pool
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
	return &Store{pool: pool}, nil
}

// Ping implements [ports.ReadinessChecker].
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

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

// newProvider opens a database handle and builds a goose provider over fsys.
// The returned function closes the handle.
func newProvider(databaseURL string, fsys fs.FS) (*goose.Provider, func(), error) {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid database URL: %w", err)
	}
	db := (*sql.DB)(stdlib.OpenDB(*cfg))
	closeDB := func() { _ = db.Close() }

	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys)
	if err != nil {
		closeDB()
		return nil, nil, fmt.Errorf("creating migration provider: %w", err)
	}
	return p, closeDB, nil
}
