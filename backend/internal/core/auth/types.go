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

// SessionKind tells how a session is used: by a browser with the cookie, or by the terminal client with a bearer token (ADR-0029).
type SessionKind string

const (
	// SessionBrowser is a session of the web interface, carried by the cookie.
	SessionBrowser SessionKind = "browser"
	// SessionCLI is a session of the terminal client, carried by a bearer token.
	SessionCLI SessionKind = "cli"
)

// Session is a signed-in session of a browser or of the terminal client. The token itself is never stored, only its hash.
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
	// Kind tells whether the browser or the terminal client uses the session.
	Kind SessionKind
	// Name is the name the terminal client declared, empty for browser sessions.
	Name string
	// Host is the host name the terminal client declared it runs on, empty for browser sessions.
	Host string
}

// IdleCutoffs are the instants before which a session of each kind counts as idle.
type IdleCutoffs struct {
	// Browser is the cutoff of browser sessions.
	Browser time.Time
	// CLI is the cutoff of sessions of the terminal client.
	CLI time.Time
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
	// ListUserSessions returns the sessions of a kind of a user, most recently used first.
	ListUserSessions(ctx context.Context, userID uuid.UUID, kind SessionKind) ([]Session, error)
	// DeleteUserSession removes one session of a user, reporting whether it existed; the session of another user is left alone.
	DeleteUserSession(ctx context.Context, userID, id uuid.UUID) (bool, error)
	// DeleteExpiredSessions removes sessions past their absolute end or idle before the cutoff of their kind, returning the count.
	DeleteExpiredSessions(ctx context.Context, now time.Time, idle IdleCutoffs) (int, error)
}

// CLIAuthorization is an approval in the browser waiting to be exchanged for a session of the terminal client (ADR-0029). The code
// itself is never stored, only its hash.
type CLIAuthorization struct {
	// ID identifies the approval.
	ID uuid.UUID
	// UserID is the person who approved.
	UserID uuid.UUID
	// CodeHash is the SHA-256 of the single-use code.
	CodeHash []byte
	// CodeChallenge is the PKCE S256 challenge of the client.
	CodeChallenge string
	// RedirectURI is the loopback address the code was sent to, or empty when the person pastes it.
	RedirectURI string
	// ClientName and ClientHost are what the client declared about itself.
	ClientName, ClientHost string
	// IP is the address of the browser that approved.
	IP string
	// CreatedAt is when it was approved, in UTC.
	CreatedAt time.Time
	// ExpiresAt is when the code stops being accepted, in UTC.
	ExpiresAt time.Time
}

// CLIAuthorizationStore persists approvals of the terminal client.
type CLIAuthorizationStore interface {
	// CreateCLIAuthorization stores an approval.
	CreateCLIAuthorization(ctx context.Context, a CLIAuthorization) error
	// UseCLIAuthorization marks the approval with the given code hash as used at the given instant and returns it, atomically, so
	// only one of several exchanges of the same code gets it; an unknown or already used code is [ErrNotFound].
	UseCLIAuthorization(ctx context.Context, codeHash []byte, at time.Time) (CLIAuthorization, error)
	// DeleteExpiredCLIAuthorizations removes approvals that expired at or before now, returning the count.
	DeleteExpiredCLIAuthorizations(ctx context.Context, now time.Time) (int, error)
}
