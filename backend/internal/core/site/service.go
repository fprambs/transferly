package site

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

const (
	maxNameRunes     = 100
	maxUsernameRunes = 255
	maxHostLength    = 253
	maxSecretBytes   = 1024
	maxKeyBytes      = 64 * 1024
	defaultDEKLevel  = 1
)

// labelPattern matches one label of a host name.
var labelPattern = regexp.MustCompile(`^[A-Za-z0-9_]([A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?$`)

// defaultPorts are the ports used when a site does not name one.
var defaultPorts = map[Protocol]int{SFTP: 22, FTP: 21, FTPS: 21}

// Authorizer checks that a user may perform an action in a workspace.
type Authorizer interface {
	// Authorize returns the role of the user, [workspace.ErrNotFound] for non members and
	// [workspace.ErrForbidden] for members whose role does not allow the action.
	Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action workspace.Action) (workspace.Role, error)
}

// Keys gives access to the decrypted vault key of a session.
type Keys interface {
	// Key returns a copy of the session's key, or [vault.ErrLocked]. The caller wipes it.
	Key(session string) ([]byte, error)
}

// Guard vets the hosts sites may point to.
type Guard interface {
	// CheckHost returns an error when the host resolves to a destination the server must not reach.
	CheckHost(ctx context.Context, host string) error
}

// Input holds the settings of a site as given by the user.
type Input struct {
	// Name is the display name.
	Name string
	// Protocol is the transfer protocol.
	Protocol Protocol
	// Host is the server name or address.
	Host string
	// Port is the server port; zero selects the protocol's default.
	Port int
	// Username is the account name.
	Username string
	// AuthMethod is how the account authenticates; empty selects password.
	AuthMethod AuthMethod
	// ListTimezone is the IANA zone of listing times; empty selects UTC.
	ListTimezone string
}

// SecretChanges tells which secrets to write. A nil field leaves the secret as it is, an empty string
// removes it and any other value replaces it.
type SecretChanges struct {
	// Password is the account password.
	Password *string
	// PrivateKey is the SSH private key.
	PrivateKey *string
	// Passphrase is the passphrase of the private key.
	Passphrase *string
}

// Service implements the site use cases. Secrets are sealed with the vault key of the user who saves them;
// sharing keys among the members of a team workspace belongs to a later phase.
type Service struct {
	store  Store
	authz  Authorizer
	keys   Keys
	guard  Guard
	clock  ports.Clock
	random io.Reader
	newID  func() (uuid.UUID, error)
}

// NewService returns a service. newID may be nil to use UUIDv7.
func NewService(store Store, authz Authorizer, keys Keys, guard Guard, clock ports.Clock, random io.Reader, newID func() (uuid.UUID, error)) *Service {
	if newID == nil {
		newID = uuid.NewV7
	}
	return &Service{store: store, authz: authz, keys: keys, guard: guard, clock: clock, random: random, newID: newID}
}

// normalize validates the input and fills in defaults.
func normalize(in Input) (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.TrimSpace(in.Host)
	in.Username = strings.TrimSpace(in.Username)
	if n := utf8.RuneCountInString(in.Name); n == 0 || n > maxNameRunes {
		return in, ErrInvalidName
	}
	if _, ok := defaultPorts[in.Protocol]; !ok {
		return in, ErrInvalidProtocol
	}
	if !validHost(in.Host) {
		return in, ErrInvalidHost
	}
	if in.Port == 0 {
		in.Port = defaultPorts[in.Protocol]
	}
	if in.Port < 1 || in.Port > 65535 {
		return in, ErrInvalidPort
	}
	if n := utf8.RuneCountInString(in.Username); n == 0 || n > maxUsernameRunes {
		return in, ErrInvalidUsername
	}
	if in.AuthMethod == "" {
		in.AuthMethod = AuthPassword
	}
	if in.AuthMethod != AuthPassword && (in.AuthMethod != AuthKey || in.Protocol != SFTP) {
		return in, ErrInvalidAuthMethod
	}
	if in.ListTimezone == "" {
		in.ListTimezone = "UTC"
	}
	if _, err := time.LoadLocation(in.ListTimezone); err != nil || in.ListTimezone == "Local" {
		return in, ErrInvalidTimezone
	}
	return in, nil
}

// validHost accepts IP addresses and dotted host names, without scheme, port, path or spaces.
func validHost(host string) bool {
	if host == "" || len(host) > maxHostLength {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if !labelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

// plan is the set of secret writes derived from the changes.
type plan struct {
	set    []Sealed
	remove []SecretKind
}

// sealChanges turns secret changes into writes for a site. Unlocking is needed only to write values.
func (s *Service) sealChanges(session string, siteID uuid.UUID, changes SecretChanges) (plan, error) {
	var p plan
	entries := []struct {
		kind  SecretKind
		value *string
		limit int
	}{
		{SecretPassword, changes.Password, maxSecretBytes},
		{SecretPrivateKey, changes.PrivateKey, maxKeyBytes},
		{SecretPassphrase, changes.Passphrase, maxSecretBytes},
	}
	var dek []byte
	defer func() { vault.Wipe(dek) }()
	for _, e := range entries {
		switch {
		case e.value == nil:
		case *e.value == "":
			p.remove = append(p.remove, e.kind)
		case len(*e.value) > e.limit:
			return plan{}, ErrSecretTooLarge
		default:
			if dek == nil {
				key, err := s.keys.Key(session)
				if errors.Is(err, vault.ErrLocked) {
					return plan{}, ErrLocked
				}
				if err != nil {
					return plan{}, err
				}
				dek = key
			}
			sealed, err := vault.SealSecret(s.random, dek, vault.SecretRef{Site: siteID.String(), Kind: string(e.kind), DEKVersion: defaultDEKLevel}, []byte(*e.value))
			if err != nil {
				return plan{}, fmt.Errorf("sealing %s: %w", e.kind, err)
			}
			p.set = append(p.set, Sealed{Kind: e.kind, DEKVersion: defaultDEKLevel, Data: sealed})
		}
	}
	return p, nil
}

// Create saves a new site and its secrets.
func (s *Service) Create(ctx context.Context, actor, workspaceID uuid.UUID, session string, in Input, changes SecretChanges) (Site, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return Site{}, err
	}
	in, err := normalize(in)
	if err != nil {
		return Site{}, err
	}
	if err := s.guard.CheckHost(ctx, in.Host); err != nil {
		return Site{}, err
	}
	id, err := s.newID()
	if err != nil {
		return Site{}, fmt.Errorf("generating site id: %w", err)
	}
	p, err := s.sealChanges(session, id, changes)
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

// build assembles a site from validated input.
func build(in Input, id, workspaceID uuid.UUID, createdAt, updatedAt time.Time) Site {
	return Site{
		ID: id, WorkspaceID: workspaceID, Name: in.Name, Protocol: in.Protocol, Host: in.Host, Port: in.Port,
		Username: in.Username, AuthMethod: in.AuthMethod, ListTimezone: in.ListTimezone, CreatedAt: createdAt, UpdatedAt: updatedAt,
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

// Update replaces the settings of a site and applies the secret changes.
func (s *Service) Update(ctx context.Context, actor, workspaceID, id uuid.UUID, session string, in Input, changes SecretChanges) (Site, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return Site{}, err
	}
	in, err := normalize(in)
	if err != nil {
		return Site{}, err
	}
	if err := s.guard.CheckHost(ctx, in.Host); err != nil {
		return Site{}, err
	}
	existing, err := s.store.Site(ctx, workspaceID, id)
	if err != nil {
		return Site{}, err
	}
	p, err := s.sealChanges(session, id, changes)
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
	dek, err := s.keys.Key(session)
	if errors.Is(err, vault.ErrLocked) {
		return nil, ErrLocked
	}
	if err != nil {
		return nil, err
	}
	defer vault.Wipe(dek)
	return vault.OpenSecret(dek, vault.SecretRef{Site: id.String(), Kind: string(kind), DEKVersion: sealed.DEKVersion}, sealed.Data)
}
