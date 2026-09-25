package httpapi

import (
	"context"

	"github.com/fprambs/sentinel/internal/core/workspace"
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
