// Package passkey implements WebAuthn credentials as a second factor of sign-in, on top of github.com/go-webauthn/webauthn. See ADR-0007
// and the guide on the second factor.
package passkey

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/ports"
)

const (
	// MaxPasskeys is how many passkeys an account may have.
	MaxPasskeys = 10
	// CeremonyTTL is how long the browser has to answer a challenge.
	CeremonyTTL = 5 * time.Minute
	maxNameLen  = 60
	// maxCeremonies bounds the challenges kept in memory, so unauthenticated calls cannot grow it without limit.
	maxCeremonies = 10_000
)

// Errors returned by the package.
var (
	// ErrInvalid means the browser's answer does not verify: wrong challenge, origin, signature or a credential of another account.
	ErrInvalid = errors.New("the passkey response does not verify")
	// ErrNoCeremony means the challenge is unknown, used, expired or belongs to another account.
	ErrNoCeremony = errors.New("unknown or expired challenge")
	// ErrNoPasskeys means the account has no passkey to sign in with.
	ErrNoPasskeys = errors.New("no passkeys")
	// ErrNotFound means the account has no such passkey.
	ErrNotFound = errors.New("passkey not found")
	// ErrTooMany means the account already has [MaxPasskeys] passkeys.
	ErrTooMany = errors.New("too many passkeys")
	// ErrInvalidName means the name of a passkey is empty, too long or has control characters.
	ErrInvalidName = errors.New("invalid passkey name")
)

// Passkey is a credential of an account.
type Passkey struct {
	// ID identifies the passkey inside Transferly.
	ID uuid.UUID
	// Name is what the user calls it, such as "Laptop" or "YubiKey".
	Name string
	// CreatedAt is when it was registered, in UTC.
	CreatedAt time.Time
	// LastUsedAt is when it last signed in, or the zero time.
	LastUsedAt time.Time
	// Credential is the WebAuthn credential with its public key and signature counter.
	Credential webauthn.Credential
}

// Store persists passkeys.
type Store interface {
	// Passkeys lists the passkeys of a user, oldest first.
	Passkeys(ctx context.Context, userID uuid.UUID) ([]Passkey, error)
	// AddPasskey stores a passkey.
	AddPasskey(ctx context.Context, userID uuid.UUID, p Passkey) error
	// DeletePasskey removes a passkey of a user, or returns [ErrNotFound].
	DeletePasskey(ctx context.Context, userID, id uuid.UUID) error
	// TouchPasskey stores the credential after a sign-in, with its new counter, and when it was used, or returns [ErrNotFound].
	TouchPasskey(ctx context.Context, userID, id uuid.UUID, credential webauthn.Credential, at time.Time) error
}

// Config identifies the relying party.
type Config struct {
	// RPID is the host name passkeys are bound to, without scheme or port.
	RPID string
	// DisplayName is the name the browser shows.
	DisplayName string
	// Origins are the origins the browser may answer from, with scheme and port.
	Origins []string
}

// Account is the part of a user the ceremonies need.
type Account struct {
	// ID identifies the user.
	ID uuid.UUID
	// Email is the address of the account.
	Email string
}

// user adapts an account and its credentials to the interface of the library.
type user struct {
	account     Account
	credentials []webauthn.Credential
}

// WebAuthnID returns the opaque handle of the user.
func (u user) WebAuthnID() []byte { return u.account.ID[:] }

// WebAuthnName returns the account name.
func (u user) WebAuthnName() string { return u.account.Email }

// WebAuthnDisplayName returns the name shown in the authenticator.
func (u user) WebAuthnDisplayName() string { return u.account.Email }

// WebAuthnCredentials returns the credentials of the user.
func (u user) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// ceremony is a challenge waiting for the answer of a browser.
type ceremony struct {
	user     uuid.UUID
	login    bool
	data     webauthn.SessionData
	deadline time.Time
}

// Service implements the passkey use cases.
type Service struct {
	wa     *webauthn.WebAuthn
	store  Store
	clock  ports.Clock
	random io.Reader
	newID  func() (uuid.UUID, error)

	mu         sync.Mutex
	ceremonies map[string]ceremony
}

// NewService returns a service for the relying party. newID may be nil to use UUIDv7.
func NewService(store Store, cfg Config, clock ports.Clock, random io.Reader, newID func() (uuid.UUID, error)) (*Service, error) {
	wa, err := webauthn.New(&webauthn.Config{RPID: cfg.RPID, RPDisplayName: cfg.DisplayName, RPOrigins: cfg.Origins})
	if err != nil {
		return nil, fmt.Errorf("configuring WebAuthn: %w", err)
	}
	if newID == nil {
		newID = uuid.NewV7
	}
	return &Service{wa: wa, store: store, clock: clock, random: random, newID: newID, ceremonies: map[string]ceremony{}}, nil
}

// remember keeps the data of a ceremony and returns the token the browser sends back with its answer.
func (s *Service) remember(c ceremony) (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", fmt.Errorf("generating the ceremony token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := s.clock.Now()
	c.deadline = now.Add(CeremonyTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, other := range s.ceremonies {
		if !other.deadline.After(now) {
			delete(s.ceremonies, key)
		}
	}
	if len(s.ceremonies) >= maxCeremonies {
		return "", ErrNoCeremony
	}
	s.ceremonies[token] = c
	return token, nil
}

// take removes and returns a ceremony of a user, so a challenge answers once.
func (s *Service) take(token string, userID uuid.UUID, login bool) (webauthn.SessionData, error) {
	s.mu.Lock()
	c, ok := s.ceremonies[token]
	delete(s.ceremonies, token)
	s.mu.Unlock()
	if !ok || c.user != userID || c.login != login || !c.deadline.After(s.clock.Now()) {
		return webauthn.SessionData{}, ErrNoCeremony
	}
	return c.data, nil
}

// load returns the account as the library wants it, with its credentials.
func (s *Service) load(ctx context.Context, account Account) (user, []Passkey, error) {
	list, err := s.store.Passkeys(ctx, account.ID)
	if err != nil {
		return user{}, nil, err
	}
	credentials := make([]webauthn.Credential, 0, len(list))
	for _, p := range list {
		credentials = append(credentials, p.Credential)
	}
	return user{account: account, credentials: credentials}, list, nil
}

// List returns the passkeys of an account.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Passkey, error) {
	return s.store.Passkeys(ctx, userID)
}

// Delete removes a passkey of an account.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return s.store.DeletePasskey(ctx, userID, id)
}

// BeginRegistration starts adding a passkey. The library fails to build options only when the system's source of randomness fails, which
// since Go 1.24 ends the process, so that failure is not handled here. It returns the options for navigator.credentials.create() as JSON and the token to send back
// with the answer. The credentials the account already has are excluded, so the same authenticator is not added twice.
func (s *Service) BeginRegistration(ctx context.Context, account Account) (json.RawMessage, string, error) {
	u, list, err := s.load(ctx, account)
	if err != nil {
		return nil, "", err
	}
	if len(list) >= MaxPasskeys {
		return nil, "", ErrTooMany
	}
	excluded := make([]protocol.CredentialDescriptor, 0, len(u.credentials))
	for _, c := range u.credentials {
		excluded = append(excluded, c.Descriptor())
	}
	creation, data, _ := s.wa.BeginRegistration(u,
		webauthn.WithExclusions(excluded),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey: protocol.ResidentKeyRequirementDiscouraged, UserVerification: protocol.VerificationPreferred,
		}))
	options, _ := json.Marshal(creation)
	token, err := s.remember(ceremony{user: account.ID, data: *data})
	return options, token, err
}

// cleanName validates and trims the name of a passkey.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLen || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", ErrInvalidName
	}
	return name, nil
}

// FinishRegistration verifies the answer of the browser and stores the new passkey. body is the JSON of the credential the browser made.
func (s *Service) FinishRegistration(ctx context.Context, account Account, token, name string, body []byte) (Passkey, error) {
	name, err := cleanName(name)
	if err != nil {
		return Passkey{}, err
	}
	data, err := s.take(token, account.ID, false)
	if err != nil {
		return Passkey{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		return Passkey{}, ErrInvalid
	}
	u, _, err := s.load(ctx, account)
	if err != nil {
		return Passkey{}, err
	}
	credential, err := s.wa.CreateCredential(u, data, parsed)
	if err != nil {
		return Passkey{}, ErrInvalid
	}
	id, err := s.newID()
	if err != nil {
		return Passkey{}, fmt.Errorf("generating the passkey id: %w", err)
	}
	created := Passkey{ID: id, Name: name, CreatedAt: s.clock.Now().UTC(), Credential: *credential}
	if err := s.store.AddPasskey(ctx, account.ID, created); err != nil {
		return Passkey{}, err
	}
	return created, nil
}

// BeginLogin starts the use of a passkey as the second factor. It returns the options for navigator.credentials.get() and the token to
// send back with the answer.
func (s *Service) BeginLogin(ctx context.Context, account Account) (json.RawMessage, string, error) {
	u, list, err := s.load(ctx, account)
	if err != nil {
		return nil, "", err
	}
	if len(list) == 0 {
		return nil, "", ErrNoPasskeys
	}
	assertion, data, _ := s.wa.BeginLogin(u, webauthn.WithUserVerification(protocol.VerificationPreferred))
	options, _ := json.Marshal(assertion)
	token, err := s.remember(ceremony{user: account.ID, login: true, data: *data})
	return options, token, err
}

// FinishLogin verifies the answer of the browser against the passkeys of the account and records the use, so the signature counter
// moves forward and a cloned authenticator can be told apart.
func (s *Service) FinishLogin(ctx context.Context, account Account, token string, body []byte) error {
	data, err := s.take(token, account.ID, true)
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		return ErrInvalid
	}
	u, list, err := s.load(ctx, account)
	if err != nil {
		return err
	}
	credential, err := s.wa.ValidateLogin(u, data, parsed)
	if err != nil {
		return ErrInvalid
	}
	var id uuid.UUID
	for _, p := range list {
		if string(p.Credential.ID) == string(credential.ID) {
			id = p.ID
		}
	}
	return s.store.TouchPasskey(ctx, account.ID, id, *credential, s.clock.Now().UTC())
}
