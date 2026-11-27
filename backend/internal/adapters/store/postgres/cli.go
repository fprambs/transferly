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

// CreateCLILink implements [auth.CLIAuthorizationStore].
func (s *Store) CreateCLILink(ctx context.Context, l auth.CLILink) error {
	return s.queries.InsertCLILink(ctx, db.InsertCLILinkParams{
		Code: l.Code, CodeChallenge: l.Request.CodeChallenge, RedirectUri: l.Request.RedirectURI, State: l.State,
		ClientName: l.Request.ClientName, ClientHost: l.Request.ClientHost, Ip: l.IP, CreatedAt: l.CreatedAt, ExpiresAt: l.ExpiresAt,
	})
}

// CLILink implements [auth.CLIAuthorizationStore].
func (s *Store) CLILink(ctx context.Context, code string, now time.Time) (auth.CLILink, error) {
	r, err := s.queries.CLILink(ctx, db.CLILinkParams{Code: code, Now: now})
	if err != nil {
		return auth.CLILink{}, notFound(err)
	}
	return auth.CLILink{
		Code: r.Code, State: r.State, IP: r.Ip, CreatedAt: r.CreatedAt.UTC(), ExpiresAt: r.ExpiresAt.UTC(),
		Request: auth.CLIRequest{CodeChallenge: r.CodeChallenge, RedirectURI: r.RedirectUri, ClientName: r.ClientName, ClientHost: r.ClientHost},
	}, nil
}

// DeleteExpiredCLILinks implements [auth.CLIAuthorizationStore].
func (s *Store) DeleteExpiredCLILinks(ctx context.Context, now time.Time) (int, error) {
	n, err := s.queries.DeleteExpiredCLILinks(ctx, now)
	return int(n), err
}
