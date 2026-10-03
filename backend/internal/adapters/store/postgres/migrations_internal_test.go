package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/fprambs/transferly/internal/testutil"
	pgmigrations "github.com/fprambs/transferly/migrations/postgres"
)

// colorsBeforeLabels is the last schema version whose sites have no label color.
const colorsBeforeLabels = 29

// TestMigration_SiteColor verifies that migration 30 gives every site an empty label, accepts the known colors only, and that its down
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
