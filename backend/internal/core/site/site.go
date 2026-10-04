// Package site implements the saved connections of a workspace: their settings, their encrypted secrets
// and the rules that keep them valid and safe to connect to.
package site

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Color is the label a site wears in the interface, so production and test servers tell apart at a glance (ADR-0021).
// The empty color means no label.
type Color string

// Colors lists the labels a site may wear, in the order the interface offers them; each theme draws them in its own tones.
var Colors = []Color{"red", "orange", "yellow", "green", "teal", "blue", "violet", "pink"}

// SecretKind is the type of a stored secret: the name of the secret field of the site's connector that holds it (ADR-0022).
type SecretKind string

// The secrets of the built-in connectors that connecting reads by name.
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
	// SecretAccount is the account an FTP site of the account logon type sends with ACCT.
	SecretAccount SecretKind = "account"
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
	// ErrInvalidSpeedLimit means the speed limit is negative, below the smallest cap or above the largest one.
	ErrInvalidSpeedLimit = errors.New("invalid speed limit")
	// ErrInvalidRemoteDir means the default remote folder is not absolute, is too long or holds control characters.
	ErrInvalidRemoteDir = errors.New("invalid default remote folder")
	// ErrInvalidColor means the label color is not one of [Colors].
	ErrInvalidColor = errors.New("invalid color")
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

// Site is a saved connection: what every site has, plus its connector and a configuration that follows the connector's schema
// (ADR-0022). It never holds secrets, only which kinds are stored.
type Site struct {
	// ID identifies the site.
	ID uuid.UUID
	// WorkspaceID is the workspace that owns the site.
	WorkspaceID uuid.UUID
	// CollectionID is the collection that holds the site, or the nil identifier for the top level.
	CollectionID uuid.UUID
	// Name is the display name, unique in the workspace regardless of case.
	Name string
	// Connector is the type of the connector the site uses, such as sftp or ftp.
	Connector string
	// Config is the configuration of the connector, settled by its schema: defaults filled in, fields that do not apply dropped.
	Config map[string]string
	// SpeedLimit caps the bytes per second the site's transfers move together, on top of the global and per-user caps of the
	// instance; zero means the site has no cap of its own.
	SpeedLimit int64
	// RemoteDir is the folder the site opens in, as FileZilla's default remote directory; empty opens the root (4.14i).
	RemoteDir string
	// Color is the label the site wears in the interface; empty for none.
	Color Color
	// Secrets lists the kinds of secret stored for the site.
	Secrets []SecretKind
	// CreatedAt is when the site was created, in UTC.
	CreatedAt time.Time
	// UpdatedAt is when the site was last changed, in UTC.
	UpdatedAt time.Time
}

// Setting returns a setting of the site's configuration, or empty when it has none.
func (s Site) Setting(name string) string { return s.Config[name] }

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

// KeyError is a private key given for a site that cannot be used, and the field of the connector that holds it. It unwraps to
// the [sshkey] error that says why.
type KeyError struct {
	// Field is the name of the private key field, such as private_key or jump_private_key.
	Field string
	// Err is why the key cannot be used: [sshkey.ErrUnreadable], [sshkey.ErrPublicKey], [sshkey.ErrPassphraseRequired],
	// [sshkey.ErrWrongPassphrase] or an [*sshkey.UnsupportedError].
	Err error
}

// Error implements [error].
func (e *KeyError) Error() string { return e.Field + ": " + e.Err.Error() }

// Unwrap returns the reason.
func (e *KeyError) Unwrap() error { return e.Err }
