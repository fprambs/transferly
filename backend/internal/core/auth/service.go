package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/ports"
)

const (
	tokenLength        = 32
	minPasswordRunes   = 12
	maxPasswordBytes   = 1024
	maxEmailLength     = 254
	touchInterval      = time.Minute
	defaultLocale      = "en"
	defaultTimezone    = "UTC"
	sessionTokenPrefix = "sentinel/session/v1"
)

// Config holds the session policy.
type Config struct {
	// IdleTimeout ends a session that was not used for this long.
	IdleTimeout time.Duration
	// AbsoluteTimeout ends a session this long after it started, however active it is.
	AbsoluteTimeout time.Duration
}

// Service implements sign-up of the first administrator, sign-in, sign-out and session validation.
type Service struct {
	users       UserStore
	sessions    SessionStore
	hasher      *Hasher
	clock       ports.Clock
	random      io.Reader
	config      Config
	newID       func() (uuid.UUID, error)
	placeholder string
}

// NewService returns a service over the given stores. newID may be nil to use UUIDv7.
func NewService(users UserStore, sessions SessionStore, hasher *Hasher, clock ports.Clock, random io.Reader, config Config) *Service {
	return &Service{
		users: users, sessions: sessions, hasher: hasher, clock: clock, random: random, config: config,
		newID:       uuid.NewV7,
		placeholder: hasher.placeholder(),
	}
}

// SetupRequired reports whether the first administrator has yet to be created.
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	n, err := s.users.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

// Setup creates the first administrator. It fails with [ErrSetupComplete] once any account exists.
func (s *Service) Setup(ctx context.Context, email, password string) (User, error) {
	user, err := s.newUser(ctx, email, password, RoleAdmin)
	if err != nil {
		return User{}, err
	}
	if err := s.users.CreateFirstUser(ctx, user); err != nil {
		return User{}, err
	}
	return user, nil
}

// Register creates a regular account.
func (s *Service) Register(ctx context.Context, email, password string) (User, error) {
	user, err := s.newUser(ctx, email, password, RoleUser)
	if err != nil {
		return User{}, err
	}
	if err := s.users.CreateUser(ctx, user); err != nil {
		return User{}, err
	}
	return user, nil
}

// newUser validates the input and builds a user with a hashed password.
func (s *Service) newUser(ctx context.Context, email, password string, role Role) (User, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	if err := checkPassword(password); err != nil {
		return User{}, err
	}
	id, err := s.newID()
	if err != nil {
		return User{}, fmt.Errorf("generating user id: %w", err)
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return User{}, err
	}
	return User{
		ID: id, Email: normalized, PasswordHash: hash, Role: role,
		Locale: defaultLocale, Timezone: defaultTimezone, CreatedAt: s.clock.Now(),
	}, nil
}

// Login verifies the credentials and opens a session.
//
// It returns the session token, which is shown to the client once, and the principal.
// Unknown accounts and wrong passwords are indistinguishable, in the error and in the time taken.
func (s *Service) Login(ctx context.Context, email, password string, client ClientInfo) (string, Principal, error) {
	user, err := s.users.UserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	stored := s.placeholder
	if err == nil {
		stored = user.PasswordHash
	} else if !errors.Is(err, ErrNotFound) {
		return "", Principal{}, err
	}

	ok, rehash, verifyErr := s.hasher.Verify(ctx, password, stored)
	if verifyErr != nil {
		return "", Principal{}, verifyErr
	}
	if err != nil || !ok {
		return "", Principal{}, ErrInvalidCredentials
	}
	if user.Disabled {
		return "", Principal{}, ErrUserDisabled
	}
	if rehash {
		if err := s.upgradeHash(ctx, user, password); err != nil {
			return "", Principal{}, err
		}
	}
	return s.openSession(ctx, user, client)
}

// upgradeHash re-hashes the password under the current parameters after a successful sign-in.
func (s *Service) upgradeHash(ctx context.Context, user User, password string) error {
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return err
	}
	return s.users.UpdatePasswordHash(ctx, user.ID, hash)
}

// openSession creates a session with a fresh random token, so any earlier token is never reused.
func (s *Service) openSession(ctx context.Context, user User, client ClientInfo) (string, Principal, error) {
	raw := make([]byte, tokenLength)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", Principal{}, fmt.Errorf("reading session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	id, err := s.newID()
	if err != nil {
		return "", Principal{}, fmt.Errorf("generating session id: %w", err)
	}
	now := s.clock.Now()
	session := Session{
		ID: id, UserID: user.ID, TokenHash: hashToken(token),
		UserAgent: client.UserAgent, IP: client.IP,
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(s.config.AbsoluteTimeout),
	}
	if err := s.sessions.CreateSession(ctx, session); err != nil {
		return "", Principal{}, err
	}
	return token, Principal{User: user, Session: session}, nil
}

// Authenticate validates a session token and returns its principal.
//
// Sessions past their absolute end or idle for too long are deleted and rejected.
// Use is recorded at most once per minute.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	session, err := s.sessions.SessionByTokenHash(ctx, hashToken(token))
	if errors.Is(err, ErrNotFound) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}

	now := s.clock.Now()
	if !now.Before(session.ExpiresAt) || now.Sub(session.LastSeenAt) >= s.config.IdleTimeout {
		if err := s.sessions.DeleteSession(ctx, session.ID); err != nil {
			return Principal{}, err
		}
		return Principal{}, ErrUnauthenticated
	}
	user, err := s.users.UserByID(ctx, session.UserID)
	if err != nil {
		return Principal{}, err
	}
	if user.Disabled {
		return Principal{}, ErrUserDisabled
	}
	if now.Sub(session.LastSeenAt) >= touchInterval {
		if err := s.sessions.TouchSession(ctx, session.ID, now); err != nil {
			return Principal{}, err
		}
		session.LastSeenAt = now
	}
	return Principal{User: user, Session: session}, nil
}

// Logout ends the session identified by token. Unknown tokens are ignored.
func (s *Service) Logout(ctx context.Context, token string) error {
	session, err := s.sessions.SessionByTokenHash(ctx, hashToken(token))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.sessions.DeleteSession(ctx, session.ID)
}

// LogoutAll ends every session of a user, for example after a password change.
func (s *Service) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	_, err := s.sessions.DeleteUserSessions(ctx, userID)
	return err
}

// PurgeExpired deletes expired sessions and returns how many were removed.
func (s *Service) PurgeExpired(ctx context.Context) (int, error) {
	now := s.clock.Now()
	return s.sessions.DeleteExpiredSessions(ctx, now, now.Add(-s.config.IdleTimeout))
}

// hashToken returns the SHA-256 of a session token, the only form that is stored.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(sessionTokenPrefix + "|" + token))
	return sum[:]
}

// NormalizeEmail trims and lower-cases an address and rejects anything that is not a plain mailbox.
func NormalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(email) > maxEmailLength {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || addr.Name != "" {
		return "", ErrInvalidEmail
	}
	return email, nil
}

// checkPassword enforces the length policy: at least 12 characters and at most 1024 bytes.
func checkPassword(password string) error {
	if len(password) > maxPasswordBytes || utf8.RuneCountInString(password) < minPasswordRunes {
		return ErrWeakPassword
	}
	return nil
}
