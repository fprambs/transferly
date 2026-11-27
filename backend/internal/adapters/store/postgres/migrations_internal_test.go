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

// sessionsBeforeCLI is the last schema version whose sessions are all of the browser.
const sessionsBeforeCLI = 37

// TestMigration_CLISessions verifies that migration 38 makes every existing session a browser session, adds the approvals of the
// terminal client, and that its down step ends terminal sessions before it drops their kind (ADR-0029).
//
// Scenarios:
//   - a session saved before the migration is a browser session with no name or host after it
//   - a terminal session and an approval are stored; an unknown kind is refused by the database
//   - migrating down deletes the terminal session, keeps the browser one, and drops the columns and the table
func TestMigration_CLISessions(t *testing.T) {
	ctx := context.Background()
	url := testutil.NewDatabase(t)
	p, closeDB, err := newProvider(url, pgmigrations.FS)
	if err != nil {
		t.Fatalf("newProvider() = %v", err)
	}
	defer closeDB()
	if _, err := p.UpTo(ctx, sessionsBeforeCLI); err != nil {
		t.Fatalf("UpTo(%d) = %v", sessionsBeforeCLI, err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	seed := []string{
		`INSERT INTO users (id, email, password_hash) VALUES ('00000000-0000-0000-0000-000000000001', 'owner@example.com', 'x')`,
		`INSERT INTO user_sessions (id, user_id, token_hash, expires_at)
		 VALUES ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', 'browser', now() + interval '1 hour')`,
	}
	for _, statement := range seed {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	if _, err := p.UpByOne(ctx); err != nil {
		t.Fatalf("UpByOne() = %v", err)
	}
	var kind, name, host string
	if err := conn.QueryRow(ctx, `SELECT kind, name, host FROM user_sessions`).Scan(&kind, &name, &host); err != nil || kind != "browser" || name != "" || host != "" {
		t.Errorf("existing session after migrating up = %q %q %q, %v; want a browser session with no name or host", kind, name, host, err)
	}
	for _, statement := range []string{
		`INSERT INTO user_sessions (id, user_id, token_hash, expires_at, kind, name, host)
		 VALUES ('00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', 'cli', now() + interval '30 days', 'cli', 'transferly-tui', 'laptop')`,
		`INSERT INTO cli_authorizations (id, user_id, code_hash, code_challenge, client_name, client_host, expires_at)
		 VALUES ('00000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001', 'code', 'challenge', 'transferly-tui', 'laptop', now())`,
	} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Errorf("storing after migrating up: %v", err)
		}
	}
	if _, err := conn.Exec(ctx, `UPDATE user_sessions SET kind = 'robot'`); err == nil {
		t.Error("storing the kind robot = nil, want a check violation")
	}
	if _, err := p.Down(ctx); err != nil {
		t.Fatalf("Down() = %v", err)
	}
	var sessions int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE token_hash = 'browser'`).Scan(&sessions); err != nil || sessions != 1 {
		t.Errorf("browser sessions after migrating down = %d, %v, want 1", sessions, err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM user_sessions`).Scan(&sessions); err != nil || sessions != 1 {
		t.Errorf("sessions after migrating down = %d, %v, want only the browser one", sessions, err)
	}
	var leftovers int
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM information_schema.columns WHERE table_name = 'user_sessions' AND column_name IN ('kind', 'name', 'host'))
		+ (SELECT count(*) FROM information_schema.tables WHERE table_name = 'cli_authorizations')`).Scan(&leftovers); err != nil || leftovers != 0 {
		t.Errorf("columns and tables left after migrating down = %d, %v, want 0", leftovers, err)
	}
}

// TestMigration_MCPLinks preserves existing sites and enforces the approved workspace and site pair.
func TestMigration_MCPLinks(t *testing.T) {
	ctx := t.Context()
	url := testutil.NewDatabase(t)
	provider, closeDatabase, err := newProvider(url, pgmigrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDatabase()
	if _, err := provider.UpTo(ctx, 39); err != nil {
		t.Fatal(err)
	}
	connection, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close(ctx) }()

	const user = "00000000-0000-0000-0000-000000000001"
	const workspace = "00000000-0000-0000-0000-000000000002"
	const otherWorkspace = "00000000-0000-0000-0000-000000000003"
	const site = "00000000-0000-0000-0000-000000000004"
	const link = "00000000-0000-0000-0000-000000000005"
	execute := func(query string, args ...any) {
		t.Helper()
		if _, err := connection.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	execute(`INSERT INTO users (id, email, password_hash) VALUES ($1, 'owner@example.invalid', 'x')`, user)
	execute(`INSERT INTO workspaces (id, owner_id, kind, name) VALUES ($1, $3, 'personal', 'Personal'), ($2, $3, 'team', 'Team')`, workspace, otherWorkspace, user)
	execute(`INSERT INTO sites (id, workspace_id, name, connector) VALUES ($1, $2, 'Existing', 'sftp')`, site, workspace)
	if _, err := provider.UpByOne(ctx); err != nil {
		t.Fatal(err)
	}
	execute(`INSERT INTO mcp_links (id, user_id, client_id) VALUES ($1, $2, 'claude')`, link, user)
	const approve = `INSERT INTO mcp_link_sites (link_id, workspace_id, site_id) VALUES ($1, $2, $3)`
	if _, err := connection.Exec(ctx, approve, link, otherWorkspace, site); err == nil {
		t.Fatal("a site was approved under the wrong workspace")
	}
	execute(approve, link, workspace, site)
	execute(`DELETE FROM mcp_links WHERE id = $1`, link)
	var sites int
	if err := connection.QueryRow(ctx, `SELECT count(*) FROM mcp_link_sites`).Scan(&sites); err != nil || sites != 0 {
		t.Fatalf("approved sites after link removal: %d, %v", sites, err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if err := connection.QueryRow(ctx, `SELECT count(*) FROM sites WHERE id = $1`, site).Scan(&sites); err != nil || sites != 1 {
		t.Fatalf("existing sites after rollback: %d, %v", sites, err)
	}
}
