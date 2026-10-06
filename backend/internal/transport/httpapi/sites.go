package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/credentials"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/vault"
)

// siteDTO converts a site to its public representation.
func siteDTO(s site.Site) Site {
	secrets := make([]string, 0, len(s.Secrets))
	for _, kind := range s.Secrets {
		secrets = append(secrets, string(kind))
	}
	out := Site{
		Id: s.ID, Name: s.Name, Connector: s.Connector, Config: SiteConfig(s.Config), SpeedLimitBps: s.SpeedLimit,
		Secrets: secrets, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
	if s.RemoteDir != "" {
		out.RemoteDir = &s.RemoteDir
	}
	if s.Color != "" {
		color := SiteColor(s.Color)
		out.Color = &color
	}
	if s.CollectionID != uuid.Nil {
		out.CollectionId = &s.CollectionID
	}
	return out
}

// secretChanges converts the secrets of a request body to secret changes.
func secretChanges(in *SiteSecretChanges) site.SecretChanges {
	changes := site.SecretChanges{}
	if in == nil {
		return changes
	}
	for kind, value := range *in {
		changes[site.SecretKind(kind)] = value
	}
	return changes
}

// siteInput converts the request body to the settings and secret changes of a site.
func siteInput(in SiteInput) (site.Input, site.SecretChanges) {
	settings := site.Input{CollectionID: optionalID(in.CollectionId), Name: in.Name, Connector: in.Connector, Config: in.Config}
	if in.SpeedLimitBps != nil {
		settings.SpeedLimit = *in.SpeedLimitBps
	}
	if in.RemoteDir != nil {
		settings.RemoteDir = *in.RemoteDir
	}
	if in.Color != nil {
		settings.Color = site.Color(*in.Color)
	}
	return settings, secretChanges(in.Secrets)
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
	var created site.Site
	var err error
	if from := request.Body.SecretsFrom; from != nil {
		created, err = s.deps.Sites.CreateCopy(ctx, p.User.ID, request.WorkspaceId, vaultKey(p), *from, settings, secrets)
	} else {
		created, err = s.deps.Sites.Create(ctx, p.User.ID, request.WorkspaceId, vaultKey(p), settings, secrets)
	}
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.SiteCreated, created.Name, map[string]string{"host": created.Setting(connectors.FieldHost)})
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
	s.siteChanged(request.SiteId)
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.SiteUpdated, updated.Name, map[string]string{"host": updated.Setting(connectors.FieldHost)})
	return UpdateSite200JSONResponse(siteDTO(updated)), nil
}

// siteChanged closes every connection to a site, for every user, and drops the passwords typed for it: whatever was
// opened with its old settings must not stay in use.
func (s *server) siteChanged(siteID uuid.UUID) {
	s.deps.Connections.CloseSite(siteID)
	s.deps.Credentials.ForgetSite(siteID)
}

// SetSiteCredential holds, for this session only, the password of a site that asks for it when connecting (ADR-0010).
func (s *server) SetSiteCredential(ctx context.Context, request SetSiteCredentialRequestObject) (SetSiteCredentialResponseObject, error) {
	p, _ := principalFrom(ctx)
	found, err := s.deps.Sites.Get(ctx, p.User.ID, request.WorkspaceId, request.SiteId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	if found.Setting(connectors.FieldAuth) != connectors.AuthAsk {
		return domainProblem{status: http.StatusConflict, code: "credentials_not_asked"}, nil
	}
	if request.Body.Password == "" {
		return domainProblem{status: http.StatusUnprocessableEntity, code: "password_required"}, nil
	}
	password := []byte(request.Body.Password)
	s.deps.Credentials.Put(credentials.Key{Session: vaultKey(p), UserID: p.User.ID, WorkspaceID: request.WorkspaceId, SiteID: request.SiteId}, password, request.Body.Remember)
	vault.Wipe(password)
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.SiteCredentialProvided, found.Name, nil)
	return SetSiteCredential204Response{}, nil
}

// DeleteSite removes a site and its secrets.
func (s *server) DeleteSite(ctx context.Context, request DeleteSiteRequestObject) (DeleteSiteResponseObject, error) {
	p, _ := principalFrom(ctx)
	name := ""
	if found, err := s.deps.Sites.Get(ctx, p.User.ID, request.WorkspaceId, request.SiteId); err == nil {
		name = found.Name
	}
	err := s.deps.Sites.Delete(ctx, p.User.ID, request.WorkspaceId, request.SiteId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.SiteDeleted, name, nil)
	s.siteChanged(request.SiteId)
	return DeleteSite204Response{}, nil
}
