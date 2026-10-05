package httpapi

import (
	"context"
	"net/http"
	"path"

	"github.com/fprambs/transferly/internal/core/activity"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// serverToolsDTO converts what a server can do to its public representation.
func serverToolsDTO(t remotefs.Tools) ServerTools {
	out := ServerTools{Copy: t.Copy, Usage: t.Usage, Checksums: []ServerToolsChecksums{}, Archive: []ArchiveFormat{}, Extract: []ArchiveFormat{}}
	for _, c := range t.Checksums {
		out.Checksums = append(out.Checksums, ServerToolsChecksums(c))
	}
	for _, f := range t.Archive {
		out.Archive = append(out.Archive, ArchiveFormat(f))
	}
	for _, f := range t.Extract {
		out.Extract = append(out.Extract, ArchiveFormat(f))
	}
	return out
}

// GetServerTools tells what the server of a connection can do on its own (ADR-0027).
func (s *server) GetServerTools(ctx context.Context, request GetServerToolsRequestObject) (GetServerToolsResponseObject, error) {
	tools, err := s.deps.Files.Tools(ctx, target(ctx, request.WorkspaceId, request.SiteId))
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return GetServerTools200JSONResponse(serverToolsDTO(tools)), nil
}

// GetDiskUsage measures the space a file or folder takes on its server.
func (s *server) GetDiskUsage(ctx context.Context, request GetDiskUsageRequestObject) (GetDiskUsageResponseObject, error) {
	used, err := s.deps.Files.Usage(ctx, target(ctx, request.WorkspaceId, request.SiteId), request.Params.Path)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return GetDiskUsage200JSONResponse{Bytes: used}, nil
}

// CompressFiles packs entries of a folder into a new archive on the server, and records it in the activity of the person.
func (s *server) CompressFiles(ctx context.Context, request CompressFilesRequestObject) (CompressFilesResponseObject, error) {
	t := target(ctx, request.WorkspaceId, request.SiteId)
	body := request.Body
	entry, err := s.deps.Files.Compress(ctx, t, body.Dir, body.Names, body.Name, string(body.Format))
	s.audit(ctx, t, activity.Compress, body.Dir, path.Join(body.Dir, body.Name), err)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return CompressFiles201JSONResponse(remoteEntryDTO(entry)), nil
}

// ExtractArchive unpacks an archive into a new folder next to it on the server, and records it in the activity of the person.
func (s *server) ExtractArchive(ctx context.Context, request ExtractArchiveRequestObject) (ExtractArchiveResponseObject, error) {
	t := target(ctx, request.WorkspaceId, request.SiteId)
	entry, err := s.deps.Files.Extract(ctx, t, request.Body.Path)
	s.audit(ctx, t, activity.Extract, request.Body.Path, entry.Path, err)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return ExtractArchive201JSONResponse(remoteEntryDTO(entry)), nil
}

// VisitGetServerToolsResponse implements [GetServerToolsResponseObject].
func (p domainProblem) VisitGetServerToolsResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitGetDiskUsageResponse implements [GetDiskUsageResponseObject].
func (p domainProblem) VisitGetDiskUsageResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCompressFilesResponse implements [CompressFilesResponseObject].
func (p domainProblem) VisitCompressFilesResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitExtractArchiveResponse implements [ExtractArchiveResponseObject].
func (p domainProblem) VisitExtractArchiveResponse(w http.ResponseWriter) error { return p.write(w) }
