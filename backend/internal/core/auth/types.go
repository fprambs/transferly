// Package auth implements accounts, password hashing, sessions and CSRF protection.
package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Role is the instance-wide role of a user.
type Role string

const (
	// RoleAdmin can administer the instance.
	RoleAdmin Role = "admin"
	// RoleUser is a regular account.
	RoleUser Role = "user"
)

// Errors returned by the package.
var (
	// ErrInvalidCredentials means the email or password is wrong; it never says which.
	ErrInvalidCredentials = errors.New("invalid email or password")
	// ErrInvalidEmail means the email address is not acceptable.
	ErrInvalidEmail = errors.New("invalid email address")
	// ErrWeakPassword means the password does not meet the length policy.
	ErrWeakPassword = errors.New("password does not meet the policy")
	// ErrEmailTaken means an account with that email already exists.
	ErrEmailTaken = errors.New("email already registered")
	// ErrSetupComplete means the first administrator already exists.
	ErrSetupComplete = errors.New("setup already completed")
	// ErrNotFound means the requested record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrUnauthenticated means the session is missing, unknown or expired.
	ErrUnauthenticated = errors.New("not authenticated")
	// ErrUserDisabled means the account is disabled.
	ErrUserDisabled = errors.New("account disabled")
	// ErrInvalidLocale means the language tag is not valid BCP 47.
	ErrInvalidLocale = errors.New("invalid locale")
	// ErrInvalidTimezone means the name is not an IANA time zone.
	ErrInvalidTimezone = errors.New("invalid time zone")
	// ErrInvalidTheme means the theme identifier is not made of lower case letters, digits and hyphens, up to 40.
	ErrInvalidTheme = errors.New("invalid theme")
	// ErrInvalidFileDoubleClick means the double click action is neither download nor edit.
	ErrInvalidFileDoubleClick = errors.New("invalid file double click action")
	// ErrMalformedHash means a stored password hash cannot be parsed.
	ErrMalformedHash = errors.New("malformed password hash")
)

// User is an account.
type User struct {
	// ID identifies the user.
	ID uuid.UUID
	// Email is the normalized, lower-case address.
	Email string
	// PasswordHash is the encoded Argon2id hash of the login password.
	PasswordHash string
	// Role is the instance-wide role.
	Role Role
	// Disabled is true when the account cannot sign in.
	Disabled bool
	// Locale is the preferred interface language as a BCP 47 tag.
	Locale string
	// Timezone is the preferred IANA time zone name.
	Timezone string
	// Theme is the identifier of the interface theme the user chose. The server only checks its shape: the themes
	// themselves belong to the clients.
	Theme string
	// FileDoubleClick is what a double click on a file of the explorer does: [FileDoubleClickDownload], [FileDoubleClickEdit] or
	// [FileDoubleClickPreview].
	FileDoubleClick string
	// CreatedAt is when the account was created, in UTC.
	CreatedAt time.Time
}

// What a double click, or Enter, on a file of the explorer does (4.6, kept with the account since 4.14a).
const (
	// FileDoubleClickDownload downloads the file, as FileZilla does. It is the default.
	FileDoubleClickDownload = "download"
	// FileDoubleClickEdit opens the file in the remote editor.
	FileDoubleClickEdit = "edit"
	// FileDoubleClickPreview shows the file in the viewer without downloading it (ADR-0018).
	FileDoubleClickPreview = "preview"
)

// Preferences are the settings a user chooses for the interface. Empty values keep the current setting when updating.
type Preferences struct {
	// Locale is the interface language as a BCP 47 tag.
	Locale string
	// Timezone is the IANA time zone name.
	Timezone string
	// Theme is the identifier of the interface theme.
	Theme string
	// FileDoubleClick is what a double click on a file does.
	FileDoubleClick string
}

// Session is a signed-in browser session. The token itself is never stored, only its hash.
type Session struct {
	// ID identifies the session.
	ID uuid.UUID
	// UserID is the owner of the session.
	UserID uuid.UUID
	// TokenHash is the SHA-256 of the session token.
	TokenHash []byte
	// UserAgent is the client that created the session.
	UserAgent string
	// IP is the address that created the session.
	IP string
	// CreatedAt is when the session started, in UTC.
	CreatedAt time.Time
	// LastSeenAt is when the session was last used, in UTC.
	LastSeenAt time.Time
	// ExpiresAt is the absolute end of the session, in UTC.
	ExpiresAt time.Time
}

// ClientInfo describes the client that opens a session.
type ClientInfo struct {
	// UserAgent is the User-Agent header.
	UserAgent string
	// IP is the network address of the client.
	IP string
}

// Principal is an authenticated user together with the session that proves it.
type Principal struct {
	// User is the account.
	User User
	// Session is the active session.
	Session Session
}

// UserStore persists accounts.
type UserStore interface {
	// CreateFirstUser stores u only when no user exists yet, atomically; otherwise it returns [ErrSetupComplete].
	CreateFirstUser(ctx context.Context, u User) error
	// CreateUser stores u, returning [ErrEmailTaken] when the email exists.
	CreateUser(ctx context.Context, u User) error
	// UserByEmail returns the user with the given normalized email, or [ErrNotFound].
	UserByEmail(ctx context.Context, email string) (User, error)
	// UserByID returns the user with the given id, or [ErrNotFound].
	UserByID(ctx context.Context, id uuid.UUID) (User, error)
	// CountUsers returns how many accounts exist.
	CountUsers(ctx context.Context) (int, error)
	// UpdatePasswordHash replaces the stored password hash.
	UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string) error
	// UpdatePreferences stores the preferences of a user, every field already validated and filled.
	UpdatePreferences(ctx context.Context, id uuid.UUID, p Preferences) error
}

// SessionStore persists sessions.
type SessionStore interface {
	// CreateSession stores a session.
	CreateSession(ctx context.Context, s Session) error
	// SessionByTokenHash returns the session with the given token hash, or [ErrNotFound].
	SessionByTokenHash(ctx context.Context, hash []byte) (Session, error)
	// TouchSession records the last use of a session.
	TouchSession(ctx context.Context, id uuid.UUID, at time.Time) error
	// DeleteSession removes a session; a missing one is not an error.
	DeleteSession(ctx context.Context, id uuid.UUID) error
	// DeleteUserSessions removes every session of a user and returns how many were removed.
	DeleteUserSessions(ctx context.Context, userID uuid.UUID) (int, error)
	// DeleteExpiredSessions removes sessions past their absolute end or idle since idleBefore, returning the count.
	DeleteExpiredSessions(ctx context.Context, now, idleBefore time.Time) (int, error)
}
