package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	"github.com/fprambs/sentinel/internal/core/auth"
)

const (
	uniqueViolation = "23505"
	statusActive    = "active"
	statusDisabled  = "disabled"
)

var (
	_ auth.UserStore    = (*Store)(nil)
	_ auth.SessionStore = (*Store)(nil)
)

// defaultTheme is the theme of users created without one, the same as the column default.
const defaultTheme = "sentinel-dark"

// insertUserParams converts a user to the parameters of the insert query.
func insertUserParams(u auth.User) db.InsertUserParams {
	theme := u.Theme
	if theme == "" {
		theme = defaultTheme
	}
	status := statusActive
	if u.Disabled {
		status = statusDisabled
	}
	return db.InsertUserParams{
		ID: u.ID, Email: u.Email, PasswordHash: u.PasswordHash, Role: string(u.Role),
		Status: status, Locale: u.Locale, Timezone: u.Timezone, CreatedAt: u.CreatedAt, Theme: theme,
	}
}

// userFrom converts a stored user row to the core type.
func userFrom(id uuid.UUID, email, hash, role, status, locale, timezone, theme string, createdAt time.Time) auth.User {
	return auth.User{
		ID: id, Email: email, PasswordHash: hash, Role: auth.Role(role), Disabled: status == statusDisabled,
		Locale: locale, Timezone: timezone, Theme: theme, CreatedAt: createdAt.UTC(),
	}
}

// errNoRows is the driver's error for a query that found no row.
var errNoRows = pgx.ErrNoRows

// notFound maps the driver's missing row error to [auth.ErrNotFound].
func notFound(err error) error {
	if errors.Is(err, errNoRows) {
		return auth.ErrNotFound
	}
	return err
}

// CreateFirstUser implements [auth.UserStore]. An advisory lock serializes concurrent callers,
// so exactly one of them creates the first account.
func (s *Store) CreateFirstUser(ctx context.Context, u auth.User) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.queries.WithTx(tx)
	if err := q.LockUserCreation(ctx); err != nil {
		return err
	}
	count, err := q.CountUsers(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return auth.ErrSetupComplete
	}
	if err := q.InsertUser(ctx, insertUserParams(u)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreateUser implements [auth.UserStore].
func (s *Store) CreateUser(ctx context.Context, u auth.User) error {
	err := s.queries.InsertUser(ctx, insertUserParams(u))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return auth.ErrEmailTaken
	}
	return err
}

// UserByEmail implements [auth.UserStore].
func (s *Store) UserByEmail(ctx context.Context, email string) (auth.User, error) {
	r, err := s.queries.UserByEmail(ctx, email)
	if err != nil {
		return auth.User{}, notFound(err)
	}
	return userFrom(r.ID, r.Email, r.PasswordHash, r.Role, r.Status, r.Locale, r.Timezone, r.Theme, r.CreatedAt), nil
}

// UserByID implements [auth.UserStore].
func (s *Store) UserByID(ctx context.Context, id uuid.UUID) (auth.User, error) {
	r, err := s.queries.UserByID(ctx, id)
	if err != nil {
		return auth.User{}, notFound(err)
	}
	return userFrom(r.ID, r.Email, r.PasswordHash, r.Role, r.Status, r.Locale, r.Timezone, r.Theme, r.CreatedAt), nil
}

// CountUsers implements [auth.UserStore].
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	n, err := s.queries.CountUsers(ctx)
	return int(n), err
}

// UpdatePasswordHash implements [auth.UserStore].
func (s *Store) UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string) error {
	return s.queries.UpdatePasswordHash(ctx, db.UpdatePasswordHashParams{ID: id, PasswordHash: hash})
}

// UpdatePreferences implements [auth.UserStore].
func (s *Store) UpdatePreferences(ctx context.Context, id uuid.UUID, locale, timezone, theme string) error {
	return s.queries.UpdatePreferences(ctx, db.UpdatePreferencesParams{ID: id, Locale: locale, Timezone: timezone, Theme: theme})
}

// CreateSession implements [auth.SessionStore].
func (s *Store) CreateSession(ctx context.Context, session auth.Session) error {
	return s.queries.InsertSession(ctx, db.InsertSessionParams{
		ID: session.ID, UserID: session.UserID, TokenHash: session.TokenHash, UserAgent: session.UserAgent,
		Ip: session.IP, CreatedAt: session.CreatedAt, LastSeenAt: session.LastSeenAt, ExpiresAt: session.ExpiresAt,
	})
}

// SessionByTokenHash implements [auth.SessionStore].
func (s *Store) SessionByTokenHash(ctx context.Context, hash []byte) (auth.Session, error) {
	r, err := s.queries.SessionByTokenHash(ctx, hash)
	if err != nil {
		return auth.Session{}, notFound(err)
	}
	return auth.Session{
		ID: r.ID, UserID: r.UserID, TokenHash: r.TokenHash, UserAgent: r.UserAgent, IP: r.Ip,
		CreatedAt: r.CreatedAt.UTC(), LastSeenAt: r.LastSeenAt.UTC(), ExpiresAt: r.ExpiresAt.UTC(),
	}, nil
}

// TouchSession implements [auth.SessionStore].
func (s *Store) TouchSession(ctx context.Context, id uuid.UUID, at time.Time) error {
	return s.queries.TouchSession(ctx, db.TouchSessionParams{ID: id, LastSeenAt: at})
}

// DeleteSession implements [auth.SessionStore].
func (s *Store) DeleteSession(ctx context.Context, id uuid.UUID) error {
	return s.queries.DeleteSession(ctx, id)
}

// DeleteUserSessions implements [auth.SessionStore].
func (s *Store) DeleteUserSessions(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := s.queries.DeleteUserSessions(ctx, userID)
	return int(n), err
}

// DeleteExpiredSessions implements [auth.SessionStore].
func (s *Store) DeleteExpiredSessions(ctx context.Context, now, idleBefore time.Time) (int, error) {
	n, err := s.queries.DeleteExpiredSessions(ctx, db.DeleteExpiredSessionsParams{ExpiresAt: now, LastSeenAt: idleBefore})
	return int(n), err
}
