package postgres

import (
	"context"
	"maps"
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

// configBeforeSchemas is the last schema version whose sites keep their settings in columns of their own.
const configBeforeSchemas = 31

// TestMigration_SiteConfig verifies that migration 32 moves the settings of every site into the configuration of its connector (ADR-0022),
// and that its down step brings the columns back.
//
// Scenarios:
//   - an SFTP site with a proxy and a jump host keeps every setting, and gets no FTP setting
//   - an FTP site keeps its encryption, time zone and character set, and gets no jump host
//   - secrets keep their kinds, and new kinds are accepted when they are names
//   - migrating down restores the columns from the configuration and drops what the old schema cannot hold
func TestMigration_SiteConfig(t *testing.T) {
	ctx := context.Background()
	url := testutil.NewDatabase(t)
	p, closeDB, err := newProvider(url, pgmigrations.FS)
	if err != nil {
		t.Fatalf("newProvider() = %v", err)
	}
	defer closeDB()
	if _, err := p.UpTo(ctx, configBeforeSchemas); err != nil {
		t.Fatalf("UpTo(%d) = %v", configBeforeSchemas, err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	seed := []string{
		`INSERT INTO users (id, email, password_hash) VALUES ('00000000-0000-0000-0000-000000000001', 'owner@example.com', 'x')`,
		`INSERT INTO workspaces (id, name, kind, owner_id) VALUES ('00000000-0000-0000-0000-000000000002', 'Personal', 'personal', '00000000-0000-0000-0000-000000000001')`,
		`INSERT INTO sites (id, workspace_id, name, protocol, host, port, username, auth_method, proxy_kind, proxy_host, proxy_port, proxy_username,
		   jump_host, jump_port, jump_username, jump_auth_method)
		 VALUES ('00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000002', 'Secure', 'sftp', 'prod.example.com', 2222, 'deploy', 'key',
		   'socks5', 'proxy.example.com', 1080, 'pu', 'bastion.example.com', 22, 'jump', 'password')`,
		`INSERT INTO sites (id, workspace_id, name, protocol, host, port, username, auth_method, ftp_encryption, list_timezone, charset)
		 VALUES ('00000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000002', 'Legacy', 'ftp', 'ftp.example.com', 990, 'anonymous', 'anonymous',
		   'implicit', 'America/Santiago', 'koi8-r')`,
		`INSERT INTO site_secrets (site_id, kind, dek_version, sealed) VALUES ('00000000-0000-0000-0000-000000000003', 'private_key', 1, '\x00')`,
	}
	for _, statement := range seed {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	if _, err := p.UpByOne(ctx); err != nil {
		t.Fatalf("UpByOne() = %v", err)
	}
	read := func(name string) (string, map[string]string) {
		t.Helper()
		var connector string
		config := map[string]string{}
		if err := conn.QueryRow(ctx, `SELECT connector, config FROM sites WHERE name = $1`, name).Scan(&connector, &config); err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		return connector, config
	}
	connector, config := read("Secure")
	wantSFTP := map[string]string{
		"host": "prod.example.com", "port": "2222", "username": "deploy", "auth": "key",
		"proxy_kind": "socks5", "proxy_host": "proxy.example.com", "proxy_port": "1080", "proxy_username": "pu",
		"jump_host": "bastion.example.com", "jump_port": "22", "jump_username": "jump", "jump_auth": "password",
	}
	if connector != "sftp" || !maps.Equal(config, wantSFTP) {
		t.Errorf("SFTP site = %s %v; want %v", connector, config, wantSFTP)
	}
	connector, config = read("Legacy")
	wantFTP := map[string]string{
		"host": "ftp.example.com", "port": "990", "username": "anonymous", "auth": "anonymous", "encryption": "implicit",
		"list_timezone": "America/Santiago", "charset": "koi8-r",
	}
	if connector != "ftp" || !maps.Equal(config, wantFTP) {
		t.Errorf("FTP site = %s %v; want %v", connector, config, wantFTP)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO site_secrets (site_id, kind, dek_version, sealed) VALUES ('00000000-0000-0000-0000-000000000003', 'secret_access_key', 1, '\x00')`); err != nil {
		t.Errorf("storing a new kind of secret: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO site_secrets (site_id, kind, dek_version, sealed) VALUES ('00000000-0000-0000-0000-000000000003', 'Not A Name', 1, '\x00')`); err == nil {
		t.Error("storing a kind that is not a name = nil, want a check violation")
	}

	if _, err := p.Down(ctx); err != nil {
		t.Fatalf("Down() = %v", err)
	}
	var host, encryption, jumpHost string
	var port int
	if err := conn.QueryRow(ctx, `SELECT host, port, ftp_encryption, jump_host FROM sites WHERE name = 'Legacy'`).Scan(&host, &port, &encryption, &jumpHost); err != nil ||
		host != "ftp.example.com" || port != 990 || encryption != "implicit" || jumpHost != "" {
		t.Errorf("FTP site after migrating down = %s %d %s %q, %v", host, port, encryption, jumpHost, err)
	}
	var kinds int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM site_secrets`).Scan(&kinds); err != nil || kinds != 1 {
		t.Errorf("secrets after migrating down = %d, %v; want the private key only", kinds, err)
	}
}
