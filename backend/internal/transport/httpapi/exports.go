package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/audit"
	"github.com/fprambs/sentinel/internal/core/backup"
	"github.com/fprambs/sentinel/internal/core/fzimport"
)

// maxSentinelImportBody bounds the body of a Sentinel import: a file of [backup.MaxBytes] written as a JSON string, whose escapes
// may nearly double it, and its password.
const maxSentinelImportBody = 2*backup.MaxBytes + 64<<10

// limitSentinelImports caps the body of Sentinel imports before it is decoded, since the file travels inside the JSON body.
func limitSentinelImports(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/imports/sentinel") {
			r.Body = http.MaxBytesReader(w, r.Body, maxSentinelImportBody)
		}
		next.ServeHTTP(w, r)
	})
}

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
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.SitesExported, "sentinel", map[string]string{"sites": strconv.Itoa(count)})
	return ExportSites200ApplicationoctetStreamResponse{
		Body: bytes.NewReader(file), ContentLength: int64(len(file)),
		Headers: ExportSites200ResponseHeaders{ContentDisposition: `attachment; filename="sentinel-sites.json"`},
	}, nil
}

// ImportSentinel creates the collections and sites of a file exported by Sentinel.
func (s *server) ImportSentinel(ctx context.Context, request ImportSentinelRequestObject) (ImportSentinelResponseObject, error) {
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
		s.record(ctx, p.User.ID, request.WorkspaceId, audit.SitesImported, "sentinel", map[string]string{"sites": strconv.Itoa(created)})
	}
	return ImportSentinel200JSONResponse(importResultDTO(result, opts.DryRun)), nil
}
