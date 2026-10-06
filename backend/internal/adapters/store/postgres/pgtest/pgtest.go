// Package pgtest gives integration tests databases with the schema of Transferly already applied: the migrations run once, into a
// template database named after them, and every test gets a copy of it, which takes a fraction of migrating a new database.
package pgtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/testutil"
	pgmigrations "github.com/fprambs/transferly/migrations/postgres"
)

// templatePrefix starts the names of the template databases, which end with a digest of the migrations they hold.
const templatePrefix = "transferly_tpl_"

// templateLock is the advisory lock that keeps the test processes of several packages from building the template at once.
const templateLock = 7_314_211

// The template of this process, built or found once.
var (
	once     sync.Once
	template string
	failure  error
)

// MigratedDatabase creates a database with every migration applied, as a copy of the template of the current migrations, and returns
// its URL. The database is dropped when the test ends.
func MigratedDatabase(t testing.TB) string {
	t.Helper()
	base := testutil.DatabaseURL(t)
	once.Do(func() { template, failure = ensureTemplate(base) })
	if failure != nil {
		t.Fatalf("preparing the migrated template database: %v", failure)
	}
	return testutil.NewDatabaseFrom(t, template)
}

// digest names the current migrations: a digest of the name and content of every file.
func digest() (string, error) {
	h := sha256.New()
	err := fs.WalkDir(pgmigrations.FS, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := fs.ReadFile(pgmigrations.FS, path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", path, len(content))
		_, _ = h.Write(content)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:16], err
}

// ensureTemplate returns the template database of the current migrations, migrating it under the advisory lock when no process has
// yet, and drops the templates of earlier migrations.
func ensureTemplate(base string) (string, error) {
	sum, err := digest()
	if err != nil {
		return "", err
	}
	name := templatePrefix + sum
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return "", err
	}
	defer func() { _ = admin.Close(context.Background()) }()
	if _, err := admin.Exec(ctx, "SELECT pg_advisory_lock($1)", templateLock); err != nil {
		return "", err
	}
	defer func() { _, _ = admin.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", templateLock) }()
	var exists bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return name, nil
	}
	build := name + "_build"
	quotedBuild := pgx.Identifier{build}.Sanitize()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quotedBuild+" WITH (FORCE)"); err != nil {
		return "", err
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quotedBuild); err != nil {
		return "", err
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = "/" + build
	if err := postgres.Migrate(ctx, u.String()); err != nil {
		return "", err
	}
	if _, err := admin.Exec(ctx, "ALTER DATABASE "+quotedBuild+" RENAME TO "+pgx.Identifier{name}.Sanitize()); err != nil {
		return "", err
	}
	rows, err := admin.Query(ctx, "SELECT datname FROM pg_database WHERE datname LIKE $1 AND datname <> $2", templatePrefix+"%", name)
	if err != nil {
		return "", err
	}
	stale, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", err
	}
	for _, old := range stale {
		if !strings.HasSuffix(old, "_build") {
			_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{old}.Sanitize()+" WITH (FORCE)")
		}
	}
	return name, nil
}
