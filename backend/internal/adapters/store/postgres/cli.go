package postgres

import (
	"context"
	"time"

	"github.com/fprambs/transferly/internal/adapters/store/postgres/db"
	"github.com/fprambs/transferly/internal/core/auth"
)

// CreateCLIAuthorization implements [auth.CLIAuthorizationStore].
func (s *Store) CreateCLIAuthorization(ctx context.Context, a auth.CLIAuthorization) error {
	return s.queries.InsertCLIAuthorization(ctx, db.InsertCLIAuthorizationParams{
		ID: a.ID, UserID: a.UserID, CodeHash: a.CodeHash, CodeChallenge: a.CodeChallenge, RedirectUri: a.RedirectURI,
		ClientName: a.ClientName, ClientHost: a.ClientHost, Ip: a.IP, CreatedAt: a.CreatedAt, ExpiresAt: a.ExpiresAt,
	})
}

// UseCLIAuthorization implements [auth.CLIAuthorizationStore]. One UPDATE marks the approval as used only while it is unused, so of
// several concurrent exchanges of one code exactly one gets a row.
func (s *Store) UseCLIAuthorization(ctx context.Context, codeHash []byte, at time.Time) (auth.CLIAuthorization, error) {
	r, err := s.queries.UseCLIAuthorization(ctx, db.UseCLIAuthorizationParams{CodeHash: codeHash, UsedAt: &at})
	if err != nil {
		return auth.CLIAuthorization{}, notFound(err)
	}
	return auth.CLIAuthorization{
		ID: r.ID, UserID: r.UserID, CodeHash: r.CodeHash, CodeChallenge: r.CodeChallenge, RedirectURI: r.RedirectUri,
		ClientName: r.ClientName, ClientHost: r.ClientHost, IP: r.Ip, CreatedAt: r.CreatedAt.UTC(), ExpiresAt: r.ExpiresAt.UTC(),
	}, nil
}

// DeleteExpiredCLIAuthorizations implements [auth.CLIAuthorizationStore].
func (s *Store) DeleteExpiredCLIAuthorizations(ctx context.Context, now time.Time) (int, error) {
	n, err := s.queries.DeleteExpiredCLIAuthorizations(ctx, now)
	return int(n), err
}
