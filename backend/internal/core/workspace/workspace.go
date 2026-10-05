// Package workspace implements workspaces, memberships and the permission matrix.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/ports"
)

// Kind tells whether a workspace belongs to one person or to a team.
type Kind string

const (
	// KindPersonal is the private workspace every user has.
	KindPersonal Kind = "personal"
	// KindTeam is a workspace shared by several members.
	KindTeam Kind = "team"
)

// Role is what a member may do inside a workspace.
type Role string

const (
	// RoleOwner has every permission and can delete the workspace.
	RoleOwner Role = "owner"
	// RoleAdmin manages members, limits and the audit log, and everything an editor can do.
	RoleAdmin Role = "admin"
	// RoleEditor creates and edits sites and changes remote files.
	RoleEditor Role = "editor"
	// RoleViewer browses and downloads only.
	RoleViewer Role = "viewer"
)

// Action is something a member may try to do in a workspace.
type Action string

const (
	// ActionBrowse lists and inspects remote files.
	ActionBrowse Action = "browse"
	// ActionDownload downloads remote files.
	ActionDownload Action = "download"
	// ActionUpload uploads files.
	ActionUpload Action = "upload"
	// ActionModify renames, deletes and changes permissions of remote files.
	ActionModify Action = "modify"
	// ActionManageSites creates and edits sites and their secrets.
	ActionManageSites Action = "manage_sites"
	// ActionElevate turns on what lets a site act beyond its account on the server, such as sudo (ADR-0027): only admins may.
	ActionElevate Action = "elevate"
	// ActionManageMembers manages members, limits and reads the audit log.
	ActionManageMembers Action = "manage_members"
	// ActionExportSecrets exports the sites with their secrets in clear text inside an encrypted file (ADR-0017). It is kept above
	// managing the sites, since editors use the secrets to connect but never read them otherwise.
	ActionExportSecrets Action = "export_secrets"
	// ActionManageWorkspace deletes the workspace or transfers its ownership.
	ActionManageWorkspace Action = "manage_workspace"
)

// permissions is the role by action matrix: each action lists the roles that may perform it.
var permissions = map[Action][]Role{
	ActionBrowse:          {RoleViewer, RoleEditor, RoleAdmin, RoleOwner},
	ActionDownload:        {RoleViewer, RoleEditor, RoleAdmin, RoleOwner},
	ActionUpload:          {RoleEditor, RoleAdmin, RoleOwner},
	ActionModify:          {RoleEditor, RoleAdmin, RoleOwner},
	ActionManageSites:     {RoleEditor, RoleAdmin, RoleOwner},
	ActionElevate:         {RoleAdmin, RoleOwner},
	ActionManageMembers:   {RoleAdmin, RoleOwner},
	ActionExportSecrets:   {RoleAdmin, RoleOwner},
	ActionManageWorkspace: {RoleOwner},
}

// Can reports whether a role may perform an action. Unknown roles and actions are denied.
func Can(role Role, action Action) bool {
	for _, allowed := range permissions[action] {
		if allowed == role {
			return true
		}
	}
	return false
}

// Errors returned by the package.
var (
	// ErrNotFound means the workspace does not exist or the user is not a member of it.
	// The two cases are indistinguishable on purpose, so workspaces cannot be probed.
	ErrNotFound = errors.New("workspace not found")
	// ErrForbidden means the user is a member but their role does not allow the action.
	ErrForbidden = errors.New("action not allowed for this role")
	// ErrExists means the user already has a personal workspace.
	ErrExists = errors.New("personal workspace already exists")
	// ErrInvalidName means a workspace name is empty, too long or holds control characters.
	ErrInvalidName = errors.New("invalid workspace name")
	// ErrPersonal means the action only makes sense in a team workspace.
	ErrPersonal = errors.New("personal workspaces have no other members")
	// ErrMemberNotFound means the user is not a member of the workspace.
	ErrMemberNotFound = errors.New("member not found")
	// ErrInvalidRole means a role cannot be granted: it is unknown, it is owner, or it is not below the role of the caller.
	ErrInvalidRole = errors.New("invalid role")
	// ErrAlreadyMember means the user already belongs to the workspace.
	ErrAlreadyMember = errors.New("already a member")
	// ErrOwnerBound means the owner cannot be changed, removed or leave: ownership must be transferred first.
	ErrOwnerBound = errors.New("the owner cannot be changed, removed or leave")
)

// Workspace is a space that groups sites, credentials and transfers.
type Workspace struct {
	// ID identifies the workspace.
	ID uuid.UUID
	// Name is the display name.
	Name string
	// Kind tells whether it is personal or a team workspace.
	Kind Kind
	// OwnerID is the user who owns it.
	OwnerID uuid.UUID
	// CreatedAt is when it was created, in UTC.
	CreatedAt time.Time
}

// Summary is a workspace together with the role of the asking user in it.
type Summary struct {
	Workspace
	// Role is the asking user's role.
	Role Role
}

// Store persists workspaces and memberships.
type Store interface {
	// CreateWorkspace stores w and the owner membership of w.OwnerID atomically.
	// It returns [ErrExists] when a personal workspace of that owner already exists.
	CreateWorkspace(ctx context.Context, w Workspace) error
	// PersonalWorkspace returns the personal workspace of a user, or [ErrNotFound].
	PersonalWorkspace(ctx context.Context, userID uuid.UUID) (Workspace, error)
	// WorkspacesForUser lists the workspaces a user is a member of, with their role.
	WorkspacesForUser(ctx context.Context, userID uuid.UUID) ([]Summary, error)
	// RoleOf returns the role of a user in a workspace, or [ErrNotFound] when they are not a member.
	RoleOf(ctx context.Context, workspaceID, userID uuid.UUID) (Role, error)
	// Workspace returns a workspace, or [ErrNotFound].
	Workspace(ctx context.Context, workspaceID uuid.UUID) (Workspace, error)
	// Members lists the members of a workspace, the owner first.
	Members(ctx context.Context, workspaceID uuid.UUID) ([]Member, error)
	// AddMember stores a membership, or returns [ErrAlreadyMember].
	AddMember(ctx context.Context, workspaceID, userID uuid.UUID, role Role) error
	// SetRole changes the role of a member, or returns [ErrMemberNotFound].
	SetRole(ctx context.Context, workspaceID, userID uuid.UUID, role Role) error
	// RemoveMember deletes a membership, or returns [ErrMemberNotFound].
	RemoveMember(ctx context.Context, workspaceID, userID uuid.UUID) error
	// TransferOwnership makes to the owner and from an admin in one step, or returns [ErrMemberNotFound].
	TransferOwnership(ctx context.Context, workspaceID, from, to uuid.UUID) error
}

// Service implements workspace use cases.
type Service struct {
	store Store
	clock ports.Clock
	newID func() (uuid.UUID, error)
}

// NewService returns a service over store. newID may be nil to use UUIDv7.
func NewService(store Store, clock ports.Clock, newID func() (uuid.UUID, error)) *Service {
	if newID == nil {
		newID = uuid.NewV7
	}
	return &Service{store: store, clock: clock, newID: newID}
}

// EnsurePersonal returns the personal workspace of a user, creating it when missing.
func (s *Service) EnsurePersonal(ctx context.Context, userID uuid.UUID) (Workspace, error) {
	existing, err := s.store.PersonalWorkspace(ctx, userID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Workspace{}, err
	}

	id, err := s.newID()
	if err != nil {
		return Workspace{}, fmt.Errorf("generating workspace id: %w", err)
	}
	created := Workspace{ID: id, Name: "Personal", Kind: KindPersonal, OwnerID: userID, CreatedAt: s.clock.Now()}
	err = s.store.CreateWorkspace(ctx, created)
	if errors.Is(err, ErrExists) {
		return s.store.PersonalWorkspace(ctx, userID)
	}
	if err != nil {
		return Workspace{}, err
	}
	return created, nil
}

// List returns the workspaces a user belongs to.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Summary, error) {
	return s.store.WorkspacesForUser(ctx, userID)
}

// Authorize checks that a user may perform an action in a workspace and returns their role.
// Non-members get [ErrNotFound]; members without permission get [ErrForbidden].
func (s *Service) Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action Action) (Role, error) {
	role, err := s.store.RoleOf(ctx, workspaceID, userID)
	if err != nil {
		return "", err
	}
	if !Can(role, action) {
		return "", ErrForbidden
	}
	return role, nil
}
