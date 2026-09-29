// Package hostkey remembers the server keys a workspace has trusted, so a connection is refused
// when a server presents a different key than the one seen the first time (trust on first use).
package hostkey

import (
	"context"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// fingerprintPattern matches the SHA-256 fingerprint format of OpenSSH: "SHA256:" and 43 base64 characters.
var fingerprintPattern = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)

// keyTypePattern matches SSH key algorithm names such as ssh-ed25519 or ecdsa-sha2-nistp256.
var keyTypePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9@.-]{0,63}$`)

// Verdict is the outcome of comparing a presented key with the trusted ones.
type Verdict int

const (
	// Unknown means no key of this type is trusted for the host yet.
	Unknown Verdict = iota
	// Trusted means the key is one of the trusted keys of its type.
	Trusted
	// Changed means the host presented a key of a type that is trusted, but none of the trusted keys of that type. The connection must be
	// refused until the user decides.
	Changed
)

// TrustMode says what trusting a key does to the keys already trusted for the host (4.13b).
type TrustMode string

const (
	// TrustNew trusts a key of a type not trusted yet; a different key of a trusted type is [ErrChanged].
	TrustNew TrustMode = ""
	// TrustAdd trusts the key next to the ones of its type, for a service with several servers behind one name, each with its own key.
	TrustAdd TrustMode = "add"
	// TrustReplace trusts the key in place of the ones of its type, for a server whose key was changed.
	TrustReplace TrustMode = "replace"
)

// Errors returned by the package.
var (
	// ErrNotFound means no such key is trusted.
	ErrNotFound = errors.New("host key not found")
	// ErrChanged means a different key of the type is already trusted for the host; it must be added next to it or replace it.
	ErrChanged = errors.New("a different key is already trusted for this host")
	// ErrInvalid means the host, port, key type or fingerprint is malformed.
	ErrInvalid = errors.New("invalid host key")
)

// Key is a trusted server key.
type Key struct {
	// Host is the server name or address.
	Host string
	// Port is the server port.
	Port int
	// Type is the key algorithm, such as ssh-ed25519.
	Type string
	// Fingerprint is the SHA-256 fingerprint in OpenSSH notation.
	Fingerprint string
	// FirstSeen is when the key was trusted, in UTC.
	FirstSeen time.Time
	// AcceptedBy is the user who trusted it.
	AcceptedBy uuid.UUID
}

// Store persists trusted keys per workspace.
type Store interface {
	// Trusted lists the keys trusted for a host and port.
	Trusted(ctx context.Context, workspaceID uuid.UUID, host string, port int) ([]Key, error)
	// List returns every key trusted in the workspace, by host, port and type.
	List(ctx context.Context, workspaceID uuid.UUID) ([]Key, error)
	// Add stores a key. The same key stored again is left as it is; other keys of its type stay trusted too.
	Add(ctx context.Context, workspaceID uuid.UUID, k Key) error
	// Delete removes the key with a fingerprint, or every key of the type when the fingerprint is empty, or returns [ErrNotFound].
	Delete(ctx context.Context, workspaceID uuid.UUID, host string, port int, keyType, fingerprint string) error
}

// Authorizer checks that a user may perform an action in a workspace.
type Authorizer interface {
	// Authorize returns the role of the user or an error that says why the action is refused.
	Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action workspace.Action) (workspace.Role, error)
}

// Clock tells the current time.
type Clock interface {
	// Now returns the current instant.
	Now() time.Time
}

// Service implements host key verification and management.
type Service struct {
	store Store
	authz Authorizer
	clock Clock
}

// NewService returns a service over the store.
func NewService(store Store, authz Authorizer, clock Clock) *Service {
	return &Service{store: store, authz: authz, clock: clock}
}

// validate checks the identity of a key.
func validate(host string, port int, keyType, fingerprint string) error {
	if !site.ValidHost(host) || port < 1 || port > 65535 || !keyTypePattern.MatchString(keyType) {
		return ErrInvalid
	}
	if !fingerprintPattern.MatchString(fingerprint) {
		return ErrInvalid
	}
	return nil
}

// Fingerprint renders the SHA-256 digest of a public key blob in OpenSSH notation.
func Fingerprint(digest [32]byte) string {
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(digest[:]), "=")
}

// Check compares a key presented by a server with the trusted ones, and returns the fingerprints trusted for its type. It needs no user:
// it runs while connecting. Only keys of the same type are compared, since connections restrict the accepted algorithms to the trusted
// ones; several keys of one type may be trusted (4.13b).
func (s *Service) Check(ctx context.Context, workspaceID uuid.UUID, host string, port int, keyType, fingerprint string) (Verdict, []string, error) {
	keys, err := s.store.Trusted(ctx, workspaceID, host, port)
	if err != nil {
		return Unknown, nil, err
	}
	var trusted []string
	for _, k := range keys {
		if k.Type != keyType {
			continue
		}
		if k.Fingerprint == fingerprint {
			return Trusted, []string{fingerprint}, nil
		}
		trusted = append(trusted, k.Fingerprint)
	}
	if len(trusted) > 0 {
		return Changed, trusted, nil
	}
	return Unknown, nil, nil
}

// Algorithms lists the key types trusted for a host, so a connection can insist on them.
func (s *Service) Algorithms(ctx context.Context, workspaceID uuid.UUID, host string, port int) ([]string, error) {
	keys, err := s.store.Trusted(ctx, workspaceID, host, port)
	if err != nil {
		return nil, err
	}
	types := make([]string, 0, len(keys))
	for _, k := range keys {
		types = append(types, k.Type)
	}
	return types, nil
}

// Trust records the key shown to the user as trusted. Trusting a key that is already trusted succeeds. A different key of a trusted type
// is [ErrChanged] unless the mode says what to do with the keys already trusted: add it next to them, or replace them.
func (s *Service) Trust(ctx context.Context, actor, workspaceID uuid.UUID, host string, port int, keyType, fingerprint string, mode TrustMode) (Key, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return Key{}, err
	}
	if err := validate(host, port, keyType, fingerprint); err != nil || (mode != TrustNew && mode != TrustAdd && mode != TrustReplace) {
		return Key{}, ErrInvalid
	}
	verdict, _, err := s.Check(ctx, workspaceID, host, port, keyType, fingerprint)
	if err != nil {
		return Key{}, err
	}
	key := Key{Host: host, Port: port, Type: keyType, Fingerprint: fingerprint, FirstSeen: s.clock.Now().UTC(), AcceptedBy: actor}
	switch {
	case verdict == Trusted:
		return key, nil
	case verdict == Changed && mode == TrustNew:
		return Key{}, ErrChanged
	case verdict == Changed && mode == TrustReplace:
		if err := s.store.Delete(ctx, workspaceID, host, port, keyType, ""); err != nil {
			return Key{}, err
		}
	}
	if err := s.store.Add(ctx, workspaceID, key); err != nil {
		return Key{}, err
	}
	return key, nil
}

// Forget removes the trusted key with a fingerprint, or every trusted key of the type when the fingerprint is empty, so the next
// connection asks again.
func (s *Service) Forget(ctx context.Context, actor, workspaceID uuid.UUID, host string, port int, keyType, fingerprint string) error {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return err
	}
	return s.store.Delete(ctx, workspaceID, host, port, keyType, fingerprint)
}

// List returns the keys trusted in a workspace the actor may browse.
func (s *Service) List(ctx context.Context, actor, workspaceID uuid.UUID) ([]Key, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionBrowse); err != nil {
		return nil, err
	}
	return s.store.List(ctx, workspaceID)
}
