package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/adapters/store/postgres/pgtest"
	"github.com/fprambs/transferly/internal/core/mcpaccess"
	"github.com/fprambs/transferly/internal/core/workspace"
)

// TestMCPLinks verifies atomic approval and that ownership, client identity and revocation restrict reads.
func TestMCPLinks(t *testing.T) {
	ctx := t.Context()
	store, err := postgres.Open(ctx, pgtest.MigratedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	user, workspaceID, site, link := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	execute := func(query string, args ...any) {
		t.Helper()
		if _, err := store.DB().Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	execute(`INSERT INTO users (id, email, password_hash) VALUES ($1, 'owner@example.invalid', 'x')`, user)
	execute(`INSERT INTO workspaces (id, owner_id, kind, name) VALUES ($1, $2, 'personal', 'Personal')`, workspaceID, user)
	execute(`INSERT INTO sites (id, workspace_id, name, connector) VALUES ($1, $2, 'Existing', 'sftp')`, site, workspaceID)
	target := mcpaccess.Target{WorkspaceID: workspaceID, SiteID: site}
	if err := store.CreateMCPLink(ctx, link, user, "claude", nil); err != mcpaccess.ErrUnavailable {
		t.Fatalf("empty scope: %v", err)
	}
	if err := store.CreateMCPLink(ctx, link, user, "claude", []mcpaccess.Target{target, {WorkspaceID: uuid.New(), SiteID: site}}); err == nil {
		t.Fatal("invalid target did not reject the transaction")
	}
	if _, err := store.MCPLinkScope(ctx, link, user, "claude"); err != mcpaccess.ErrUnavailable {
		t.Fatalf("partial link survived rollback: %v", err)
	}
	if err := store.CreateMCPLink(ctx, link, user, "claude", []mcpaccess.Target{target}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateMCPLink(ctx, link, user, "claude", []mcpaccess.Target{target}); err == nil {
		t.Fatal("duplicate link was accepted")
	}
	scope, err := store.MCPLinkScope(ctx, link, user, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := scope.Authorize(ctx, target, func(context.Context, uuid.UUID, mcpaccess.Target, workspace.Action) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MCPLinkScope(ctx, link, user, "other"); err != mcpaccess.ErrUnavailable {
		t.Fatalf("wrong client: %v", err)
	}
	if err := store.RevokeMCPLink(ctx, link, uuid.New()); err != mcpaccess.ErrUnavailable {
		t.Fatalf("wrong owner: %v", err)
	}
	if err := store.RevokeMCPLink(ctx, link, user); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MCPLinkScope(ctx, link, user, "claude"); err != mcpaccess.ErrUnavailable {
		t.Fatalf("revoked link: %v", err)
	}
	store.Close()
	if err := store.CreateMCPLink(ctx, link, user, "claude", []mcpaccess.Target{target}); err == nil {
		t.Fatal("closed database accepted a link")
	}
	if _, err := store.MCPLinkScope(ctx, link, user, "claude"); err == nil {
		t.Fatal("closed database returned a scope")
	}
	if err := store.RevokeMCPLink(ctx, link, user); err == nil {
		t.Fatal("closed database accepted a revocation")
	}
}
