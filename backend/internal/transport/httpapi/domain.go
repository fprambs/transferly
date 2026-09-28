package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/fprambs/sentinel/internal/core/admin"
	"github.com/fprambs/sentinel/internal/core/audit"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/credentials"
	"github.com/fprambs/sentinel/internal/core/files"
	"github.com/fprambs/sentinel/internal/core/fzimport"
	"github.com/fprambs/sentinel/internal/core/hostkey"
	"github.com/fprambs/sentinel/internal/core/invite"
	"github.com/fprambs/sentinel/internal/core/limits"
	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/quick"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/transfer"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/core/wskey"
)

// domainProblem is the answer to a operation that failed for a reason the client can act on.
// It implements the response interface of every operation.
type domainProblem struct {
	status  int
	code    string
	params  map[string]any
	headers map[string]string
}

// write sends the problem document.
func (p domainProblem) write(w http.ResponseWriter) error {
	for name, value := range p.headers {
		w.Header().Set(name, value)
	}
	writeProblem(w, p.status, p.code, p.params)
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

// VisitListQuickConnectionsResponse implements [ListQuickConnectionsResponseObject].
func (p domainProblem) VisitListQuickConnectionsResponse(w http.ResponseWriter) error {
	return p.write(w)
}

// VisitCreateQuickConnectionResponse implements [CreateQuickConnectionResponseObject].
func (p domainProblem) VisitCreateQuickConnectionResponse(w http.ResponseWriter) error {
	return p.write(w)
}

// VisitCloseQuickConnectionResponse implements [CloseQuickConnectionResponseObject].
func (p domainProblem) VisitCloseQuickConnectionResponse(w http.ResponseWriter) error {
	return p.write(w)
}

// VisitSaveQuickConnectionResponse implements [SaveQuickConnectionResponseObject].
func (p domainProblem) VisitSaveQuickConnectionResponse(w http.ResponseWriter) error {
	return p.write(w)
}

// VisitListCollectionsResponse implements [ListCollectionsResponseObject].
func (p domainProblem) VisitListCollectionsResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCreateCollectionResponse implements [CreateCollectionResponseObject].
func (p domainProblem) VisitCreateCollectionResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitUpdateCollectionResponse implements [UpdateCollectionResponseObject].
func (p domainProblem) VisitUpdateCollectionResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitDeleteCollectionResponse implements [DeleteCollectionResponseObject].
func (p domainProblem) VisitDeleteCollectionResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitGetTransferResponse implements [GetTransferResponseObject].
func (p domainProblem) VisitGetTransferResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitDeleteTransferResponse implements [DeleteTransferResponseObject].
func (p domainProblem) VisitDeleteTransferResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitPauseTransferResponse implements [PauseTransferResponseObject].
func (p domainProblem) VisitPauseTransferResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitResumeTransferResponse implements [ResumeTransferResponseObject].
func (p domainProblem) VisitResumeTransferResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitSetTransferPriorityResponse implements [SetTransferPriorityResponseObject].
func (p domainProblem) VisitSetTransferPriorityResponse(w http.ResponseWriter) error {
	return p.write(w)
}

// VisitCancelTransferResponse implements [CancelTransferResponseObject].
func (p domainProblem) VisitCancelTransferResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitListFilesResponse implements [ListFilesResponseObject].
func (p domainProblem) VisitListFilesResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitStatFileResponse implements [StatFileResponseObject].
func (p domainProblem) VisitStatFileResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitDownloadFileResponse implements [DownloadFileResponseObject].
func (p domainProblem) VisitDownloadFileResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitUploadFileResponse implements [UploadFileResponseObject].
func (p domainProblem) VisitUploadFileResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCreateDirectoryResponse implements [CreateDirectoryResponseObject].
func (p domainProblem) VisitCreateDirectoryResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitRenameFileResponse implements [RenameFileResponseObject].
func (p domainProblem) VisitRenameFileResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitDeleteFileResponse implements [DeleteFileResponseObject].
func (p domainProblem) VisitDeleteFileResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitSetSiteCredentialResponse implements [SetSiteCredentialResponseObject].
func (p domainProblem) VisitSetSiteCredentialResponse(w http.ResponseWriter) error { return p.write(w) }

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
	{workspace.ErrInvalidName, http.StatusUnprocessableEntity, "invalid_name"},
	{workspace.ErrPersonal, http.StatusConflict, "personal_workspace"},
	{workspace.ErrMemberNotFound, http.StatusNotFound, "member_not_found"},
	{workspace.ErrInvalidRole, http.StatusUnprocessableEntity, "invalid_role"},
	{vault.ErrLocked, http.StatusConflict, "vault_locked"},
	{credentials.ErrRequired, http.StatusConflict, "credentials_required"},
	{wskey.ErrPending, http.StatusConflict, "workspace_key_pending"},
	{workspace.ErrOwnerBound, http.StatusConflict, "owner_bound"},
	{workspace.ErrAlreadyMember, http.StatusConflict, "already_member"},
	{audit.ErrInvalidFilter, http.StatusUnprocessableEntity, "invalid_filter"},
	{audit.ErrForbidden, http.StatusForbidden, "forbidden"},
	{admin.ErrForbidden, http.StatusForbidden, "forbidden"},
	{admin.ErrNotFound, http.StatusNotFound, "account_not_found"},
	{admin.ErrSelf, http.StatusConflict, "own_account"},
	{admin.ErrLastAdmin, http.StatusConflict, "last_admin"},
	{admin.ErrInvalidRole, http.StatusUnprocessableEntity, "invalid_role"},
	{limits.ErrInvalid, http.StatusUnprocessableEntity, "invalid_limits"},
	{limits.ErrSiteQuota, http.StatusConflict, "site_quota_reached"},
	{limits.ErrTeamQuota, http.StatusConflict, "team_quota_reached"},
	{limits.ErrMemberQuota, http.StatusConflict, "member_quota_reached"},
	{limits.ErrDailyQuota, http.StatusForbidden, "daily_quota_reached"},
	{admin.ErrInvalidNetwork, http.StatusUnprocessableEntity, "invalid_network"},
	{admin.ErrTooManyNetworks, http.StatusUnprocessableEntity, "too_many_networks"},
	{invite.ErrNotFound, http.StatusNotFound, "invitation_not_found"},
	{invite.ErrTooMany, http.StatusConflict, "too_many_invitations"},
	{auth.ErrInvalidEmail, http.StatusUnprocessableEntity, "invalid_email"},
	{auth.ErrEmailTaken, http.StatusConflict, "account_exists"},
	{site.ErrNotFound, http.StatusNotFound, "site_not_found"},
	{site.ErrNameTaken, http.StatusConflict, "site_name_taken"},
	{site.ErrCollectionNotFound, http.StatusNotFound, "collection_not_found"},
	{site.ErrCollectionNameTaken, http.StatusConflict, "collection_name_taken"},
	{site.ErrCollectionNotEmpty, http.StatusConflict, "collection_not_empty"},
	{site.ErrCollectionCycle, http.StatusUnprocessableEntity, "collection_cycle"},
	{site.ErrCollectionTooDeep, http.StatusUnprocessableEntity, "collection_too_deep"},
	{site.ErrLocked, http.StatusConflict, "vault_locked"},
	{site.ErrSecretTooLarge, http.StatusUnprocessableEntity, "secret_too_large"},
	{site.ErrInvalidName, http.StatusUnprocessableEntity, "invalid_name"},
	{site.ErrInvalidProtocol, http.StatusUnprocessableEntity, "invalid_protocol"},
	{site.ErrInvalidHost, http.StatusUnprocessableEntity, "invalid_host"},
	{site.ErrInvalidPort, http.StatusUnprocessableEntity, "invalid_port"},
	{site.ErrInvalidUsername, http.StatusUnprocessableEntity, "invalid_username"},
	{site.ErrInvalidAuthMethod, http.StatusUnprocessableEntity, "invalid_auth_method"},
	{site.ErrInvalidCharset, http.StatusUnprocessableEntity, "invalid_charset"},
	{site.ErrInvalidTimezone, http.StatusUnprocessableEntity, "invalid_timezone"},
	{site.ErrInvalidSpeedLimit, http.StatusUnprocessableEntity, "invalid_speed_limit"},
	{site.ErrInvalidProxy, http.StatusUnprocessableEntity, "invalid_proxy"},
	{site.ErrInvalidJump, http.StatusUnprocessableEntity, "invalid_jump"},
	{hostkey.ErrNotFound, http.StatusNotFound, "host_key_not_found"},
	{hostkey.ErrChanged, http.StatusConflict, "host_key_changed"},
	{hostkey.ErrInvalid, http.StatusUnprocessableEntity, "invalid_host_key"},
	{context.Canceled, http.StatusConflict, "transfer_canceled"},
	{quick.ErrNotFound, http.StatusNotFound, "quick_connection_not_found"},
	{quick.ErrTooMany, http.StatusConflict, "too_many_quick_connections"},
	{transfer.ErrNotFound, http.StatusNotFound, "transfer_not_found"},
	{transfer.ErrActive, http.StatusConflict, "transfer_active"},
	{transfer.ErrNotActive, http.StatusConflict, "transfer_not_active"},
	{transfer.ErrNotQueued, http.StatusConflict, "transfer_not_queued"},
	{transfer.ErrInvalidPriority, http.StatusUnprocessableEntity, "invalid_priority"},
	{transfer.ErrExists, http.StatusConflict, "transfer_id_taken"},
	{fzimport.ErrInvalidFile, http.StatusUnprocessableEntity, "invalid_import_file"},
	{fzimport.ErrTooLarge, http.StatusUnprocessableEntity, "import_file_too_large"},
	{files.ErrProbeInconclusive, http.StatusUnprocessableEntity, "time_zone_inconclusive"},
	{files.ErrUnsupportedAlgorithm, http.StatusUnprocessableEntity, "unsupported_algorithm"},
	{files.ErrChecksumMismatch, http.StatusBadGateway, "checksum_mismatch"},
	{files.ErrTreeTooLarge, http.StatusUnprocessableEntity, "tree_too_large"},
	{remotefs.ErrNotFound, http.StatusNotFound, "path_not_found"},
	{remotefs.ErrExists, http.StatusConflict, "path_exists"},
	{remotefs.ErrNotEmpty, http.StatusConflict, "directory_not_empty"},
	{remotefs.ErrNotDirectory, http.StatusUnprocessableEntity, "not_a_directory"},
	{remotefs.ErrInvalid, http.StatusUnprocessableEntity, "invalid_path"},
	{remotefs.ErrPermission, http.StatusForbidden, "remote_permission_denied"},
	{remotefs.ErrQuota, http.StatusInsufficientStorage, "remote_quota_exceeded"},
	{remotefs.ErrAuth, http.StatusBadGateway, "remote_auth_failed"},
	{remotefs.ErrConnectionLost, http.StatusBadGateway, "remote_unavailable"},
	{netguard.ErrBlocked, http.StatusUnprocessableEntity, "destination_blocked"},
	{netguard.ErrUnresolvable, http.StatusUnprocessableEntity, "host_unresolvable"},
}

// asProblem returns the problem for err, or false when err is not one the client can act on.
func asProblem(err error) (domainProblem, bool) {
	var hostKey *remotefs.HostKeyError
	var badRange *files.RangeError
	switch {
	case errors.As(err, &hostKey):
		code := "host_key_unknown"
		if hostKey.Changed {
			code = "host_key_changed"
		}
		return domainProblem{status: http.StatusConflict, code: code, params: map[string]any{"keyType": hostKey.Type, "fingerprint": hostKey.Fingerprint}}, true
	case errors.As(err, &badRange):
		return domainProblem{status: http.StatusRequestedRangeNotSatisfiable, code: "range_not_satisfiable",
			headers: map[string]string{"Content-Range": fmt.Sprintf("bytes */%d", badRange.Size)}}, true
	}
	for _, m := range domainProblems {
		if errors.Is(err, m.err) {
			return domainProblem{status: m.status, code: m.code}, true
		}
	}
	return domainProblem{}, false
}

// VisitCreateCopyResponse implements [CreateCopyResponseObject].
func (p domainProblem) VisitCreateCopyResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitDownloadArchiveResponse implements [DownloadArchiveResponseObject].
func (p domainProblem) VisitDownloadArchiveResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitGetChecksumResponse implements [GetChecksumResponseObject].
func (p domainProblem) VisitGetChecksumResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitImportFilezillaResponse implements [ImportFilezillaResponseObject].
func (p domainProblem) VisitImportFilezillaResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitProbeTimeZoneResponse implements [ProbeTimeZoneResponseObject].
func (p domainProblem) VisitProbeTimeZoneResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCreateWorkspaceResponse implements [CreateWorkspaceResponseObject].
func (p domainProblem) VisitCreateWorkspaceResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitListMembersResponse implements [ListMembersResponseObject].
func (p domainProblem) VisitListMembersResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitSetMemberRoleResponse implements [SetMemberRoleResponseObject].
func (p domainProblem) VisitSetMemberRoleResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitRemoveMemberResponse implements [RemoveMemberResponseObject].
func (p domainProblem) VisitRemoveMemberResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitLeaveWorkspaceResponse implements [LeaveWorkspaceResponseObject].
func (p domainProblem) VisitLeaveWorkspaceResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitTransferOwnershipResponse implements [TransferOwnershipResponseObject].
func (p domainProblem) VisitTransferOwnershipResponse(w http.ResponseWriter) error {
	return p.write(w)
}

// VisitListInvitationsResponse implements [ListInvitationsResponseObject].
func (p domainProblem) VisitListInvitationsResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCreateInvitationResponse implements [CreateInvitationResponseObject].
func (p domainProblem) VisitCreateInvitationResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitRevokeInvitationResponse implements [RevokeInvitationResponseObject].
func (p domainProblem) VisitRevokeInvitationResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitPreviewInvitationResponse implements [PreviewInvitationResponseObject].
func (p domainProblem) VisitPreviewInvitationResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitAcceptInvitationResponse implements [AcceptInvitationResponseObject].
func (p domainProblem) VisitAcceptInvitationResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitJoinWithInvitationResponse implements [JoinWithInvitationResponseObject].
func (p domainProblem) VisitJoinWithInvitationResponse(w http.ResponseWriter) error {
	return p.write(w)
}

// VisitListAuditResponse implements [ListAuditResponseObject].
func (p domainProblem) VisitListAuditResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitListAdminUsersResponse implements [ListAdminUsersResponseObject].
func (p domainProblem) VisitListAdminUsersResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitUpdateAdminUserResponse implements [UpdateAdminUserResponseObject].
func (p domainProblem) VisitUpdateAdminUserResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitResetSecondFactorResponse implements [ResetSecondFactorResponseObject].
func (p domainProblem) VisitResetSecondFactorResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitGetNetworksResponse implements [GetNetworksResponseObject].
func (p domainProblem) VisitGetNetworksResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitSetNetworksResponse implements [SetNetworksResponseObject].
func (p domainProblem) VisitSetNetworksResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitListAdminAuditResponse implements [ListAdminAuditResponseObject].
func (p domainProblem) VisitListAdminAuditResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitGetLimitsResponse implements [GetLimitsResponseObject].
func (p domainProblem) VisitGetLimitsResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitSetLimitsResponse implements [SetLimitsResponseObject].
func (p domainProblem) VisitSetLimitsResponse(w http.ResponseWriter) error { return p.write(w) }
