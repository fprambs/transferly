package site

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"slices"
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
	// minSpeedLimit is the smallest speed limit a site may have, in bytes per second (1 KiB): below it a single chunk of
	// data would keep a request waiting for hours.
	minSpeedLimit = 1024
	// maxSpeedLimit is the largest speed limit a site may have, in bytes per second (1 TiB).
	maxSpeedLimit = 1 << 40
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

// Input holds the settings of a site as given by the user.
type Input struct {
	// Name is the display name.
	Name string
	// Protocol is the transfer protocol; FTPS is read as FTP that requires TLS.
	Protocol Protocol
	// FTPEncryption is how an FTP site protects the connection; empty selects clear text for FTP, the encryption the port
	// implies for FTPS, and nothing for SFTP.
	FTPEncryption FTPEncryption
	// Host is the server name or address.
	Host string
	// Port is the server port; zero selects the protocol's default.
	Port int
	// Username is the account name.
	Username string
	// CollectionID is the collection to keep the site in; the nil identifier keeps it at the top level.
	CollectionID uuid.UUID
	// AuthMethod is how the account authenticates; empty selects password.
	AuthMethod AuthMethod
	// ListTimezone is the IANA zone of listing times; empty selects UTC.
	ListTimezone string
	// Charset is the character set of file names; empty selects UTF-8. Only FTP and FTPS accept another one.
	Charset string
	// SpeedLimit caps the bytes per second the site's transfers move together; zero means no cap of its own.
	SpeedLimit int64
	// Proxy is the SOCKS5 or HTTP proxy the connection goes through; the zero value connects directly.
	Proxy Proxy
	// Jump is the SSH server the connection tunnels through before reaching the site; the zero value connects
	// directly. Only valid for SFTP.
	Jump Jump
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
	// JumpPassword is the password of the jump host.
	JumpPassword *string
	// JumpPrivateKey is the SSH private key of the jump host.
	JumpPrivateKey *string
	// JumpPassphrase is the passphrase of the jump host's private key.
	JumpPassphrase *string
	// ProxyPassword is the password of the SOCKS5 or HTTP proxy.
	ProxyPassword *string
}

// Service implements the site use cases. Secrets are sealed with the key of their workspace: the DEK of the owner for a personal
// workspace and the shared workspace key for a team.
type Service struct {
	store interface {
		Store
		CollectionStore
	}
	authz  Authorizer
	keys   Keys
	guard  Guard
	clock  ports.Clock
	random io.Reader
	newID  func() (uuid.UUID, error)
	quota  Quota
}

// Quota decides whether a workspace may have another site.
type Quota interface {
	// CheckSite returns an error when the workspace already has as many sites as it may, such as [limits.ErrSiteQuota].
	CheckSite(ctx context.Context, workspaceID uuid.UUID) error
}

// UseQuota makes the service ask a quota before it creates a site. Without one nothing is refused. Call it before the service is used.
func (s *Service) UseQuota(q Quota) { s.quota = q }

// NewService returns a service. newID may be nil to use UUIDv7.
func NewService(store interface {
	Store
	CollectionStore
}, authz Authorizer, keys Keys, guard Guard, clock ports.Clock, random io.Reader, newID func() (uuid.UUID, error)) *Service {
	if newID == nil {
		newID = uuid.NewV7
	}
	return &Service{store: store, authz: authz, keys: keys, guard: guard, clock: clock, random: random, newID: newID}
}

// Normalize validates the settings and fills in the defaults: the port of the protocol, password authentication and
// UTC listing times. Text fields are trimmed.
func Normalize(in Input) (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.TrimSpace(in.Host)
	in.Username = strings.TrimSpace(in.Username)
	if in.AuthMethod == AuthAnonymous {
		in.Username = AnonymousUser
	}
	if n := utf8.RuneCountInString(in.Name); n == 0 || n > maxNameRunes {
		return in, ErrInvalidName
	}
	if _, ok := defaultPorts[in.Protocol]; !ok {
		return in, ErrInvalidProtocol
	}
	if !ValidHost(in.Host) {
		return in, ErrInvalidHost
	}
	var err error
	if in.Protocol, in.FTPEncryption, err = normalizeEncryption(in.Protocol, in.Port, in.FTPEncryption); err != nil {
		return in, err
	}
	if in.Port == 0 {
		in.Port = defaultPorts[in.Protocol]
		if in.FTPEncryption == EncryptionImplicit {
			in.Port = implicitPort
		}
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
	if !slices.Contains([]AuthMethod{AuthPassword, AuthAsk, AuthAnonymous}, in.AuthMethod) && (in.AuthMethod != AuthKey || in.Protocol != SFTP) {
		return in, ErrInvalidAuthMethod
	}
	if in.Charset == "" {
		in.Charset = "utf-8"
	}
	in.Charset = strings.ToLower(strings.TrimSpace(in.Charset))
	if !slices.Contains(Charsets, in.Charset) || (in.Protocol == SFTP && in.Charset != "utf-8") {
		return in, ErrInvalidCharset
	}
	if in.ListTimezone == "" {
		in.ListTimezone = "UTC"
	}
	if _, err := time.LoadLocation(in.ListTimezone); err != nil || in.ListTimezone == "Local" {
		return in, ErrInvalidTimezone
	}
	if in.SpeedLimit != 0 && (in.SpeedLimit < minSpeedLimit || in.SpeedLimit > maxSpeedLimit) {
		return in, ErrInvalidSpeedLimit
	}
	proxy, err := normalizeProxy(in.Proxy)
	if err != nil {
		return in, err
	}
	in.Proxy = proxy
	jump, err := normalizeJump(in.Jump, in.Protocol)
	if err != nil {
		return in, err
	}
	in.Jump = jump
	return in, nil
}

// normalizeProxy trims and validates a proxy; the zero value, meaning no proxy, is always valid.
func normalizeProxy(p Proxy) (Proxy, error) {
	p.Host = strings.TrimSpace(p.Host)
	p.Username = strings.TrimSpace(p.Username)
	if !p.Set() {
		return Proxy{}, nil
	}
	if p.Kind != ProxySOCKS5 && p.Kind != ProxyHTTP {
		return p, ErrInvalidProxy
	}
	if !ValidHost(p.Host) || p.Port < 1 || p.Port > 65535 {
		return p, ErrInvalidProxy
	}
	return p, nil
}

// normalizeEncryption settles how an FTP site protects the connection. SFTP takes none. The legacy FTPS protocol becomes FTP
// that requires TLS: implicit on port 990, explicit on any other, as before encryptions could be chosen. FTP without one keeps
// clear text, as it always meant.
func normalizeEncryption(protocol Protocol, port int, encryption FTPEncryption) (Protocol, FTPEncryption, error) {
	switch {
	case protocol == SFTP && encryption == "":
		return SFTP, "", nil
	case protocol == SFTP:
		return protocol, encryption, ErrInvalidEncryption
	case protocol == FTPS && encryption == "" && port == implicitPort:
		return FTP, EncryptionImplicit, nil
	case protocol == FTPS && encryption == "":
		return FTP, EncryptionExplicit, nil
	case protocol == FTPS && encryption != EncryptionExplicit && encryption != EncryptionImplicit:
		return protocol, encryption, ErrInvalidEncryption
	case encryption == "":
		return FTP, EncryptionPlain, nil
	}
	switch encryption {
	case EncryptionOptional, EncryptionExplicit, EncryptionImplicit, EncryptionPlain:
		return FTP, encryption, nil
	}
	return protocol, encryption, ErrInvalidEncryption
}

// normalizeJump trims and validates a jump host; the zero value, meaning no jump host, is always valid. A jump host
// is only valid for SFTP, since FTP and FTPS have no equivalent of an SSH tunnel to send it through.
func normalizeJump(j Jump, protocol Protocol) (Jump, error) {
	j.Host = strings.TrimSpace(j.Host)
	j.Username = strings.TrimSpace(j.Username)
	if !j.Set() {
		return Jump{}, nil
	}
	if protocol != SFTP || !ValidHost(j.Host) {
		return j, ErrInvalidJump
	}
	if j.Port == 0 {
		j.Port = defaultPorts[SFTP]
	}
	if j.Port < 1 || j.Port > 65535 {
		return j, ErrInvalidJump
	}
	if n := utf8.RuneCountInString(j.Username); n == 0 || n > maxUsernameRunes {
		return j, ErrInvalidJump
	}
	if j.AuthMethod == "" {
		j.AuthMethod = AuthPassword
	}
	if j.AuthMethod != AuthPassword && j.AuthMethod != AuthKey {
		return j, ErrInvalidJump
	}
	return j, nil
}

// ValidHost reports whether host is an IP address or a dotted host name, without scheme, port, path or spaces.
func ValidHost(host string) bool {
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
func (s *Service) sealChanges(ctx context.Context, actor, workspaceID uuid.UUID, session string, siteID uuid.UUID, changes SecretChanges) (plan, error) {
	var p plan
	entries := []struct {
		kind  SecretKind
		value *string
		limit int
	}{
		{SecretPassword, changes.Password, maxSecretBytes},
		{SecretPrivateKey, changes.PrivateKey, maxKeyBytes},
		{SecretPassphrase, changes.Passphrase, maxSecretBytes},
		{SecretJumpPassword, changes.JumpPassword, maxSecretBytes},
		{SecretJumpPrivateKey, changes.JumpPrivateKey, maxKeyBytes},
		{SecretJumpPassphrase, changes.JumpPassphrase, maxSecretBytes},
		{SecretProxyPassword, changes.ProxyPassword, maxSecretBytes},
	}
	var dek []byte
	var version int
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
				key, current, err := s.keys.Current(ctx, actor, workspaceID, session)
				if errors.Is(err, vault.ErrLocked) {
					return plan{}, ErrLocked
				}
				if err != nil {
					return plan{}, err
				}
				dek, version = key, current
			}
			sealed, err := vault.SealSecret(s.random, dek, vault.SecretRef{Site: siteID.String(), Kind: string(e.kind), DEKVersion: version}, []byte(*e.value))
			if err != nil {
				return plan{}, fmt.Errorf("sealing %s: %w", e.kind, err)
			}
			p.set = append(p.set, Sealed{Kind: e.kind, DEKVersion: version, Data: sealed})
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
	in, err := Normalize(in)
	if err != nil {
		return Site{}, err
	}
	changes = withoutStoredPassword(in, changes)
	if err := s.checkHosts(ctx, in); err != nil {
		return Site{}, err
	}
	if err := s.checkCollection(ctx, workspaceID, in.CollectionID); err != nil {
		return Site{}, err
	}
	id, err := s.newID()
	if err != nil {
		return Site{}, fmt.Errorf("generating site id: %w", err)
	}
	p, err := s.sealChanges(ctx, actor, workspaceID, session, id, changes)
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
		ID: id, WorkspaceID: workspaceID, CollectionID: in.CollectionID, Name: in.Name, Protocol: in.Protocol, FTPEncryption: in.FTPEncryption, Host: in.Host,
		Port: in.Port, Username: in.Username, AuthMethod: in.AuthMethod, ListTimezone: in.ListTimezone, Charset: in.Charset, SpeedLimit: in.SpeedLimit,
		Proxy: in.Proxy, Jump: in.Jump, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
}

// checkHosts asks the guard about every literal address the input configures: the site's own host, and its proxy's
// and jump host's, when set.
func (s *Service) checkHosts(ctx context.Context, in Input) error {
	if err := s.guard.CheckHost(ctx, in.Host); err != nil {
		return err
	}
	if in.Proxy.Set() {
		if err := s.guard.CheckHost(ctx, in.Proxy.Host); err != nil {
			return err
		}
	}
	if in.Jump.Set() {
		if err := s.guard.CheckHost(ctx, in.Jump.Host); err != nil {
			return err
		}
	}
	return nil
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
	in, err := Normalize(in)
	if err != nil {
		return Site{}, err
	}
	changes = withoutStoredPassword(in, changes)
	if err := s.checkHosts(ctx, in); err != nil {
		return Site{}, err
	}
	if err := s.checkCollection(ctx, workspaceID, in.CollectionID); err != nil {
		return Site{}, err
	}
	existing, err := s.store.Site(ctx, workspaceID, id)
	if err != nil {
		return Site{}, err
	}
	p, err := s.sealChanges(ctx, actor, workspaceID, session, id, changes)
	if err != nil {
		return Site{}, err
	}
	updated := build(in, id, workspaceID, existing.CreatedAt, s.clock.Now().UTC())
	if err := s.store.UpdateSite(ctx, updated, p.set, p.remove); err != nil {
		return Site{}, err
	}
	return s.store.Site(ctx, workspaceID, id)
}

// withoutStoredPassword makes a site that asks for its password when connecting drop any stored one, whatever the
// request said: its password must never be written anywhere (ADR-0010). An anonymous site drops its password, private
// key and passphrase alike, since it has no secret to use them for.
func withoutStoredPassword(in Input, changes SecretChanges) SecretChanges {
	none := ""
	switch in.AuthMethod {
	case AuthAsk:
		changes.Password = &none
	case AuthAnonymous:
		changes.Password, changes.PrivateKey, changes.Passphrase = &none, &none, &none
	}
	return changes
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
