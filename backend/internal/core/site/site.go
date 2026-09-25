// Package site implements the saved connections of a workspace: their settings, their encrypted secrets
// and the rules that keep them valid and safe to connect to.
package site

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Protocol is the file transfer protocol of a site.
type Protocol string

const (
	// SFTP is SSH file transfer.
	SFTP Protocol = "sftp"
	// FTP is plain FTP.
	FTP Protocol = "ftp"
	// FTPS is FTP over TLS.
	FTPS Protocol = "ftps"
)

// AuthMethod is how the user proves their identity to the remote server.
type AuthMethod string

const (
	// AuthPassword logs in with a password.
	AuthPassword AuthMethod = "password"
	// AuthKey logs in with an SSH private key, only for SFTP.
	AuthKey AuthMethod = "key"
)

// SecretKind is the type of a stored secret.
type SecretKind string

const (
	// SecretPassword is the account password.
	SecretPassword SecretKind = "password"
	// SecretPrivateKey is an SSH private key.
	SecretPrivateKey SecretKind = "private_key"
	// SecretPassphrase is the passphrase of a private key.
	SecretPassphrase SecretKind = "passphrase"
)

// Errors returned by the package.
var (
	// ErrNotFound means the site does not exist in the workspace.
	ErrNotFound = errors.New("site not found")
	// ErrNameTaken means another site of the workspace has the same name.
	ErrNameTaken = errors.New("site name already in use")
	// ErrLocked means secrets were involved and the vault is locked in this session.
	ErrLocked = errors.New("vault is locked")
	// ErrSecretTooLarge means a secret exceeds the allowed size.
	ErrSecretTooLarge = errors.New("secret is too large")
	// ErrNoSecret means the site has no secret of the requested kind.
	ErrNoSecret = errors.New("secret not stored")
	// ErrInvalidName means the site name is empty or too long.
	ErrInvalidName = errors.New("invalid site name")
	// ErrInvalidHost means the host is not a valid host name or address.
	ErrInvalidHost = errors.New("invalid host")
	// ErrInvalidPort means the port is outside 1 to 65535.
	ErrInvalidPort = errors.New("invalid port")
	// ErrInvalidProtocol means the protocol is not supported.
	ErrInvalidProtocol = errors.New("invalid protocol")
	// ErrInvalidAuthMethod means the authentication method is unknown or not available for the protocol.
	ErrInvalidAuthMethod = errors.New("invalid authentication method")
	// ErrInvalidUsername means the user name is empty or too long.
	ErrInvalidUsername = errors.New("invalid username")
	// ErrInvalidTimezone means the listing time zone is not an IANA name.
	ErrInvalidTimezone = errors.New("invalid time zone")
)

// Site is a saved connection. It never holds secrets, only which kinds are stored.
type Site struct {
	// ID identifies the site.
	ID uuid.UUID
	// WorkspaceID is the workspace that owns the site.
	WorkspaceID uuid.UUID
	// Name is the display name, unique in the workspace regardless of case.
	Name string
	// Protocol is the transfer protocol.
	Protocol Protocol
	// Host is the server name or address.
	Host string
	// Port is the server port.
	Port int
	// Username is the account name on the server.
	Username string
	// AuthMethod is how the account authenticates.
	AuthMethod AuthMethod
	// ListTimezone is the IANA zone in which the server prints listing times, for FTP servers without MLSD.
	ListTimezone string
	// Secrets lists the kinds of secret stored for the site.
	Secrets []SecretKind
	// CreatedAt is when the site was created, in UTC.
	CreatedAt time.Time
	// UpdatedAt is when the site was last changed, in UTC.
	UpdatedAt time.Time
}

// Sealed is an encrypted secret as stored.
type Sealed struct {
	// Kind is the type of secret.
	Kind SecretKind
	// DEKVersion is the version of the key that sealed it.
	DEKVersion int
	// Data is the ciphertext.
	Data []byte
}

// Store persists sites and their sealed secrets. Every lookup is scoped by workspace, so an identifier
// from another workspace is indistinguishable from a missing one.
type Store interface {
	// CreateSite stores a site and its sealed secrets atomically, or returns [ErrNameTaken].
	CreateSite(ctx context.Context, s Site, secrets []Sealed) error
	// UpdateSite replaces the settings of a site, stores the sealed secrets and deletes the removed kinds,
	// atomically. It returns [ErrNotFound] or [ErrNameTaken].
	UpdateSite(ctx context.Context, s Site, secrets []Sealed, remove []SecretKind) error
	// Site returns one site with its secret kinds, or [ErrNotFound].
	Site(ctx context.Context, workspaceID, id uuid.UUID) (Site, error)
	// Sites lists the sites of a workspace by name, with their secret kinds.
	Sites(ctx context.Context, workspaceID uuid.UUID) ([]Site, error)
	// DeleteSite removes a site and its secrets, or returns [ErrNotFound].
	DeleteSite(ctx context.Context, workspaceID, id uuid.UUID) error
	// Secret returns one sealed secret of a site, or [ErrNoSecret].
	Secret(ctx context.Context, workspaceID, siteID uuid.UUID, kind SecretKind) (Sealed, error)
}
