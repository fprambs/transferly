package httpapi

import (
	"context"

	"github.com/google/uuid"

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
