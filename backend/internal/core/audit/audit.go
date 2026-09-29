// Package audit keeps the trail of what changed in a workspace: who joined, left or was removed, which roles changed, which invitations
// were made, which sites and keys changed. Events name the people and things involved, never credentials, keys or file contents, and
// they can be read only by members who manage the workspace.
package audit

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// Actions recorded in the trail. Each is `group.what`, so a group can be filtered by its prefix.
const (
	// TeamCreated is a team workspace that was created.
	TeamCreated = "workspace.created"
	// OwnershipTransferred is a workspace handed to another member; the target is their email.
	OwnershipTransferred = "workspace.ownership_transferred"
	// MemberJoined is a person who joined through an invitation; the role is in the metadata.
	MemberJoined = "member.joined"
	// MemberRoleChanged is a member whose role changed; the new role is in the metadata.
	MemberRoleChanged = "member.role_changed"
	// MemberRemoved is a member removed by an admin.
	MemberRemoved = "member.removed"
	// MemberLeft is a member who left.
	MemberLeft = "member.left"
	// InvitationCreated is an invitation; the role is in the metadata.
	InvitationCreated = "invitation.created"
	// InvitationRevoked is an invitation that was canceled.
	InvitationRevoked = "invitation.revoked"
	// KeyRotated is a new version of the workspace key; the version is in the metadata.
	KeyRotated = "key.rotated"
	// SiteCreated is a saved site that was created; the target is its name.
	SiteCreated = "site.created"
	// SiteUpdated is a saved site that was changed.
	SiteUpdated = "site.updated"
	// SiteDeleted is a saved site that was deleted.
	SiteDeleted = "site.deleted"
	// SiteCredentialProvided is a password typed for a site that asks for it when connecting; the value is never recorded.
	SiteCredentialProvided = "site.credential_provided"
	// SitesImported is a FileZilla site manager that was imported; the number of sites is in the metadata.
	SitesImported = "sites.imported"
	// SitesExported is an encrypted export of the sites and their secrets (ADR-0017); the number of sites is in the metadata.
	SitesExported = "sites.exported"
	// HostKeyTrusted is a server key that was trusted; the target is host:port.
	HostKeyTrusted = "host_key.trusted"
	// HostKeyForgotten is a server key that was forgotten.
	HostKeyForgotten = "host_key.forgotten"
	// BookmarkCreated is a folder of a saved site that was bookmarked; the target is the bookmark's name, the site and path are in the
	// metadata.
	BookmarkCreated = "bookmark.created"
	// BookmarkUpdated is a bookmark that was renamed or pointed at another folder.
	BookmarkUpdated = "bookmark.updated"
	// BookmarkDeleted is a bookmark that was removed.
	BookmarkDeleted = "bookmark.deleted"
	// AdminUserDisabled is an account an administrator of the instance disabled; the target is its email. It belongs to no workspace.
	AdminUserDisabled = "admin.user_disabled"
	// AdminUserEnabled is an account an administrator of the instance enabled again.
	AdminUserEnabled = "admin.user_enabled"
	// AdminRoleChanged is an account whose role in the instance changed; the new role is in the metadata.
	AdminRoleChanged = "admin.role_changed"
	// AdminSecondFactorReset is an account whose second factor an administrator removed.
	AdminSecondFactorReset = "admin.second_factor_reset"
	// AdminNetworksChanged is a change of the private networks sites may reach; the number of networks is in the metadata.
	AdminNetworksChanged = "admin.networks_changed"
	// AdminLimitsChanged is a change of the caps and quotas of the instance.
	AdminLimitsChanged = "admin.limits_changed"
)

const (
	defaultLimit = 100
	maxLimit     = 500
)

// ErrForbidden means the account that asked for the trail of the instance is not an administrator.
var ErrForbidden = errors.New("administrator required")

// ErrInvalidFilter means the action filter is not a group or an action of the trail.
var ErrInvalidFilter = errors.New("invalid audit filter")

// filterPattern matches the filters a list accepts: lower case words joined by dots and underscores.
var filterPattern = regexp.MustCompile(`^[a-z_.]{0,40}$`)

// Event is one line of the trail.
type Event struct {
	// ID orders the events; larger is newer.
	ID int64
	// WorkspaceID is the workspace it happened in.
	WorkspaceID uuid.UUID
	// ActorID is who did it, or the nil identifier once that account was deleted.
	ActorID uuid.UUID
	// ActorEmail is the address of the actor, empty once the account was deleted.
	ActorEmail string
	// Action says what happened, one of the constants of the package.
	Action string
	// Target is what it happened to: an email address, a site name, a host.
	Target string
	// IP is the client address of the request that did it.
	IP string
	// Meta adds short facts such as a role or a count.
	Meta map[string]string
	// At is when it happened, in UTC.
	At time.Time
}

// Filter narrows a list.
type Filter struct {
	// Action keeps the events whose action starts with it, such as `member.` or `site.created`; empty keeps all.
	Action string
	// Before keeps the events older than this identifier, to read the next page; zero starts from the newest.
	Before int64
}

// Store persists the trail.
type Store interface {
	// InsertAudit stores an event.
	InsertAudit(ctx context.Context, e Event) error
	// AuditByWorkspace returns the events of a workspace, newest first.
	AuditByWorkspace(ctx context.Context, workspaceID uuid.UUID, filter Filter, limit int) ([]Event, error)
	// AuditInstance returns the events of the administrators of the instance, which belong to no workspace, newest first.
	AuditInstance(ctx context.Context, filter Filter, limit int) ([]Event, error)
}

// Authorizer decides who may read the trail.
type Authorizer interface {
	// Authorize returns the role of the user, or a workspace error when they may not.
	Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action workspace.Action) (workspace.Role, error)
}

// Service records and lists the trail.
type Service struct {
	store Store
	authz Authorizer
	clock ports.Clock
}

// NewService returns a service.
func NewService(store Store, authz Authorizer, clock ports.Clock) *Service {
	return &Service{store: store, authz: authz, clock: clock}
}

// Record adds an event stamped with the current time. Failing to record is returned but never worth failing what happened.
func (s *Service) Record(ctx context.Context, e Event) error {
	e.At = s.clock.Now().UTC()
	return s.store.InsertAudit(ctx, e)
}

// List returns the newest events of a workspace, at most limit (a default when not positive, never more than the maximum). It needs the
// permission to manage members.
func (s *Service) List(ctx context.Context, actor, workspaceID uuid.UUID, filter Filter, limit int) ([]Event, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageMembers); err != nil {
		return nil, err
	}
	if !filterPattern.MatchString(filter.Action) || filter.Before < 0 {
		return nil, ErrInvalidFilter
	}
	return s.store.AuditByWorkspace(ctx, workspaceID, filter, pageSize(limit))
}

// pageSize applies the default and the maximum to the number of events asked for.
func pageSize(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	return min(limit, maxLimit)
}

// ListInstance returns the newest events of the administrators of the instance, with the same filter and limit rules as [Service.List]. Only
// an active administrator may read them.
func (s *Service) ListInstance(ctx context.Context, actor auth.User, filter Filter, limit int) ([]Event, error) {
	if actor.Role != auth.RoleAdmin || actor.Disabled {
		return nil, ErrForbidden
	}
	if !filterPattern.MatchString(filter.Action) || filter.Before < 0 {
		return nil, ErrInvalidFilter
	}
	return s.store.AuditInstance(ctx, filter, pageSize(limit))
}
