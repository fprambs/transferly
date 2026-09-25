package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// siteProblem is the answer to a site operation that failed for a reason the client can act on.
// It implements the response interface of every site operation.
type siteProblem struct {
	status int
	code   string
}

// write sends the problem document.
func (p siteProblem) write(w http.ResponseWriter) error {
	writeProblem(w, p.status, p.code, nil)
	return nil
}

// VisitListSitesResponse implements [ListSitesResponseObject].
func (p siteProblem) VisitListSitesResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCreateSiteResponse implements [CreateSiteResponseObject].
func (p siteProblem) VisitCreateSiteResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitGetSiteResponse implements [GetSiteResponseObject].
func (p siteProblem) VisitGetSiteResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitUpdateSiteResponse implements [UpdateSiteResponseObject].
func (p siteProblem) VisitUpdateSiteResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitDeleteSiteResponse implements [DeleteSiteResponseObject].
func (p siteProblem) VisitDeleteSiteResponse(w http.ResponseWriter) error { return p.write(w) }

// siteProblems maps the errors of the site use cases to their status and stable code.
var siteProblems = []struct {
	err    error
	status int
	code   string
}{
	{workspace.ErrNotFound, http.StatusNotFound, "workspace_not_found"},
	{workspace.ErrForbidden, http.StatusForbidden, "forbidden"},
	{site.ErrNotFound, http.StatusNotFound, "site_not_found"},
	{site.ErrNameTaken, http.StatusConflict, "site_name_taken"},
	{site.ErrLocked, http.StatusConflict, "vault_locked"},
	{site.ErrSecretTooLarge, http.StatusUnprocessableEntity, "secret_too_large"},
	{site.ErrInvalidName, http.StatusUnprocessableEntity, "invalid_name"},
	{site.ErrInvalidProtocol, http.StatusUnprocessableEntity, "invalid_protocol"},
	{site.ErrInvalidHost, http.StatusUnprocessableEntity, "invalid_host"},
	{site.ErrInvalidPort, http.StatusUnprocessableEntity, "invalid_port"},
	{site.ErrInvalidUsername, http.StatusUnprocessableEntity, "invalid_username"},
	{site.ErrInvalidAuthMethod, http.StatusUnprocessableEntity, "invalid_auth_method"},
	{site.ErrInvalidTimezone, http.StatusUnprocessableEntity, "invalid_timezone"},
	{netguard.ErrBlocked, http.StatusUnprocessableEntity, "destination_blocked"},
	{netguard.ErrUnresolvable, http.StatusUnprocessableEntity, "host_unresolvable"},
}

// asSiteProblem returns the problem for err, or false when err is not one the client can act on.
func asSiteProblem(err error) (siteProblem, bool) {
	for _, m := range siteProblems {
		if errors.Is(err, m.err) {
			return siteProblem{m.status, m.code}, true
		}
	}
	return siteProblem{}, false
}

// siteDTO converts a site to its public representation.
func siteDTO(s site.Site) Site {
	secrets := make([]SiteSecrets, 0, len(s.Secrets))
	for _, kind := range s.Secrets {
		secrets = append(secrets, SiteSecrets(kind))
	}
	return Site{
		Id: s.ID, Name: s.Name, Protocol: SiteProtocol(s.Protocol), Host: s.Host, Port: s.Port, Username: s.Username,
		AuthMethod: SiteAuthMethod(s.AuthMethod), ListTimezone: s.ListTimezone, Secrets: secrets, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

// siteInput converts the request body to the settings and secret changes of a site.
func siteInput(in SiteInput) (site.Input, site.SecretChanges) {
	settings := site.Input{Name: in.Name, Protocol: site.Protocol(in.Protocol), Host: in.Host, Username: in.Username}
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
	if problem, ok := asSiteProblem(err); ok {
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
	if problem, ok := asSiteProblem(err); ok {
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
	if problem, ok := asSiteProblem(err); ok {
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
	if problem, ok := asSiteProblem(err); ok {
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
	if problem, ok := asSiteProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return DeleteSite204Response{}, nil
}
