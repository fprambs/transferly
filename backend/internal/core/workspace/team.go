package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// maxNameLength is the longest workspace name, in characters.
const maxNameLength = 80

// rank orders the roles: a member may act only on members ranked below them.
var rank = map[Role]int{RoleViewer: 1, RoleEditor: 2, RoleAdmin: 3, RoleOwner: 4}

// Member is a user of a workspace together with their role.
type Member struct {
	// UserID identifies the user.
	UserID uuid.UUID
	// Email is the address of the account.
	Email string
	// Role is the role in the workspace.
	Role Role
	// JoinedAt is when the membership began, in UTC.
	JoinedAt time.Time
}

// cleanName validates and trims a workspace name.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", ErrInvalidName
	}
	return name, nil
}

// CreateTeam creates a team workspace owned by the actor.
func (s *Service) CreateTeam(ctx context.Context, actor uuid.UUID, name string) (Workspace, error) {
	name, err := cleanName(name)
	if err != nil {
		return Workspace{}, err
	}
	id, err := s.newID()
	if err != nil {
		return Workspace{}, fmt.Errorf("generating workspace id: %w", err)
	}
	created := Workspace{ID: id, Name: name, Kind: KindTeam, OwnerID: actor, CreatedAt: s.clock.Now()}
	if err := s.store.CreateWorkspace(ctx, created); err != nil {
		return Workspace{}, err
	}
	return created, nil
}

// team returns the workspace after checking that the actor may perform an action in it and that it is a team.
func (s *Service) team(ctx context.Context, actor, workspaceID uuid.UUID, action Action) (Workspace, Role, error) {
	role, err := s.Authorize(ctx, actor, workspaceID, action)
	if err != nil {
		return Workspace{}, "", err
	}
	w, err := s.store.Workspace(ctx, workspaceID)
	if err != nil {
		return Workspace{}, "", err
	}
	if w.Kind != KindTeam {
		return Workspace{}, "", ErrPersonal
	}
	return w, role, nil
}

// Members lists the members of a team workspace. Any member may read the list.
func (s *Service) Members(ctx context.Context, actor, workspaceID uuid.UUID) ([]Member, error) {
	if _, _, err := s.team(ctx, actor, workspaceID, ActionBrowse); err != nil {
		return nil, err
	}
	return s.store.Members(ctx, workspaceID)
}

// SetRole changes the role of another member. The new role and the current role of the member must both rank below the caller's.
func (s *Service) SetRole(ctx context.Context, actor, workspaceID, userID uuid.UUID, role Role) error {
	_, mine, err := s.team(ctx, actor, workspaceID, ActionManageMembers)
	if err != nil {
		return err
	}
	if rank[role] == 0 || rank[role] >= rank[mine] {
		return ErrInvalidRole
	}
	current, err := s.target(ctx, actor, workspaceID, userID, mine)
	if err != nil {
		return err
	}
	if current == role {
		return nil
	}
	return s.store.SetRole(ctx, workspaceID, userID, role)
}

// target loads the role of the member an action is aimed at and checks that the caller outranks them.
func (s *Service) target(ctx context.Context, actor, workspaceID, userID uuid.UUID, mine Role) (Role, error) {
	current, err := s.store.RoleOf(ctx, workspaceID, userID)
	if errors.Is(err, ErrNotFound) {
		return "", ErrMemberNotFound
	}
	if err != nil {
		return "", err
	}
	if current == RoleOwner {
		return "", ErrOwnerBound
	}
	if userID == actor {
		return "", ErrInvalidRole
	}
	if rank[current] >= rank[mine] {
		return "", ErrForbidden
	}
	return current, nil
}

// RemoveMember removes another member from a team workspace.
func (s *Service) RemoveMember(ctx context.Context, actor, workspaceID, userID uuid.UUID) error {
	_, mine, err := s.team(ctx, actor, workspaceID, ActionManageMembers)
	if err != nil {
		return err
	}
	if _, err := s.target(ctx, actor, workspaceID, userID, mine); err != nil {
		return err
	}
	return s.store.RemoveMember(ctx, workspaceID, userID)
}

// Leave removes the actor from a team workspace. The owner cannot leave without transferring ownership.
func (s *Service) Leave(ctx context.Context, actor, workspaceID uuid.UUID) error {
	_, role, err := s.team(ctx, actor, workspaceID, ActionBrowse)
	if err != nil {
		return err
	}
	if role == RoleOwner {
		return ErrOwnerBound
	}
	return s.store.RemoveMember(ctx, workspaceID, actor)
}

// TransferOwnership hands a team workspace to another member; the previous owner becomes an admin.
func (s *Service) TransferOwnership(ctx context.Context, actor, workspaceID, to uuid.UUID) error {
	if _, _, err := s.team(ctx, actor, workspaceID, ActionManageWorkspace); err != nil {
		return err
	}
	if to == actor {
		return ErrOwnerBound
	}
	if _, err := s.store.RoleOf(ctx, workspaceID, to); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrMemberNotFound
		}
		return err
	}
	return s.store.TransferOwnership(ctx, workspaceID, actor, to)
}
