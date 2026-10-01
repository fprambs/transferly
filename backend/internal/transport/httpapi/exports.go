package httpapi

import (
	"bytes"
	"context"
	"strconv"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/fzimport"
)

// ExportSites answers every collection and site of the workspace, with their secrets, as a file sealed with the password given
// (ADR-0017).
func (s *server) ExportSites(ctx context.Context, request ExportSitesRequestObject) (ExportSitesResponseObject, error) {
	p, _ := principalFrom(ctx)
	file, count, err := s.deps.Backup.Export(ctx, p.User.ID, request.WorkspaceId, vaultKey(p), request.Body.Password)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.SitesExported, "transferly", map[string]string{"sites": strconv.Itoa(count)})
	return ExportSites200ApplicationoctetStreamResponse{
		Body: bytes.NewReader(file), ContentLength: int64(len(file)),
		Headers: ExportSites200ResponseHeaders{ContentDisposition: `attachment; filename="transferly-sites.json"`},
	}, nil
}

// ImportTransferly creates the collections and sites of a file exported by Transferly.
func (s *server) ImportTransferly(ctx context.Context, request ImportTransferlyRequestObject) (ImportTransferlyResponseObject, error) {
	p, _ := principalFrom(ctx)
	opts := fzimport.Options{
		DryRun: request.Params.DryRun != nil && *request.Params.DryRun, Passwords: request.Params.Secrets != nil && *request.Params.Secrets,
	}
	if request.Params.OnConflict != nil {
		opts.OnConflict = fzimport.Conflict(*request.Params.OnConflict)
	}
	if request.Params.CollectionId != nil {
		opts.Into = uuid.UUID(*request.Params.CollectionId)
	}
	result, err := s.deps.Backup.Import(ctx, p.User.ID, request.WorkspaceId, vaultKey(p), request.Body.Password, []byte(request.Body.File), opts)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	if created := result.Created(); !opts.DryRun && created > 0 {
		s.record(ctx, p.User.ID, request.WorkspaceId, audit.SitesImported, "transferly", map[string]string{"sites": strconv.Itoa(created)})
	}
	return ImportTransferly200JSONResponse(importResultDTO(result, opts.DryRun)), nil
}
