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

// Charsets lists the character sets a site may use for file names, by their lower case IANA names; UTF-8 is the default.
var Charsets = []string{
	"utf-8", "iso-8859-1", "iso-8859-2", "iso-8859-15", "windows-1250", "windows-1251", "windows-1252", "koi8-r", "shift_jis", "euc-jp", "gbk", "big5", "euc-kr",
}

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
	// SecretJumpPassword is the password of the jump host.
	SecretJumpPassword SecretKind = "jump_password"
	// SecretJumpPrivateKey is the SSH private key of the jump host.
	SecretJumpPrivateKey SecretKind = "jump_private_key"
	// SecretJumpPassphrase is the passphrase of the jump host's private key.
	SecretJumpPassphrase SecretKind = "jump_passphrase"
	// SecretProxyPassword is the password of the SOCKS5 or HTTP proxy.
	SecretProxyPassword SecretKind = "proxy_password"
)

// ProxyKind is a proxy protocol a site's connection can go through.
type ProxyKind string

const (
	// ProxyNone means the connection is made directly.
	ProxyNone ProxyKind = ""
	// ProxySOCKS5 tunnels the connection through a SOCKS5 proxy.
	ProxySOCKS5 ProxyKind = "socks5"
	// ProxyHTTP tunnels the connection through an HTTP CONNECT proxy.
	ProxyHTTP ProxyKind = "http"
)

// Proxy is the SOCKS5 or HTTP proxy a site's connection goes through, in place of a direct TCP connection. The zero
// value connects directly.
type Proxy struct {
	// Kind selects the proxy protocol; the empty value means no proxy.
	Kind ProxyKind
	// Host is the proxy's server name or address.
	Host string
	// Port is the proxy's server port.
	Port int
	// Username authenticates to the proxy; empty means no authentication.
	Username string
}

// Jump is the SSH server a site's connection tunnels through before reaching the site itself. Only SFTP sites may
// have one. The zero value connects directly.
type Jump struct {
	// Host is the jump host's server name or address; empty means no jump host.
	Host string
	// Port is the jump host's server port.
	Port int
	// Username is the account name on the jump host.
	Username string
	// AuthMethod is how the jump host account authenticates.
	AuthMethod AuthMethod
}

// Set reports whether a jump host is configured.
func (j Jump) Set() bool { return j.Host != "" }

// Set reports whether a proxy is configured.
func (p Proxy) Set() bool { return p.Kind != ProxyNone }

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
	// ErrInvalidCharset means the character set is not one of the supported ones, or is not UTF-8 for a protocol that only uses UTF-8.
	ErrInvalidCharset = errors.New("invalid character set")
	// ErrInvalidTimezone means the listing time zone is not an IANA name.
	ErrInvalidTimezone = errors.New("invalid time zone")
	// ErrInvalidSpeedLimit means the speed limit is negative, below the smallest cap or above the largest one.
	ErrInvalidSpeedLimit = errors.New("invalid speed limit")
	// ErrInvalidProxy means the proxy's kind, host or port is invalid.
	ErrInvalidProxy = errors.New("invalid proxy")
	// ErrInvalidJump means the jump host's host, port or authentication method is invalid, or one was given for a
	// protocol other than SFTP.
	ErrInvalidJump = errors.New("invalid jump host")
	// ErrCollectionNotFound means the collection does not exist in the workspace.
	ErrCollectionNotFound = errors.New("collection not found")
	// ErrCollectionNameTaken means the parent already has a collection with that name.
	ErrCollectionNameTaken = errors.New("collection name already in use")
	// ErrCollectionNotEmpty means the collection still holds sites or collections.
	ErrCollectionNotEmpty = errors.New("collection is not empty")
	// ErrCollectionCycle means a collection cannot move into itself or into one of its descendants.
	ErrCollectionCycle = errors.New("a collection cannot move into itself")
	// ErrCollectionTooDeep means the collections would nest deeper than the limit.
	ErrCollectionTooDeep = errors.New("collections are nested too deeply")
)

// Site is a saved connection. It never holds secrets, only which kinds are stored.
type Site struct {
	// ID identifies the site.
	ID uuid.UUID
	// WorkspaceID is the workspace that owns the site.
	WorkspaceID uuid.UUID
	// CollectionID is the collection that holds the site, or the nil identifier for the top level.
	CollectionID uuid.UUID
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
	// Charset is the character set the server uses for file names; FTP servers that do not speak UTF-8 need another one.
	Charset string
	// SpeedLimit caps the bytes per second the site's transfers move together, on top of the global and per-user caps of the
	// instance; zero means the site has no cap of its own.
	SpeedLimit int64
	// Proxy is the SOCKS5 or HTTP proxy the connection goes through; the zero value connects directly.
	Proxy Proxy
	// Jump is the SSH server the connection tunnels through before reaching the site; the zero value connects directly.
	// Only SFTP sites may have one.
	Jump Jump
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

// Collection groups sites and other collections into a tree.
type Collection struct {
	// ID identifies the collection.
	ID uuid.UUID
	// WorkspaceID is the workspace that owns the collection.
	WorkspaceID uuid.UUID
	// ParentID is the collection that holds this one, or the nil identifier for the top level.
	ParentID uuid.UUID
	// Name is the display name, unique among the collections of the same parent regardless of case.
	Name string
}

// CollectionStore persists collections. Every lookup is scoped by workspace.
type CollectionStore interface {
	// CreateCollection stores a collection, or returns [ErrCollectionNameTaken].
	CreateCollection(ctx context.Context, c Collection) error
	// UpdateCollection replaces the name and parent of a collection, or returns [ErrCollectionNotFound] or [ErrCollectionNameTaken].
	UpdateCollection(ctx context.Context, c Collection) error
	// Collection returns one collection, or [ErrCollectionNotFound].
	Collection(ctx context.Context, workspaceID, id uuid.UUID) (Collection, error)
	// Collections lists every collection of a workspace by name.
	Collections(ctx context.Context, workspaceID uuid.UUID) ([]Collection, error)
	// DeleteCollection removes an empty collection, or returns [ErrCollectionNotFound] or [ErrCollectionNotEmpty].
	DeleteCollection(ctx context.Context, workspaceID, id uuid.UUID) error
}
