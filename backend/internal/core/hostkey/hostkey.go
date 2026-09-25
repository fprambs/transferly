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
	// Trusted means the key equals the trusted key of its type.
	Trusted
	// Changed means the host presented a different key than the trusted one of its type. The connection must be refused.
	Changed
)

// Errors returned by the package.
var (
	// ErrNotFound means no such key is trusted.
	ErrNotFound = errors.New("host key not found")
	// ErrChanged means a different key is already trusted for the host; it must be forgotten first.
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
	// Add stores a key. A stored key of the same host, port and type is left as it is.
	Add(ctx context.Context, workspaceID uuid.UUID, k Key) error
	// Delete removes a key, or returns [ErrNotFound].
	Delete(ctx context.Context, workspaceID uuid.UUID, host string, port int, keyType string) error
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

// Check compares a key presented by a server with the trusted ones. It needs no user: it runs while connecting.
// Only a key of the same type is compared, since connections restrict the accepted algorithms to the trusted ones.
func (s *Service) Check(ctx context.Context, workspaceID uuid.UUID, host string, port int, keyType, fingerprint string) (Verdict, error) {
	keys, err := s.store.Trusted(ctx, workspaceID, host, port)
	if err != nil {
		return Unknown, err
	}
	for _, k := range keys {
		if k.Type != keyType {
			continue
		}
		if k.Fingerprint == fingerprint {
			return Trusted, nil
		}
		return Changed, nil
	}
	return Unknown, nil
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

// Trust records the key shown to the user as trusted. Trusting the key that is already trusted succeeds;
// a different key of the same type is [ErrChanged] and must be forgotten first.
func (s *Service) Trust(ctx context.Context, actor, workspaceID uuid.UUID, host string, port int, keyType, fingerprint string) (Key, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return Key{}, err
	}
	if err := validate(host, port, keyType, fingerprint); err != nil {
		return Key{}, err
	}
	verdict, err := s.Check(ctx, workspaceID, host, port, keyType, fingerprint)
	if err != nil {
		return Key{}, err
	}
	if verdict == Changed {
		return Key{}, ErrChanged
	}
	key := Key{Host: host, Port: port, Type: keyType, Fingerprint: fingerprint, FirstSeen: s.clock.Now().UTC(), AcceptedBy: actor}
	if verdict == Trusted {
		return key, nil
	}
	if err := s.store.Add(ctx, workspaceID, key); err != nil {
		return Key{}, err
	}
	return key, nil
}

// Forget removes a trusted key, so the next connection asks again.
func (s *Service) Forget(ctx context.Context, actor, workspaceID uuid.UUID, host string, port int, keyType string) error {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return err
	}
	return s.store.Delete(ctx, workspaceID, host, port, keyType)
}

// List returns the keys trusted in a workspace the actor may browse.
func (s *Service) List(ctx context.Context, actor, workspaceID uuid.UUID) ([]Key, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionBrowse); err != nil {
		return nil, err
	}
	return s.store.List(ctx, workspaceID)
}
