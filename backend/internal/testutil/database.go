// Package testutil provides helpers shared by integration tests.
package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const envDatabaseURL = "SENTINEL_TEST_DATABASE_URL"

// DatabaseURL returns the URL of the test database.
// When it is not configured, the test is skipped locally and fails when CI is set.
func DatabaseURL(t testing.TB) string {
	t.Helper()
	u := os.Getenv(envDatabaseURL)
	if u == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s is required in CI", envDatabaseURL)
		}
		t.Skipf("%s is not set; run the tests through compose.dev.yaml", envDatabaseURL)
	}
	return u
}

// NewDatabase creates an empty, uniquely named database and returns its URL.
// The database is dropped when the test ends.
func NewDatabase(t testing.TB) string {
	t.Helper()
	baseURL := DatabaseURL(t)
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		t.Fatalf("connecting to the test database: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close(ctx) })

	name := fmt.Sprintf("sentinel_t_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatalf("creating isolated database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)")
	})

	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parsing database URL: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}
