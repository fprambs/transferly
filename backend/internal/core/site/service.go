package site

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/sshkey"
	"github.com/fprambs/transferly/internal/core/vault"
	"github.com/fprambs/transferly/internal/core/workspace"
)

const (
	// maxRemoteDirBytes is the longest default remote folder.
	maxRemoteDirBytes = 4096
	maxNameRunes      = 100
	maxSecretBytes    = 1024
	maxKeyBytes       = 64 * 1024
	// minSpeedLimit is the smallest speed limit a site may have, in bytes per second (1 KiB): below it a single chunk of
	// data would keep a request waiting for hours.
	minSpeedLimit = 1024
	// maxSpeedLimit is the largest speed limit a site may have, in bytes per second (1 TiB).
	maxSpeedLimit = 1 << 40
)

// Authorizer checks that a user may perform an action in a workspace.
type Authorizer interface {
	// Authorize returns the role of the user, [workspace.ErrNotFound] for non members and
	// [workspace.ErrForbidden] for members whose role does not allow the action.
	Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action workspace.Action) (workspace.Role, error)
}

// Keys gives access to the keys that seal the secrets of a workspace, opened with the vault of a session.
type Keys interface {
	// Current returns the key that seals new secrets of a workspace and its version, or [vault.ErrLocked]. The caller wipes the key.
	Current(ctx context.Context, actor, workspaceID uuid.UUID, session string) ([]byte, int, error)
	// Version returns the key that opens secrets sealed at a version, or [vault.ErrLocked]. The caller wipes it.
	Version(ctx context.Context, actor, workspaceID uuid.UUID, session string, version int) ([]byte, error)
	// Unlocked reports whether the session's vault is unlocked.
	Unlocked(session string) bool
}

// Guard vets the hosts sites may point to.
type Guard interface {
	// CheckHost returns an error when the host resolves to a destination the server must not reach.
	CheckHost(ctx context.Context, host string) error
}

// Schemas finds the descriptors of the connectors, whose schemas sites follow (ADR-0022).
type Schemas interface {
	// Lookup returns the descriptor of a connector, or [connectors.ErrUnknown].
	Lookup(kind string) (connectors.Descriptor, error)
}

// Input holds the settings of a site as given by the user.
type Input struct {
	// Name is the display name.
	Name string
	// CollectionID is the collection to keep the site in; the nil identifier keeps it at the top level.
	CollectionID uuid.UUID
	// Connector is the type of the connector the site uses.
	Connector string
	// Config is the configuration of the connector, without its secrets.
	Config map[string]string
	// SpeedLimit caps the bytes per second the site's transfers move together; zero means no cap of its own.
	SpeedLimit int64
	// RemoteDir is the folder the site opens in; empty, or the root, opens the root. It is stored cleaned.
	RemoteDir string
	// Color is the label the site wears in the interface; empty for none.
	Color Color
}

// SecretChanges tells which secrets to write, by the name of their field. A secret left out stays as it is, an empty value
// removes it and any other value replaces it.
type SecretChanges map[SecretKind]string

// Service implements the site use cases. Secrets are sealed with the key of their workspace: the DEK of the owner for a personal
// workspace and the shared workspace key for a team.
type Service struct {
	store interface {
		Store
		CollectionStore
	}
	authz    Authorizer
	keys     Keys
	guard    Guard
	schemas  Schemas
	clock    ports.Clock
	random   io.Reader
	newID    func() (uuid.UUID, error)
	quota    Quota
	offering Offering
}

// Quota decides whether a workspace may have another site.
type Quota interface {
	// CheckSite returns an error when the workspace already has as many sites as it may, such as [limits.ErrSiteQuota].
	CheckSite(ctx context.Context, workspaceID uuid.UUID) error
}

// UseQuota makes the service ask a quota before it creates a site. Without one nothing is refused. Call it before the service is used.
func (s *Service) UseQuota(q Quota) { s.quota = q }

// Offering tells whether the instance offers a connector (ADR-0019).
type Offering interface {
	// CheckConnector returns [connectors.ErrDisabled] when the instance does not offer the connector.
	CheckConnector(ctx context.Context, kind string) error
}

// UseOffering makes the service refuse sites of a connector the instance does not offer: new ones, and existing ones moved to it. Without
// it every connector of the catalog is offered. Call it before the service is used.
func (s *Service) UseOffering(o Offering) { s.offering = o }

// checkOffered refuses a connector the instance does not offer.
func (s *Service) checkOffered(ctx context.Context, kind string) error {
	if s.offering == nil {
		return nil
	}
	return s.offering.CheckConnector(ctx, kind)
}

// NewService returns a service. newID may be nil to use UUIDv7.
func NewService(store interface {
	Store
	CollectionStore
}, authz Authorizer, keys Keys, guard Guard, schemas Schemas, clock ports.Clock, random io.Reader, newID func() (uuid.UUID, error)) *Service {
	if newID == nil {
		newID = uuid.NewV7
	}
	return &Service{store: store, authz: authz, keys: keys, guard: guard, schemas: schemas, clock: clock, random: random, newID: newID}
}

// Settle validates the settings of a site and fills in the defaults: the name is trimmed, and the configuration is settled by the schema of
// its connector (see [connectors.Descriptor.Validate]). An unknown connector is a [*connectors.FieldError] of the connector field. It
// returns the settled input and the descriptor of its connector.
func (s *Service) Settle(in Input) (Input, connectors.Descriptor, error) {
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n == 0 || n > maxNameRunes {
		return in, connectors.Descriptor{}, ErrInvalidName
	}
	if in.SpeedLimit != 0 && (in.SpeedLimit < minSpeedLimit || in.SpeedLimit > maxSpeedLimit) {
		return in, connectors.Descriptor{}, ErrInvalidSpeedLimit
	}
	remoteDir, err := normalizeRemoteDir(in.RemoteDir)
	if err != nil {
		return in, connectors.Descriptor{}, err
	}
	in.RemoteDir = remoteDir
	if in.Color != "" && !slices.Contains(Colors, in.Color) {
		return in, connectors.Descriptor{}, ErrInvalidColor
	}
	d, err := s.schemas.Lookup(in.Connector)
	if errors.Is(err, connectors.ErrUnknown) {
		return in, connectors.Descriptor{}, &connectors.FieldError{Field: "connector", Reason: connectors.ReasonUnknown}
	}
	if err != nil {
		return in, connectors.Descriptor{}, err
	}
	if in.Config, err = d.Validate(in.Config); err != nil {
		return in, connectors.Descriptor{}, err
	}
	return in, d, nil
}

// CheckHosts asks the guard about every host a settled configuration points to: the server's, and those of its proxy and jump host when
// they apply.
func (s *Service) CheckHosts(ctx context.Context, d connectors.Descriptor, config map[string]string) error {
	for _, f := range d.Fields {
		if f.Kind != connectors.Host || config[f.Name] == "" {
			continue
		}
		if err := s.guard.CheckHost(ctx, config[f.Name]); err != nil {
			return err
		}
	}
	return nil
}

// PlanSecrets settles the secret changes of a site against the schema of its connector: a secret the connector does not have is refused, a
// private key is read with its passphrase (see [checkKey]), and every secret whose field does not apply to the configuration is removed,
// stored or given, so a site that asks for its password when connecting (ADR-0010), an anonymous one and an interactive one (ADR-0016) keep
// none. stored lists the kinds the site already has, which are removed too when they are not secrets of its connector.
func PlanSecrets(d connectors.Descriptor, config map[string]string, changes SecretChanges, stored []SecretKind) (SecretChanges, error) {
	out := SecretChanges{}
	for kind, value := range changes {
		f, ok := d.Field(string(kind))
		if !ok || !f.IsSecret() {
			return nil, &connectors.FieldError{Field: string(kind), Reason: connectors.ReasonUnknown}
		}
		if f.Applies(config) {
			out[kind] = value
		}
	}
	for _, kind := range stored {
		if f, ok := d.Field(string(kind)); !ok || !f.Applies(config) {
			out[kind] = ""
		}
	}
	for _, f := range d.Fields {
		kind := SecretKind(f.Name)
		if !f.IsSecret() || f.Applies(config) {
			continue
		}
		if _, given := changes[kind]; given {
			out[kind] = ""
		}
	}
	for _, f := range d.Fields {
		if f.Kind != connectors.PrivateKey || out[SecretKind(f.Name)] == "" {
			continue
		}
		if err := checkKey(f, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// checkKey reads a private key being saved with the passphrase given with it (4.8g): a key that cannot be used is refused with a [*KeyError],
// and a usable one is stored in the form connecting reads, PuTTY and encrypted PKCS#8 keys converted to OpenSSH's. A key that is not
// encrypted drops any passphrase, which would only stop it from opening. A key larger than any key file is refused as any secret is.
func checkKey(f connectors.Field, changes SecretChanges) error {
	key := changes[SecretKind(f.Name)]
	if len(key) > maxKeyBytes {
		return ErrSecretTooLarge
	}
	read, err := sshkey.Read([]byte(key), []byte(changes[SecretKind(f.Passphrase)]))
	if err != nil {
		return &KeyError{Field: f.Name, Err: err}
	}
	changes[SecretKind(f.Name)] = string(read.PEM)
	if !read.Encrypted {
		changes[SecretKind(f.Passphrase)] = ""
	}
	return nil
}

// plan is the set of secret writes derived from the changes.
type plan struct {
	set    []Sealed
	remove []SecretKind
}

// sealChanges turns secret changes into writes for a site. Unlocking is needed only to write values.
func (s *Service) sealChanges(ctx context.Context, actor, workspaceID uuid.UUID, session string, siteID uuid.UUID, d connectors.Descriptor, changes SecretChanges) (plan, error) {
	var p plan
	var dek []byte
	var version int
	defer func() { vault.Wipe(dek) }()
	ordered := make([]SecretKind, 0, len(changes))
	for kind := range changes {
		ordered = append(ordered, kind)
	}
	slices.Sort(ordered)
	for _, kind := range ordered {
		value := changes[kind]
		limit := maxSecretBytes
		if f, _ := d.Field(string(kind)); f.Kind == connectors.PrivateKey {
			limit = maxKeyBytes
		}
		switch {
		case value == "":
			p.remove = append(p.remove, kind)
		case len(value) > limit:
			return plan{}, ErrSecretTooLarge
		default:
			if dek == nil {
				key, current, err := s.keys.Current(ctx, actor, workspaceID, session)
				if errors.Is(err, vault.ErrLocked) {
					return plan{}, ErrLocked
				}
				if err != nil {
					return plan{}, err
				}
				dek, version = key, current
			}
			sealed, err := vault.SealSecret(s.random, dek, vault.SecretRef{Site: siteID.String(), Kind: string(kind), DEKVersion: version}, []byte(value))
			if err != nil {
				return plan{}, fmt.Errorf("sealing %s: %w", kind, err)
			}
			p.set = append(p.set, Sealed{Kind: kind, DEKVersion: version, Data: sealed})
		}
	}
	return p, nil
}

// CanSeal reports whether a session can store secrets, which needs its vault to be unlocked.
func (s *Service) CanSeal(session string) bool {
	return s.keys.Unlocked(session)
}

// Create saves a new site and its secrets.
func (s *Service) Create(ctx context.Context, actor, workspaceID uuid.UUID, session string, in Input, changes SecretChanges) (Site, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return Site{}, err
	}
	if s.quota != nil {
		if err := s.quota.CheckSite(ctx, workspaceID); err != nil {
			return Site{}, err
		}
	}
	in, d, err := s.Settle(in)
	if err != nil {
		return Site{}, err
	}
	if err := s.checkOffered(ctx, in.Connector); err != nil {
		return Site{}, err
	}
	if changes, err = PlanSecrets(d, in.Config, changes, nil); err != nil {
		return Site{}, err
	}
	if err := s.CheckHosts(ctx, d, in.Config); err != nil {
		return Site{}, err
	}
	if err := s.checkCollection(ctx, workspaceID, in.CollectionID); err != nil {
		return Site{}, err
	}
	id, err := s.newID()
	if err != nil {
		return Site{}, fmt.Errorf("generating site id: %w", err)
	}
	p, err := s.sealChanges(ctx, actor, workspaceID, session, id, d, changes)
	if err != nil {
		return Site{}, err
	}
	now := s.clock.Now().UTC()
	created := build(in, id, workspaceID, now, now)
	if err := s.store.CreateSite(ctx, created, p.set); err != nil {
		return Site{}, err
	}
	created.Secrets = kinds(p.set)
	return created, nil
}

// normalizeRemoteDir checks the default remote folder of a site and cleans it: empty and the root both mean the root and are
// stored empty, anything else must be an absolute path without control characters.
func normalizeRemoteDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil
	}
	if !strings.HasPrefix(dir, "/") || len(dir) > maxRemoteDirBytes || !utf8.ValidString(dir) || strings.ContainsFunc(dir, unicode.IsControl) {
		return "", ErrInvalidRemoteDir
	}
	if dir = path.Clean(dir); dir == "/" {
		return "", nil
	}
	return dir, nil
}

// build assembles a site from settled input.
func build(in Input, id, workspaceID uuid.UUID, createdAt, updatedAt time.Time) Site {
	return Site{
		ID: id, WorkspaceID: workspaceID, CollectionID: in.CollectionID, Name: in.Name, Connector: in.Connector, Config: in.Config,
		SpeedLimit: in.SpeedLimit, RemoteDir: in.RemoteDir, Color: in.Color, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
}

// kinds lists the kinds of a set of sealed secrets.
func kinds(sealed []Sealed) []SecretKind {
	var out []SecretKind
	for _, s := range sealed {
		out = append(out, s.Kind)
	}
	return out
}

// Update replaces the settings of a site and applies the secret changes. Secrets of fields that no longer apply are removed.
func (s *Service) Update(ctx context.Context, actor, workspaceID, id uuid.UUID, session string, in Input, changes SecretChanges) (Site, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return Site{}, err
	}
	in, d, err := s.Settle(in)
	if err != nil {
		return Site{}, err
	}
	existing, err := s.store.Site(ctx, workspaceID, id)
	if err != nil {
		return Site{}, err
	}
	if existing.Connector != in.Connector {
		if err := s.checkOffered(ctx, in.Connector); err != nil {
			return Site{}, err
		}
	}
	if changes, err = PlanSecrets(d, in.Config, changes, existing.Secrets); err != nil {
		return Site{}, err
	}
	if err := s.CheckHosts(ctx, d, in.Config); err != nil {
		return Site{}, err
	}
	if err := s.checkCollection(ctx, workspaceID, in.CollectionID); err != nil {
		return Site{}, err
	}
	p, err := s.sealChanges(ctx, actor, workspaceID, session, id, d, changes)
	if err != nil {
		return Site{}, err
	}
	updated := build(in, id, workspaceID, existing.CreatedAt, s.clock.Now().UTC())
	if err := s.store.UpdateSite(ctx, updated, p.set, p.remove); err != nil {
		return Site{}, err
	}
	return s.store.Site(ctx, workspaceID, id)
}

// Delete removes a site and its secrets.
func (s *Service) Delete(ctx context.Context, actor, workspaceID, id uuid.UUID) error {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return err
	}
	return s.store.DeleteSite(ctx, workspaceID, id)
}

// Get returns one site of a workspace the actor may browse.
func (s *Service) Get(ctx context.Context, actor, workspaceID, id uuid.UUID) (Site, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionBrowse); err != nil {
		return Site{}, err
	}
	return s.store.Site(ctx, workspaceID, id)
}

// List returns the sites of a workspace the actor may browse.
func (s *Service) List(ctx context.Context, actor, workspaceID uuid.UUID) ([]Site, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionBrowse); err != nil {
		return nil, err
	}
	return s.store.Sites(ctx, workspaceID)
}

// OpenSecret decrypts one secret of a site for connecting. It needs the vault unlocked in the session
// and returns [ErrNoSecret] when none is stored. The caller wipes the result after use.
func (s *Service) OpenSecret(ctx context.Context, actor, workspaceID, id uuid.UUID, session string, kind SecretKind) ([]byte, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionBrowse); err != nil {
		return nil, err
	}
	sealed, err := s.store.Secret(ctx, workspaceID, id, kind)
	if err != nil {
		return nil, err
	}
	dek, err := s.keys.Version(ctx, actor, workspaceID, session, sealed.DEKVersion)
	if errors.Is(err, vault.ErrLocked) {
		return nil, ErrLocked
	}
	if err != nil {
		return nil, err
	}
	defer vault.Wipe(dek)
	return vault.OpenSecret(dek, vault.SecretRef{Site: id.String(), Kind: string(kind), DEKVersion: sealed.DEKVersion}, sealed.Data)
}
