package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/site"
)

// siteDTO converts a site to its public representation.
func siteDTO(s site.Site) Site {
	secrets := make([]SiteSecrets, 0, len(s.Secrets))
	for _, kind := range s.Secrets {
		secrets = append(secrets, SiteSecrets(kind))
	}
	out := Site{
		Id: s.ID, Name: s.Name, Protocol: SiteProtocol(s.Protocol), Host: s.Host, Port: s.Port, Username: s.Username,
		AuthMethod: SiteAuthMethod(s.AuthMethod), ListTimezone: s.ListTimezone, Secrets: secrets, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
	if s.CollectionID != uuid.Nil {
		out.CollectionId = &s.CollectionID
	}
	return out
}

// siteInput converts the request body to the settings and secret changes of a site.
func siteInput(in SiteInput) (site.Input, site.SecretChanges) {
	settings := site.Input{CollectionID: optionalID(in.CollectionId), Name: in.Name, Protocol: site.Protocol(in.Protocol), Host: in.Host, Username: in.Username}
	if in.Port != nil {
		settings.Port = *in.Port
	}
	if in.AuthMethod != nil {
		settings.AuthMethod = site.AuthMethod(*in.AuthMethod)
	}
	if in.ListTimezone != nil {
		settings.ListTimezone = *in.ListTimezone
	}
	return settings, site.SecretChanges{Password: in.Password, PrivateKey: in.PrivateKey, Passphrase: in.Passphrase}
}

// ListSites lists the sites of a workspace.
func (s *server) ListSites(ctx context.Context, request ListSitesRequestObject) (ListSitesResponseObject, error) {
	p, _ := principalFrom(ctx)
	sites, err := s.deps.Sites.List(ctx, p.User.ID, request.WorkspaceId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(ListSites200JSONResponse, 0, len(sites))
	for _, st := range sites {
		out = append(out, siteDTO(st))
	}
	return out, nil
}

// CreateSite saves a site.
func (s *server) CreateSite(ctx context.Context, request CreateSiteRequestObject) (CreateSiteResponseObject, error) {
	p, _ := principalFrom(ctx)
	settings, secrets := siteInput(*request.Body)
	created, err := s.deps.Sites.Create(ctx, p.User.ID, request.WorkspaceId, vaultKey(p), settings, secrets)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return CreateSite201JSONResponse(siteDTO(created)), nil
}

// GetSite returns one site.
func (s *server) GetSite(ctx context.Context, request GetSiteRequestObject) (GetSiteResponseObject, error) {
	p, _ := principalFrom(ctx)
	found, err := s.deps.Sites.Get(ctx, p.User.ID, request.WorkspaceId, uuid.UUID(request.SiteId))
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return GetSite200JSONResponse(siteDTO(found)), nil
}

// UpdateSite changes a site.
func (s *server) UpdateSite(ctx context.Context, request UpdateSiteRequestObject) (UpdateSiteResponseObject, error) {
	p, _ := principalFrom(ctx)
	settings, secrets := siteInput(*request.Body)
	updated, err := s.deps.Sites.Update(ctx, p.User.ID, request.WorkspaceId, request.SiteId, vaultKey(p), settings, secrets)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return UpdateSite200JSONResponse(siteDTO(updated)), nil
}

// DeleteSite removes a site and its secrets.
func (s *server) DeleteSite(ctx context.Context, request DeleteSiteRequestObject) (DeleteSiteResponseObject, error) {
	p, _ := principalFrom(ctx)
	err := s.deps.Sites.Delete(ctx, p.User.ID, request.WorkspaceId, request.SiteId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return DeleteSite204Response{}, nil
}
