package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fprambs/transferly/internal/adapters/store/postgres/db"
	"github.com/fprambs/transferly/internal/core/site"
)

var _ site.Store = (*Store)(nil)

// siteWrite maps a unique violation to [site.ErrNameTaken] and leaves other errors untouched.
func siteWrite(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return site.ErrNameTaken
	}
	return err
}

// writeSecrets stores and removes the secrets of a site inside a transaction.
func writeSecrets(ctx context.Context, q *db.Queries, siteID uuid.UUID, set []site.Sealed, remove []site.SecretKind) error {
	for _, s := range set {
		err := q.UpsertSecret(ctx, db.UpsertSecretParams{SiteID: siteID, Kind: string(s.Kind), DekVersion: int32(s.DEKVersion), Sealed: s.Data})
		if err != nil {
			return err
		}
	}
	for _, kind := range remove {
		if err := q.DeleteSecret(ctx, db.DeleteSecretParams{SiteID: siteID, Kind: string(kind)}); err != nil {
			return err
		}
	}
	return nil
}

// CreateSite implements [site.Store].
func (s *Store) CreateSite(ctx context.Context, st site.Site, secrets []site.Sealed) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	err = q.InsertSite(ctx, db.InsertSiteParams{
		ID: st.ID, WorkspaceID: st.WorkspaceID, Name: st.Name, Connector: st.Connector, Config: configJSON(st.Config), CreatedAt: st.CreatedAt,
		FolderID: folderID(st.CollectionID), SpeedLimit: st.SpeedLimit, RemoteDir: st.RemoteDir, Color: string(st.Color),
	})
	if err != nil {
		return siteWrite(err)
	}
	if err := writeSecrets(ctx, q, st.ID, secrets, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpdateSite implements [site.Store].
func (s *Store) UpdateSite(ctx context.Context, st site.Site, secrets []site.Sealed, remove []site.SecretKind) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	rows, err := q.UpdateSite(ctx, db.UpdateSiteParams{
		ID: st.ID, WorkspaceID: st.WorkspaceID, Name: st.Name, Connector: st.Connector, Config: configJSON(st.Config), UpdatedAt: st.UpdatedAt,
		FolderID: folderID(st.CollectionID), SpeedLimit: st.SpeedLimit, RemoteDir: st.RemoteDir, Color: string(st.Color),
	})
	if err != nil {
		return siteWrite(err)
	}
	if rows == 0 {
		return site.ErrNotFound
	}
	if err := writeSecrets(ctx, q, st.ID, secrets, remove); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// folderID converts a collection identifier to the nullable column, the nil identifier being the top level.
func folderID(id uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: id, Valid: id != uuid.Nil}
}

// configJSON encodes the configuration of a site for its jsonb column; a map of strings always encodes.
func configJSON(config map[string]string) []byte {
	if config == nil {
		config = map[string]string{}
	}
	data, _ := json.Marshal(config)
	return data
}

// siteFrom converts a stored row and its secret kinds to a site. The configuration column only ever holds what [configJSON] wrote.
func siteFrom(r db.SiteByIDRow, secrets []site.SecretKind) site.Site {
	config := map[string]string{}
	_ = json.Unmarshal(r.Config, &config)
	return site.Site{
		ID: r.ID, WorkspaceID: r.WorkspaceID, CollectionID: r.FolderID.UUID, Name: r.Name, Connector: r.Connector, Config: config,
		SpeedLimit: r.SpeedLimit, RemoteDir: r.RemoteDir, Color: site.Color(r.Color), Secrets: secrets,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

// secretKinds returns the secret kinds of every site of a workspace, by site.
func (s *Store) secretKinds(ctx context.Context, workspaceID uuid.UUID) (map[uuid.UUID][]site.SecretKind, error) {
	rows, err := s.queries.SecretKindsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]site.SecretKind{}
	for _, r := range rows {
		out[r.SiteID] = append(out[r.SiteID], site.SecretKind(r.Kind))
	}
	return out, nil
}

// Site implements [site.Store].
func (s *Store) Site(ctx context.Context, workspaceID, id uuid.UUID) (site.Site, error) {
	r, err := s.queries.SiteByID(ctx, db.SiteByIDParams{ID: id, WorkspaceID: workspaceID})
	if errors.Is(err, errNoRows) {
		return site.Site{}, site.ErrNotFound
	}
	if err != nil {
		return site.Site{}, err
	}
	kinds, err := s.secretKinds(ctx, workspaceID)
	if err != nil {
		return site.Site{}, err
	}
	return siteFrom(r, kinds[id]), nil
}

// Sites implements [site.Store].
func (s *Store) Sites(ctx context.Context, workspaceID uuid.UUID) ([]site.Site, error) {
	rows, err := s.queries.SitesByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	kinds, err := s.secretKinds(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]site.Site, 0, len(rows))
	for _, r := range rows {
		out = append(out, siteFrom(db.SiteByIDRow(r), kinds[r.ID]))
	}
	return out, nil
}

// DeleteSite implements [site.Store].
func (s *Store) DeleteSite(ctx context.Context, workspaceID, id uuid.UUID) error {
	rows, err := s.queries.DeleteSite(ctx, db.DeleteSiteParams{ID: id, WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	if rows == 0 {
		return site.ErrNotFound
	}
	return nil
}

// Secret implements [site.Store].
func (s *Store) Secret(ctx context.Context, workspaceID, siteID uuid.UUID, kind site.SecretKind) (site.Sealed, error) {
	r, err := s.queries.SecretBySiteKind(ctx, db.SecretBySiteKindParams{SiteID: siteID, WorkspaceID: workspaceID, Kind: string(kind)})
	if errors.Is(err, errNoRows) {
		return site.Sealed{}, site.ErrNoSecret
	}
	if err != nil {
		return site.Sealed{}, err
	}
	return site.Sealed{Kind: kind, DEKVersion: int(r.DekVersion), Data: r.Sealed}, nil
}
