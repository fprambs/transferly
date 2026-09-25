package httpapi

import (
	"errors"
	"net/http"

	"github.com/fprambs/sentinel/internal/core/hostkey"
	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// domainProblem is the answer to a operation that failed for a reason the client can act on.
// It implements the response interface of every operation.
type domainProblem struct {
	status int
	code   string
}

// write sends the problem document.
func (p domainProblem) write(w http.ResponseWriter) error {
	writeProblem(w, p.status, p.code, nil)
	return nil
}

// VisitListSitesResponse implements [ListSitesResponseObject].
func (p domainProblem) VisitListSitesResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCreateSiteResponse implements [CreateSiteResponseObject].
func (p domainProblem) VisitCreateSiteResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitGetSiteResponse implements [GetSiteResponseObject].
func (p domainProblem) VisitGetSiteResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitUpdateSiteResponse implements [UpdateSiteResponseObject].
func (p domainProblem) VisitUpdateSiteResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitListKnownHostsResponse implements [ListKnownHostsResponseObject].
func (p domainProblem) VisitListKnownHostsResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitTrustHostKeyResponse implements [TrustHostKeyResponseObject].
func (p domainProblem) VisitTrustHostKeyResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitForgetHostKeyResponse implements [ForgetHostKeyResponseObject].
func (p domainProblem) VisitForgetHostKeyResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitDeleteSiteResponse implements [DeleteSiteResponseObject].
func (p domainProblem) VisitDeleteSiteResponse(w http.ResponseWriter) error { return p.write(w) }

// domainProblems maps the errors of the use cases to their status and stable code.
var domainProblems = []struct {
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
	{hostkey.ErrNotFound, http.StatusNotFound, "host_key_not_found"},
	{hostkey.ErrChanged, http.StatusConflict, "host_key_changed"},
	{hostkey.ErrInvalid, http.StatusUnprocessableEntity, "invalid_host_key"},
	{netguard.ErrBlocked, http.StatusUnprocessableEntity, "destination_blocked"},
	{netguard.ErrUnresolvable, http.StatusUnprocessableEntity, "host_unresolvable"},
}

// asProblem returns the problem for err, or false when err is not one the client can act on.
func asProblem(err error) (domainProblem, bool) {
	for _, m := range domainProblems {
		if errors.Is(err, m.err) {
			return domainProblem{m.status, m.code}, true
		}
	}
	return domainProblem{}, false
}
