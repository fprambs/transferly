package httpapi

import (
	"context"
	"path"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/files"
	"github.com/fprambs/sentinel/internal/core/transfer"
)

// transferDTO converts a transfer to its public representation.
func transferDTO(t transfer.Transfer) Transfer {
	out := Transfer{
		Id: t.ID, WorkspaceId: t.WorkspaceID, SiteName: t.SiteName, Direction: TransferDirection(t.Direction), Path: t.Path, Name: t.Name,
		Status: TransferStatus(t.Status), TotalBytes: t.TotalBytes, BytesDone: t.BytesDone, Attempts: t.Attempts,
		CreatedAt: t.CreatedAt, StartedAt: t.StartedAt, UpdatedAt: t.UpdatedAt, FinishedAt: t.FinishedAt,
	}
	if t.SiteID != uuid.Nil {
		out.SiteId = &t.SiteID
	}
	if t.ErrorCode != "" {
		out.ErrorCode = &t.ErrorCode
	}
	priority := TransferPriority(t.Priority)
	out.Paused, out.Priority = &t.Paused, &priority
	return out
}

// ListTransfers lists the signed-in user's transfers.
func (s *server) ListTransfers(ctx context.Context, request ListTransfersRequestObject) (ListTransfersResponseObject, error) {
	p, _ := principalFrom(ctx)
	workspaceID, limit := uuid.Nil, 0
	if request.Params.WorkspaceId != nil {
		workspaceID = *request.Params.WorkspaceId
	}
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	list, err := s.deps.Transfers.List(ctx, p.User.ID, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	out := make(ListTransfers200JSONResponse, 0, len(list))
	for _, t := range list {
		out = append(out, transferDTO(t))
	}
	return out, nil
}

// GetTransfer returns one of the signed-in user's transfers.
func (s *server) GetTransfer(ctx context.Context, request GetTransferRequestObject) (GetTransferResponseObject, error) {
	p, _ := principalFrom(ctx)
	t, err := s.deps.Transfers.Get(ctx, p.User.ID, request.TransferId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return GetTransfer200JSONResponse(transferDTO(t)), nil
}

// DeleteTransfer removes a finished transfer from the history.
func (s *server) DeleteTransfer(ctx context.Context, request DeleteTransferRequestObject) (DeleteTransferResponseObject, error) {
	p, _ := principalFrom(ctx)
	err := s.deps.Transfers.Delete(ctx, p.User.ID, request.TransferId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return DeleteTransfer204Response{}, nil
}

// CancelTransfer stops a transfer in progress.
func (s *server) CancelTransfer(ctx context.Context, request CancelTransferRequestObject) (CancelTransferResponseObject, error) {
	p, _ := principalFrom(ctx)
	err := s.deps.Transfers.Cancel(ctx, p.User.ID, request.TransferId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return CancelTransfer204Response{}, nil
}

// PauseTransfer holds a waiting transfer back.
func (s *server) PauseTransfer(ctx context.Context, request PauseTransferRequestObject) (PauseTransferResponseObject, error) {
	p, _ := principalFrom(ctx)
	t, err := s.deps.Transfers.Pause(ctx, p.User.ID, request.TransferId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return PauseTransfer200JSONResponse(transferDTO(t)), nil
}

// ResumeTransfer lets a paused transfer start.
func (s *server) ResumeTransfer(ctx context.Context, request ResumeTransferRequestObject) (ResumeTransferResponseObject, error) {
	p, _ := principalFrom(ctx)
	t, err := s.deps.Transfers.Resume(ctx, p.User.ID, request.TransferId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return ResumeTransfer200JSONResponse(transferDTO(t)), nil
}

// SetTransferPriority changes the place of a waiting transfer in the queue.
func (s *server) SetTransferPriority(ctx context.Context, request SetTransferPriorityRequestObject) (SetTransferPriorityResponseObject, error) {
	p, _ := principalFrom(ctx)
	t, err := s.deps.Transfers.SetPriority(ctx, p.User.ID, request.TransferId, int(request.Body.Priority))
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return SetTransferPriority200JSONResponse(transferDTO(t)), nil
}

// copyPolicies maps the public policies of a copy to the ones of the file service.
var copyPolicies = map[CopyInputPolicy]files.Policy{
	CopyInputPolicyFail: files.PolicyFail, CopyInputPolicyOverwrite: files.PolicyOverwrite, CopyInputPolicySkip: files.PolicySkip,
	CopyInputPolicyNewer: files.PolicyNewer, CopyInputPolicyRename: files.PolicyRename,
}

// CreateCopy starts the copy of a file from one site to another and answers with its transfer. The data moves in the
// background, out of the request, so that closing the browser does not stop it; the user can cancel it as any transfer.
func (s *server) CreateCopy(ctx context.Context, request CreateCopyRequestObject) (CreateCopyResponseObject, error) {
	body := request.Body
	source := target(ctx, request.WorkspaceId, body.SourceSiteId)
	destination := target(ctx, request.WorkspaceId, body.TargetSiteId)
	location := files.CopyLocation{
		Source: source, SourcePath: body.SourcePath, Destination: destination, DestinationPath: body.TargetPath,
		Verify: body.Verify != nil && *body.Verify,
	}
	if err := s.deps.Files.CheckCopy(ctx, location); err != nil {
		return copyFailure(err)
	}
	_, sourceName, err := s.siteOf(ctx, source)
	if err != nil {
		return copyFailure(err)
	}
	targetID, targetName, err := s.siteOf(ctx, destination)
	if err != nil {
		return copyFailure(err)
	}
	in := transfer.BeginInput{
		UserID: destination.Actor, WorkspaceID: request.WorkspaceId, SiteID: targetID, Route: body.TargetSiteId,
		SiteName: sourceName + " → " + targetName, Direction: transfer.Copy, Path: body.TargetPath, Name: path.Base(body.TargetPath), Total: -1,
	}
	if body.TransferId != nil {
		in.ID = *body.TransferId
	}
	tracker, runCtx, err := s.deps.Transfers.Begin(context.WithoutCancel(ctx), in)
	if err != nil {
		return copyFailure(err)
	}
	policy := copyPolicies[CopyInputPolicyFail]
	if body.Policy != nil {
		policy = copyPolicies[*body.Policy]
	}
	location.Destination.Observer = tracker
	record := tracker.Snapshot()
	go func() {
		err := tracker.Admit(runCtx)
		if err == nil {
			_, err = s.deps.Files.Copy(runCtx, location, policy)
		}
		tracker.Finish(err, problemCode(err))
	}()
	return CreateCopy202JSONResponse(transferDTO(record)), nil
}

// copyFailure turns an error of a copy into its problem response, or returns it when it is unexpected.
func copyFailure(err error) (CreateCopyResponseObject, error) {
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	return nil, err
}

// ListActivity returns the signed-in user's activity log of a workspace.
func (s *server) ListActivity(ctx context.Context, request ListActivityRequestObject) (ListActivityResponseObject, error) {
	p, _ := principalFrom(ctx)
	siteID, limit := uuid.Nil, 0
	if request.Params.SiteId != nil {
		siteID = *request.Params.SiteId
	}
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	list, err := s.deps.Activity.List(ctx, p.User.ID, request.WorkspaceId, siteID, limit)
	if err != nil {
		return nil, err
	}
	out := make(ListActivity200JSONResponse, 0, len(list))
	for _, e := range list {
		entry := ActivityEntry{Id: e.ID, SiteName: e.SiteName, Kind: ActivityEntryKind(e.Kind), Path: e.Path, At: e.At}
		if e.SiteID != uuid.Nil {
			entry.SiteId = &e.SiteID
		}
		if e.Detail != "" {
			entry.Detail = &e.Detail
		}
		if e.ErrorCode != "" {
			entry.ErrorCode = &e.ErrorCode
		}
		out = append(out, entry)
	}
	return out, nil
}
