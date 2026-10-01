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

// colorsBeforeLabels is the last schema version whose sites have no label color.
const colorsBeforeLabels = 30

// TestMigration_SiteColor verifies that migration 31 gives every site an empty label, accepts the known colors only, and that its down
// step drops the column.
//
// Scenarios:
//   - a site saved before the migration has no label after it
//   - a known color is stored and an unknown one is refused by the database
//   - migrating down removes the column
func TestMigration_SiteColor(t *testing.T) {
	ctx := context.Background()
	url := testutil.NewDatabase(t)
	p, closeDB, err := newProvider(url, pgmigrations.FS)
	if err != nil {
		t.Fatalf("newProvider() = %v", err)
	}
	defer closeDB()
	if _, err := p.UpTo(ctx, colorsBeforeLabels); err != nil {
		t.Fatalf("UpTo(%d) = %v", colorsBeforeLabels, err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	seed := []string{
		`INSERT INTO users (id, email, password_hash) VALUES ('00000000-0000-0000-0000-000000000001', 'owner@example.com', 'x')`,
		`INSERT INTO workspaces (id, name, kind, owner_id) VALUES ('00000000-0000-0000-0000-000000000002', 'Personal', 'personal', '00000000-0000-0000-0000-000000000001')`,
		`INSERT INTO sites (id, workspace_id, name, protocol, host, port, username, auth_method)
		 VALUES ('00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000002', 'Prod', 'sftp', 'prod.example.com', 22, 'deploy', 'password')`,
	}
	for _, statement := range seed {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	if _, err := p.UpByOne(ctx); err != nil {
		t.Fatalf("UpByOne() = %v", err)
	}
	var color string
	if err := conn.QueryRow(ctx, `SELECT color FROM sites`).Scan(&color); err != nil || color != "" {
		t.Errorf("color after migrating up = %q, %v, want empty", color, err)
	}
	if _, err := conn.Exec(ctx, `UPDATE sites SET color = 'teal'`); err != nil {
		t.Errorf("storing teal: %v", err)
	}
	if _, err := conn.Exec(ctx, `UPDATE sites SET color = 'purple'`); err == nil {
		t.Error("storing purple = nil, want a check violation")
	}
	if _, err := p.Down(ctx); err != nil {
		t.Fatalf("Down() = %v", err)
	}
	var columns int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'sites' AND column_name = 'color'`).Scan(&columns); err != nil || columns != 0 {
		t.Errorf("color columns after migrating down = %d, %v, want 0", columns, err)
	}
}
