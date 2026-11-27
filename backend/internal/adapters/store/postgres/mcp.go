package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres/db"
	"github.com/fprambs/transferly/internal/core/mcpaccess"
)

// CreateMCPLink stores an approved link and all its targets in one transaction.
func (s *Store) CreateMCPLink(ctx context.Context, id, user uuid.UUID, client string, targets []mcpaccess.Target) error {
	if len(targets) == 0 {
		return mcpaccess.ErrUnavailable
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	if err := queries.InsertMCPLink(ctx, db.InsertMCPLinkParams{ID: id, UserID: user, ClientID: client}); err != nil {
		return err
	}
	for _, target := range targets {
		if err := queries.InsertMCPLinkSite(ctx, db.InsertMCPLinkSiteParams{
			LinkID: id, WorkspaceID: target.WorkspaceID, SiteID: target.SiteID,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// MCPLinkScope returns the approved snapshot only for an active link belonging to the given user and client.
func (s *Store) MCPLinkScope(ctx context.Context, id, user uuid.UUID, client string) (mcpaccess.Scope, error) {
	rows, err := s.queries.ActiveMCPLinkSites(ctx, db.ActiveMCPLinkSitesParams{ID: id, UserID: user, ClientID: client})
	if err != nil {
		return mcpaccess.Scope{}, err
	}
	if len(rows) == 0 {
		return mcpaccess.Scope{}, mcpaccess.ErrUnavailable
	}
	targets := make([]mcpaccess.Target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, mcpaccess.Target{WorkspaceID: row.WorkspaceID, SiteID: row.SiteID})
	}
	return mcpaccess.NewScope(user, targets), nil
}

// RevokeMCPLink disables an active link owned by the given user.
func (s *Store) RevokeMCPLink(ctx context.Context, id, user uuid.UUID) error {
	updated, err := s.queries.RevokeMCPLink(ctx, db.RevokeMCPLinkParams{ID: id, UserID: user})
	if err != nil {
		return err
	}
	if updated == 0 {
		return mcpaccess.ErrUnavailable
	}
	return nil
}
