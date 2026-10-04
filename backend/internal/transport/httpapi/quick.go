package httpapi

import (
	"context"
	"net/http"

	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/quick"
	"github.com/fprambs/transferly/internal/core/site"
)

// quickDTO converts a quick connection to its public representation, which never holds credentials.
func quickDTO(c quick.Connection) QuickConnection {
	return QuickConnection{Id: c.Site.ID, Name: c.Site.Name, Connector: c.Site.Connector, Config: SiteConfig(c.Site.Config), CreatedAt: c.Site.CreatedAt}
}

// ListQuickConnections lists the user's quick connections of a workspace.
func (s *server) ListQuickConnections(ctx context.Context, request ListQuickConnectionsRequestObject) (ListQuickConnectionsResponseObject, error) {
	p, _ := principalFrom(ctx)
	list, err := s.deps.Quick.List(ctx, p.User.ID, request.WorkspaceId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(ListQuickConnections200JSONResponse, 0, len(list))
	for _, c := range list {
		out = append(out, quickDTO(c))
	}
	return out, nil
}

// CreateQuickConnection starts keeping a server the user wants to try.
func (s *server) CreateQuickConnection(ctx context.Context, request CreateQuickConnectionRequestObject) (CreateQuickConnectionResponseObject, error) {
	p, _ := principalFrom(ctx)
	if ok, wait := s.deps.Limits.QuickConnect.Allow("quick:" + p.User.ID.String()); !ok {
		return CreateQuickConnection429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	b := request.Body
	in := site.Input{Connector: b.Connector, Config: b.Config}
	secrets := quick.Secrets{}
	for kind, value := range secretChanges(b.Secrets) {
		if value != "" {
			secrets[kind] = []byte(value)
		}
	}
	created, err := s.deps.Quick.Create(ctx, p.User.ID, request.WorkspaceId, in, secrets)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return CreateQuickConnection201JSONResponse(quickDTO(created)), nil
}

// CloseQuickConnection ends a quick connection and its pooled connections.
func (s *server) CloseQuickConnection(ctx context.Context, request CloseQuickConnectionRequestObject) (CloseQuickConnectionResponseObject, error) {
	p, _ := principalFrom(ctx)
	if err := s.deps.Quick.Close(p.User.ID, request.WorkspaceId, request.QuickId); err != nil {
		return domainProblem{status: http.StatusNotFound, code: "quick_connection_not_found"}, nil
	}
	s.deps.Connections.CloseKey(connections.Key{UserID: p.User.ID, WorkspaceID: request.WorkspaceId, SiteID: request.QuickId})
	return CloseQuickConnection204Response{}, nil
}

// SaveQuickConnection keeps a quick connection as a site.
func (s *server) SaveQuickConnection(ctx context.Context, request SaveQuickConnectionRequestObject) (SaveQuickConnectionResponseObject, error) {
	p, _ := principalFrom(ctx)
	saved, err := s.deps.Quick.Save(ctx, p.User.ID, request.WorkspaceId, request.QuickId, vaultKey(p), request.Body.Name, optionalID(request.Body.CollectionId))
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.deps.Connections.CloseKey(connections.Key{UserID: p.User.ID, WorkspaceID: request.WorkspaceId, SiteID: request.QuickId})
	return SaveQuickConnection201JSONResponse(siteDTO(saved)), nil
}
