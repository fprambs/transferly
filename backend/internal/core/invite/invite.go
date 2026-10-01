// Package invite implements invitations to team workspaces.
//
// An invitation is bound to an email address, a role and a workspace. Its token is random, is shown once, is stored only as a
// hash and works once. Every way an acceptance can fail looks the same to the caller, so tokens and accounts cannot be probed.
package invite

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/workspace"
)

const (
	// DefaultTTL is how long an invitation stays valid.
	DefaultTTL = 7 * 24 * time.Hour
	// MaxPending is how many invitations a workspace may have waiting.
	MaxPending = 50
	tokenBytes = 32
)

// Errors returned by the package.
var (
	// ErrNotFound means the invitation does not exist, was used, revoked or expired, or belongs to another email address.
	ErrNotFound = errors.New("invitation not found")
	// ErrTooMany means the workspace already has [MaxPending] invitations waiting.
	ErrTooMany = errors.New("too many pending invitations")
)

// Invitation is an offer to join a team workspace.
type Invitation struct {
	// ID identifies the invitation.
	ID uuid.UUID
	// WorkspaceID is the workspace it gives access to.
	WorkspaceID uuid.UUID
	// WorkspaceName is the display name of that workspace.
	WorkspaceName string
	// Email is the normalized address it is meant for.
	Email string
	// Role is the role the invitee receives.
	Role workspace.Role
	// InvitedBy is the member who created it, or the nil identifier when they were deleted.
	InvitedBy uuid.UUID
	// InvitedByEmail is the address of that member, empty when they were deleted.
	InvitedByEmail string
	// CreatedAt is when it was created, in UTC.
	CreatedAt time.Time
	// ExpiresAt is when it stops working, in UTC.
	ExpiresAt time.Time
}

// Preview is what the holder of a token may learn before accepting.
type Preview struct {
	Invitation
	// AccountExists tells whether the invited address already has an account.
	AccountExists bool
}

// Store persists invitations.
type Store interface {
	// Create stores an invitation, replacing the pending one for the same workspace and email, and keeps only the hash of the token.
	Create(ctx context.Context, i Invitation, tokenHash []byte) error
	// ByTokenHash returns the unexpired invitation with that token hash, or [ErrNotFound].
	ByTokenHash(ctx context.Context, hash []byte, now time.Time) (Invitation, error)
	// Pending lists the unexpired invitations of a workspace, newest first.
	Pending(ctx context.Context, workspaceID uuid.UUID, now time.Time) ([]Invitation, error)
	// CountPending counts the unexpired invitations of a workspace.
	CountPending(ctx context.Context, workspaceID uuid.UUID, now time.Time) (int, error)
	// Revoke deletes an invitation of a workspace, or returns [ErrNotFound].
	Revoke(ctx context.Context, workspaceID, id uuid.UUID) error
	// Accept deletes an unexpired invitation and makes the user a member with its role, in one step. A user who is already a member
	// keeps their role. It returns [ErrNotFound] when the invitation is gone, used or expired.
	Accept(ctx context.Context, id, userID uuid.UUID, now time.Time) (workspace.Summary, error)
	// PurgeExpired deletes the invitations that expired and returns how many.
	PurgeExpired(ctx context.Context, now time.Time) (int, error)
}

// Accounts looks accounts up and creates them.
type Accounts interface {
	// UserByEmail returns the account with a normalized email, or [auth.ErrNotFound].
	UserByEmail(ctx context.Context, email string) (auth.User, error)
}

// Registrar creates accounts.
type Registrar interface {
	// Register creates a regular account.
	Register(ctx context.Context, email, password string) (auth.User, error)
}

// Authorizer decides who may invite and lists the members of a workspace.
type Authorizer interface {
	// Invitable checks that the actor may invite people with a role and returns the workspace.
	Invitable(ctx context.Context, actor, workspaceID uuid.UUID, role workspace.Role) (workspace.Workspace, error)
	// Authorize checks that the actor may perform an action in the workspace.
	Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action workspace.Action) (workspace.Role, error)
	// Members lists the members of a team workspace.
	Members(ctx context.Context, actor, workspaceID uuid.UUID) ([]workspace.Member, error)
}

// Service implements the invitation use cases.
type Service struct {
	store      Store
	authz      Authorizer
	accounts   Accounts
	registrar  Registrar
	clock      ports.Clock
	random     io.Reader
	ttl        time.Duration
	newID      func() (uuid.UUID, error)
	maxPending int
	quota      Quota
}

// Quota decides whether a team may take one more person.
type Quota interface {
	// CheckMember returns an error when the members of the team and its waiting invitations already reach its quota, such as
	// [limits.ErrMemberQuota].
	CheckMember(ctx context.Context, workspaceID uuid.UUID) error
}

// UseQuota makes the service ask a quota before it creates an invitation. Without one nothing is refused. Call it before the service is used.
func (s *Service) UseQuota(q Quota) { s.quota = q }

// NewService returns a service. newID may be nil to use UUIDv7 and ttl zero to use [DefaultTTL].
func NewService(store Store, authz Authorizer, accounts Accounts, registrar Registrar, clock ports.Clock, random io.Reader, ttl time.Duration, newID func() (uuid.UUID, error)) *Service {
	if newID == nil {
		newID = uuid.NewV7
	}
	if ttl == 0 {
		ttl = DefaultTTL
	}
	return &Service{store: store, authz: authz, accounts: accounts, registrar: registrar, clock: clock, random: random, ttl: ttl, newID: newID, maxPending: MaxPending}
}

// hash returns the stored form of a token.
func hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// Create invites an email address to a team workspace and returns the invitation with its token, which is not kept anywhere.
func (s *Service) Create(ctx context.Context, actor, workspaceID uuid.UUID, email string, role workspace.Role) (Invitation, string, error) {
	w, err := s.authz.Invitable(ctx, actor, workspaceID, role)
	if err != nil {
		return Invitation{}, "", err
	}
	email, err = auth.NormalizeEmail(email)
	if err != nil {
		return Invitation{}, "", err
	}
	members, err := s.authz.Members(ctx, actor, workspaceID)
	if err != nil {
		return Invitation{}, "", err
	}
	for _, m := range members {
		if m.Email == email {
			return Invitation{}, "", workspace.ErrAlreadyMember
		}
	}
	if s.quota != nil {
		if err := s.quota.CheckMember(ctx, workspaceID); err != nil {
			return Invitation{}, "", err
		}
	}
	now := s.clock.Now()
	pending, err := s.store.CountPending(ctx, workspaceID, now)
	if err != nil {
		return Invitation{}, "", err
	}
	if pending >= s.maxPending {
		return Invitation{}, "", ErrTooMany
	}
	id, err := s.newID()
	if err != nil {
		return Invitation{}, "", fmt.Errorf("generating invitation id: %w", err)
	}
	raw := make([]byte, tokenBytes)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return Invitation{}, "", fmt.Errorf("generating invitation token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	created := Invitation{ID: id, WorkspaceID: workspaceID, WorkspaceName: w.Name, Email: email, Role: role, InvitedBy: actor, CreatedAt: now, ExpiresAt: now.Add(s.ttl)}
	if err := s.store.Create(ctx, created, hash(token)); err != nil {
		return Invitation{}, "", err
	}
	return created, token, nil
}

// List returns the invitations waiting in a workspace. It needs the permission to manage members.
func (s *Service) List(ctx context.Context, actor, workspaceID uuid.UUID) ([]Invitation, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageMembers); err != nil {
		return nil, err
	}
	return s.store.Pending(ctx, workspaceID, s.clock.Now())
}

// Revoke cancels an invitation. It needs the permission to manage members.
func (s *Service) Revoke(ctx context.Context, actor, workspaceID, id uuid.UUID) error {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageMembers); err != nil {
		return err
	}
	return s.store.Revoke(ctx, workspaceID, id)
}

// Preview tells the holder of a token what it offers and whether the invited address has an account.
func (s *Service) Preview(ctx context.Context, token string) (Preview, error) {
	found, err := s.store.ByTokenHash(ctx, hash(token), s.clock.Now())
	if err != nil {
		return Preview{}, err
	}
	_, err = s.accounts.UserByEmail(ctx, found.Email)
	if err != nil && !errors.Is(err, auth.ErrNotFound) {
		return Preview{}, err
	}
	return Preview{Invitation: found, AccountExists: err == nil}, nil
}

// Accept makes a signed-in user a member. The account must have the invited email address.
func (s *Service) Accept(ctx context.Context, user auth.User, token string) (workspace.Summary, error) {
	now := s.clock.Now()
	found, err := s.store.ByTokenHash(ctx, hash(token), now)
	if err != nil {
		return workspace.Summary{}, err
	}
	if found.Email != user.Email {
		return workspace.Summary{}, ErrNotFound
	}
	return s.store.Accept(ctx, found.ID, user.ID, now)
}

// Join creates the account an invitation is meant for and makes it a member. Unlike sign-up elsewhere it is possible only with
// a valid invitation, and only for the invited address. An address that already has an account gets [auth.ErrEmailTaken].
func (s *Service) Join(ctx context.Context, token, password string) (auth.User, workspace.Summary, error) {
	found, err := s.store.ByTokenHash(ctx, hash(token), s.clock.Now())
	if err != nil {
		return auth.User{}, workspace.Summary{}, err
	}
	user, err := s.registrar.Register(ctx, found.Email, password)
	if err != nil {
		return auth.User{}, workspace.Summary{}, err
	}
	w, err := s.Accept(ctx, user, token)
	if err != nil {
		return auth.User{}, workspace.Summary{}, err
	}
	return user, w, nil
}

// PurgeExpired deletes the invitations that expired.
func (s *Service) PurgeExpired(ctx context.Context) (int, error) {
	return s.store.PurgeExpired(ctx, s.clock.Now())
}
