package httpapi

import (
	"context"
	"strconv"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/fzimport"
)

// ImportFilezilla creates the folders and sites of a FileZilla site manager file.
func (s *server) ImportFilezilla(ctx context.Context, request ImportFilezillaRequestObject) (ImportFilezillaResponseObject, error) {
	p, _ := principalFrom(ctx)
	opts := fzimport.ParseOptions{}
	if request.Params.XFileZillaMasterPassword != nil {
		opts.MasterPassword = *request.Params.XFileZillaMasterPassword
	}
	servers, skipped, err := fzimport.ParseWithOptions(request.Body, opts)
	if err != nil {
		return importFailure(err)
	}
	importOpts := fzimport.Options{
		DryRun: request.Params.DryRun != nil && *request.Params.DryRun, Passwords: request.Params.Passwords != nil && *request.Params.Passwords,
	}
	if request.Params.OnConflict != nil {
		importOpts.OnConflict = fzimport.Conflict(*request.Params.OnConflict)
	}
	if request.Params.CollectionId != nil {
		importOpts.Into = uuid.UUID(*request.Params.CollectionId)
	}
	result, err := s.deps.Import.Import(ctx, p.User.ID, request.WorkspaceId, vaultKey(p), servers, skipped, importOpts)
	if err != nil {
		return importFailure(err)
	}
	if created := result.Created(); !importOpts.DryRun && created > 0 {
		s.record(ctx, p.User.ID, request.WorkspaceId, audit.SitesImported, "filezilla", map[string]string{"sites": strconv.Itoa(created)})
	}
	return ImportFilezilla200JSONResponse(importResultDTO(result, importOpts.DryRun)), nil
}

// importResultDTO renders what an import did, or would do, to each site.
func importResultDTO(result fzimport.Result, dryRun bool) ImportResult {
	out := ImportResult{DryRun: dryRun, Items: make([]ImportItem, 0, len(result.Items))}
	for _, item := range result.Items {
		dto := ImportItem{Name: item.Name, Path: item.Path, Status: ImportItemStatus(item.Status), PasswordStored: item.PasswordStored, PasswordSkipped: item.PasswordSkipped}
		if item.Reason != "" {
			dto.Reason = &item.Reason
		}
		out.Items = append(out.Items, dto)
	}
	return out
}

// importFailure answers an import that could not run: with its problem, or as an internal error.
func importFailure(err error) (ImportFilezillaResponseObject, error) {
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	return nil, err
}
