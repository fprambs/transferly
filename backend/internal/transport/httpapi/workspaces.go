package httpapi

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/audit"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/core/wskey"
)

// workspaceDTO converts a workspace and the caller's role to its public representation.
func workspaceDTO(w workspace.Summary) Workspace {
	return Workspace{Id: w.ID, Name: w.Name, Kind: WorkspaceKind(w.Kind), Role: WorkspaceRole(w.Role), CreatedAt: w.CreatedAt}
}

// ListWorkspaces lists the workspaces the signed-in user belongs to.
func (s *server) ListWorkspaces(ctx context.Context, _ ListWorkspacesRequestObject) (ListWorkspacesResponseObject, error) {
	p, _ := principalFrom(ctx)
	list, err := s.deps.Workspaces.List(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	out := make(ListWorkspaces200JSONResponse, 0, len(list))
	for _, w := range list {
		out = append(out, workspaceDTO(w))
	}
	return out, nil
}

// GetWorkspace returns one workspace. Every role may browse, so membership is enough; workspaces of other
// users answer 404, like missing ones.
func (s *server) GetWorkspace(ctx context.Context, request GetWorkspaceRequestObject) (GetWorkspaceResponseObject, error) {
	p, _ := principalFrom(ctx)
	list, err := s.deps.Workspaces.List(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	for _, w := range list {
		if w.ID != request.WorkspaceId {
			continue
		}
		return GetWorkspace200JSONResponse(workspaceDTO(w)), nil
	}
	return GetWorkspace404ApplicationProblemPlusJSONResponse(problemBody(404, "workspace_not_found", nil)), nil
}

// memberDTO converts a member to its public representation.
func memberDTO(m workspace.Member, sealed bool) Member {
	state := Waiting
	if sealed {
		state = Sealed
	}
	return Member{UserId: m.UserID, Email: m.Email, Role: MemberRole(m.Role), JoinedAt: m.JoinedAt, KeyState: state}
}

// CreateWorkspace creates a team workspace owned by the signed-in user.
func (s *server) CreateWorkspace(ctx context.Context, request CreateWorkspaceRequestObject) (CreateWorkspaceResponseObject, error) {
	p, _ := principalFrom(ctx)
	err := s.deps.Quotas.CheckTeam(ctx, p.User.ID)
	var created workspace.Workspace
	if err == nil {
		created, err = s.deps.Keys.CreateTeam(ctx, p.User.ID, vaultKey(p), request.Body.Name)
	}
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, created.ID, audit.TeamCreated, created.Name, nil)
	return CreateWorkspace201JSONResponse(workspaceDTO(workspace.Summary{Workspace: created, Role: workspace.RoleOwner})), nil
}

// ListMembers lists the members of a team workspace.
func (s *server) ListMembers(ctx context.Context, request ListMembersRequestObject) (ListMembersResponseObject, error) {
	p, _ := principalFrom(ctx)
	list, err := s.deps.Workspaces.Members(ctx, p.User.ID, request.WorkspaceId)
	if err == nil {
		err = s.completeKeys(ctx, p, request.WorkspaceId)
	}
	var sealed map[uuid.UUID]bool
	if err == nil {
		sealed, err = s.deps.Keys.Sealed(ctx, p.User.ID, request.WorkspaceId)
	}
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(ListMembers200JSONResponse, 0, len(list))
	for _, m := range list {
		out = append(out, memberDTO(m, sealed[m.UserID]))
	}
	return out, nil
}

// completeKeys brings the keys of a team up to date for the caller: it seals the key for members who have set up their vault and rotates
// it after a removal. It needs the vault unlocked and the key in the caller's hands; when either is missing there is nothing to do.
func (s *server) completeKeys(ctx context.Context, p authenticated, workspaceID uuid.UUID) error {
	err := s.deps.Keys.Complete(ctx, p.User.ID, workspaceID, vaultKey(p))
	if errors.Is(err, vault.ErrLocked) || errors.Is(err, wskey.ErrPending) {
		return nil
	}
	return err
}

// SetMemberRole changes the role of a member.
func (s *server) SetMemberRole(ctx context.Context, request SetMemberRoleRequestObject) (SetMemberRoleResponseObject, error) {
	p, _ := principalFrom(ctx)
	email := s.memberEmail(ctx, p.User.ID, request.WorkspaceId, request.UserId)
	err := s.deps.Workspaces.SetRole(ctx, p.User.ID, request.WorkspaceId, request.UserId, workspace.Role(request.Body.Role))
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.MemberRoleChanged, email, map[string]string{"role": string(request.Body.Role)})
	return SetMemberRole204Response{}, nil
}

// RemoveMember removes a member from a team workspace.
func (s *server) RemoveMember(ctx context.Context, request RemoveMemberRequestObject) (RemoveMemberResponseObject, error) {
	p, _ := principalFrom(ctx)
	email := s.memberEmail(ctx, p.User.ID, request.WorkspaceId, request.UserId)
	err := s.deps.Workspaces.RemoveMember(ctx, p.User.ID, request.WorkspaceId, request.UserId)
	if err == nil {
		s.record(ctx, p.User.ID, request.WorkspaceId, audit.MemberRemoved, email, nil)
		err = s.completeKeys(ctx, p, request.WorkspaceId)
	}
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return RemoveMember204Response{}, nil
}

// LeaveWorkspace removes the signed-in user from a team workspace.
func (s *server) LeaveWorkspace(ctx context.Context, request LeaveWorkspaceRequestObject) (LeaveWorkspaceResponseObject, error) {
	p, _ := principalFrom(ctx)
	err := s.deps.Workspaces.Leave(ctx, p.User.ID, request.WorkspaceId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.MemberLeft, p.User.Email, nil)
	return LeaveWorkspace204Response{}, nil
}

// TransferOwnership hands a team workspace to another member.
func (s *server) TransferOwnership(ctx context.Context, request TransferOwnershipRequestObject) (TransferOwnershipResponseObject, error) {
	p, _ := principalFrom(ctx)
	email := s.memberEmail(ctx, p.User.ID, request.WorkspaceId, request.Body.UserId)
	err := s.deps.Workspaces.TransferOwnership(ctx, p.User.ID, request.WorkspaceId, request.Body.UserId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.OwnershipTransferred, email, nil)
	return TransferOwnership204Response{}, nil
}
