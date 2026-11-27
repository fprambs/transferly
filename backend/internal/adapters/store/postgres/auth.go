package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fprambs/transferly/internal/adapters/store/postgres/db"
	"github.com/fprambs/transferly/internal/core/auth"
)

const (
	uniqueViolation = "23505"
	statusActive    = "active"
	statusDisabled  = "disabled"
)

var (
	_ auth.UserStore             = (*Store)(nil)
	_ auth.SessionStore          = (*Store)(nil)
	_ auth.CLIAuthorizationStore = (*Store)(nil)
)

// defaultTheme is the theme of users created without one, the same as the column default.
const defaultTheme = "transferly-dark"

// insertUserParams converts a user to the parameters of the insert query.
func insertUserParams(u auth.User) db.InsertUserParams {
	theme := u.Theme
	if theme == "" {
		theme = defaultTheme
	}
	fileDoubleClick := u.FileDoubleClick
	if fileDoubleClick == "" {
		fileDoubleClick = auth.FileDoubleClickDownload
	}
	status := statusActive
	if u.Disabled {
		status = statusDisabled
	}
	return db.InsertUserParams{
		ID: u.ID, Email: u.Email, PasswordHash: u.PasswordHash, Role: string(u.Role),
		Status: status, Locale: u.Locale, Timezone: u.Timezone, CreatedAt: u.CreatedAt, Theme: theme, FileDoubleClick: fileDoubleClick,
	}
}

// userFrom converts a stored user row to the core type.
func userFrom(id uuid.UUID, email, hash, role, status, locale, timezone, theme, fileDoubleClick string, createdAt time.Time) auth.User {
	return auth.User{
		ID: id, Email: email, PasswordHash: hash, Role: auth.Role(role), Disabled: status == statusDisabled,
		Locale: locale, Timezone: timezone, Theme: theme, FileDoubleClick: fileDoubleClick, CreatedAt: createdAt.UTC(),
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
	return userFrom(r.ID, r.Email, r.PasswordHash, r.Role, r.Status, r.Locale, r.Timezone, r.Theme, r.FileDoubleClick, r.CreatedAt), nil
}

// UserByID implements [auth.UserStore].
func (s *Store) UserByID(ctx context.Context, id uuid.UUID) (auth.User, error) {
	r, err := s.queries.UserByID(ctx, id)
	if err != nil {
		return auth.User{}, notFound(err)
	}
	return userFrom(r.ID, r.Email, r.PasswordHash, r.Role, r.Status, r.Locale, r.Timezone, r.Theme, r.FileDoubleClick, r.CreatedAt), nil
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
func (s *Store) UpdatePreferences(ctx context.Context, id uuid.UUID, p auth.Preferences) error {
	return s.queries.UpdatePreferences(ctx, db.UpdatePreferencesParams{ID: id, Locale: p.Locale, Timezone: p.Timezone, Theme: p.Theme, FileDoubleClick: p.FileDoubleClick})
}

// CreateSession implements [auth.SessionStore]. A session without a kind is a browser session, the same as the column default.
func (s *Store) CreateSession(ctx context.Context, session auth.Session) error {
	kind := session.Kind
	if kind == "" {
		kind = auth.SessionBrowser
	}
	return s.queries.InsertSession(ctx, db.InsertSessionParams{
		ID: session.ID, UserID: session.UserID, TokenHash: session.TokenHash, UserAgent: session.UserAgent,
		Ip: session.IP, CreatedAt: session.CreatedAt, LastSeenAt: session.LastSeenAt, ExpiresAt: session.ExpiresAt,
		Kind: string(kind), Name: session.Name, Host: session.Host,
	})
}

// SessionByTokenHash implements [auth.SessionStore].
func (s *Store) SessionByTokenHash(ctx context.Context, hash []byte) (auth.Session, error) {
	r, err := s.queries.SessionByTokenHash(ctx, hash)
	if err != nil {
		return auth.Session{}, notFound(err)
	}
	return sessionFrom(r), nil
}

// sessionFrom converts a stored session row to the core type.
func sessionFrom(r db.UserSession) auth.Session {
	return auth.Session{
		ID: r.ID, UserID: r.UserID, TokenHash: r.TokenHash, UserAgent: r.UserAgent, IP: r.Ip,
		CreatedAt: r.CreatedAt.UTC(), LastSeenAt: r.LastSeenAt.UTC(), ExpiresAt: r.ExpiresAt.UTC(),
		Kind: auth.SessionKind(r.Kind), Name: r.Name, Host: r.Host,
	}
}

// ListUserSessions implements [auth.SessionStore].
func (s *Store) ListUserSessions(ctx context.Context, userID uuid.UUID, kind auth.SessionKind) ([]auth.Session, error) {
	rows, err := s.queries.ListUserSessions(ctx, db.ListUserSessionsParams{UserID: userID, Kind: string(kind)})
	if err != nil {
		return nil, err
	}
	sessions := make([]auth.Session, 0, len(rows))
	for _, r := range rows {
		sessions = append(sessions, sessionFrom(r))
	}
	return sessions, nil
}

// TouchSession implements [auth.SessionStore].
func (s *Store) TouchSession(ctx context.Context, id uuid.UUID, at time.Time) error {
	return s.queries.TouchSession(ctx, db.TouchSessionParams{ID: id, LastSeenAt: at})
}

// DeleteSession implements [auth.SessionStore].
func (s *Store) DeleteSession(ctx context.Context, id uuid.UUID) error {
	return s.queries.DeleteSession(ctx, id)
}

// DeleteUserSession implements [auth.SessionStore].
func (s *Store) DeleteUserSession(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	n, err := s.queries.DeleteUserSession(ctx, db.DeleteUserSessionParams{ID: id, UserID: userID})
	return n > 0, err
}

// DeleteUserSessions implements [auth.SessionStore].
func (s *Store) DeleteUserSessions(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := s.queries.DeleteUserSessions(ctx, userID)
	return int(n), err
}

// DeleteExpiredSessions implements [auth.SessionStore].
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time, idle auth.IdleCutoffs) (int, error) {
	n, err := s.queries.DeleteExpiredSessions(ctx, db.DeleteExpiredSessionsParams{Now: now, BrowserIdleBefore: idle.Browser, CliIdleBefore: idle.CLI})
	return int(n), err
}
