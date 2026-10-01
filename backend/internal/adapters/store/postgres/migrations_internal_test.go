package postgres

import (
	"context"
	"maps"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/fprambs/transferly/internal/testutil"
	pgmigrations "github.com/fprambs/transferly/migrations/postgres"
)

// themesBeforeRename is the last schema version whose themes carry the former product name.
const themesBeforeRename = 29

// TestMigration_TransferlyThemes verifies that migration 30 renames the themes of the former product name, on an empty
// database and on one with data, and that its down step restores them.
//
// Scenarios:
//   - an empty database gets the new default theme
//   - the dark and light themes of the former name become transferly-dark and transferly-light
//   - other themes are kept
//   - a user created after the migration gets transferly-dark
//   - migrating down restores the former identifiers and default
func TestMigration_TransferlyThemes(t *testing.T) {
	ctx := context.Background()
	url := testutil.NewDatabase(t)
	p, closeDB, err := newProvider(url, pgmigrations.FS)
	if err != nil {
		t.Fatalf("newProvider() = %v", err)
	}
	defer closeDB()
	if _, err := p.UpTo(ctx, themesBeforeRename); err != nil {
		t.Fatalf("UpTo(%d) = %v", themesBeforeRename, err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	insert := `INSERT INTO users (id, email, password_hash, theme) VALUES (gen_random_uuid(), $1, 'x', $2)`
	for email, theme := range map[string]string{"dark@example.com": "sentinel-dark", "light@example.com": "sentinel-light", "nord@example.com": "nord"} {
		if _, err := conn.Exec(ctx, insert, email, theme); err != nil {
			t.Fatalf("inserting %s: %v", email, err)
		}
	}
	if _, err := p.UpByOne(ctx); err != nil {
		t.Fatalf("UpByOne() = %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO users (id, email, password_hash) VALUES (gen_random_uuid(), 'new@example.com', 'x')`); err != nil {
		t.Fatalf("inserting new user: %v", err)
	}
	want := map[string]string{"dark@example.com": "transferly-dark", "light@example.com": "transferly-light", "nord@example.com": "nord", "new@example.com": "transferly-dark"}
	if got := themes(ctx, t, conn); !maps.Equal(got, want) {
		t.Errorf("themes after migrating up = %v, want %v", got, want)
	}
	if _, err := p.Down(ctx); err != nil {
		t.Fatalf("Down() = %v", err)
	}
	want = map[string]string{"dark@example.com": "sentinel-dark", "light@example.com": "sentinel-light", "nord@example.com": "nord", "new@example.com": "sentinel-dark"}
	if got := themes(ctx, t, conn); !maps.Equal(got, want) {
		t.Errorf("themes after migrating down = %v, want %v", got, want)
	}
	var fallback string
	if err := conn.QueryRow(ctx, `SELECT column_default FROM information_schema.columns WHERE table_name = 'users' AND column_name = 'theme'`).Scan(&fallback); err != nil {
		t.Fatalf("reading the default theme: %v", err)
	}
	if fallback != "'sentinel-dark'::text" {
		t.Errorf("default theme after migrating down = %s, want 'sentinel-dark'::text", fallback)
	}
}

// themes returns the theme of every user, by email.
func themes(ctx context.Context, t *testing.T, conn *pgx.Conn) map[string]string {
	t.Helper()
	rows, err := conn.Query(ctx, `SELECT email, theme FROM users`)
	if err != nil {
		t.Fatalf("listing themes: %v", err)
	}
	got := map[string]string{}
	for rows.Next() {
		var email, theme string
		if err := rows.Scan(&email, &theme); err != nil {
			t.Fatalf("reading a theme: %v", err)
		}
		got[email] = theme
	}
	return got
}
