package httpapi

import (
	"context"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/files"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/transfer"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// target returns who acts on which site for the request.
func target(ctx context.Context, workspaceID, siteID uuid.UUID) files.Target {
	p, _ := principalFrom(ctx)
	return files.Target{Actor: p.User.ID, Workspace: workspaceID, Site: siteID, Session: vaultKey(p)}
}

// entryType names the kind of a remote entry.
func entryType(e remotefs.Entry) RemoteEntryType {
	switch {
	case e.Mode.IsDir():
		return "directory"
	case e.Mode&fs.ModeSymlink != 0:
		return "symlink"
	case e.Mode.IsRegular():
		return "file"
	}
	return "other"
}

// precisionNames maps how exact a listed time is to its public name.
var precisionNames = map[remotefs.Precision]RemoteEntryModifiedPrecision{
	remotefs.PrecisionSecond: "second",
	remotefs.PrecisionMinute: "minute",
	remotefs.PrecisionDay:    "day",
}

// remoteEntryDTO converts a remote entry to its public representation.
func remoteEntryDTO(e remotefs.Entry) RemoteEntry {
	return RemoteEntry{
		Name: e.Name, Path: e.Path, Type: entryType(e), Size: e.Size, Mode: e.Mode.String(),
		ModifiedAt: e.ModTime.UTC(), ModifiedPrecision: precisionNames[e.ModTimePrecision],
	}
}

// ListFiles lists a remote directory.
func (s *server) ListFiles(ctx context.Context, request ListFilesRequestObject) (ListFilesResponseObject, error) {
	entries, err := s.deps.Files.List(ctx, target(ctx, request.WorkspaceId, request.SiteId), request.Params.Path)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(ListFiles200JSONResponse, 0, len(entries))
	for _, e := range entries {
		out = append(out, remoteEntryDTO(e))
	}
	return out, nil
}

// StatFile describes one remote path.
func (s *server) StatFile(ctx context.Context, request StatFileRequestObject) (StatFileResponseObject, error) {
	entry, err := s.deps.Files.Stat(ctx, target(ctx, request.WorkspaceId, request.SiteId), request.Params.Path)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return StatFile200JSONResponse(remoteEntryDTO(entry)), nil
}

// CreateDirectory creates a remote directory.
func (s *server) CreateDirectory(ctx context.Context, request CreateDirectoryRequestObject) (CreateDirectoryResponseObject, error) {
	entry, err := s.deps.Files.Mkdir(ctx, target(ctx, request.WorkspaceId, request.SiteId), request.Body.Path)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return CreateDirectory201JSONResponse(remoteEntryDTO(entry)), nil
}

// RenameFile moves a remote file or directory.
func (s *server) RenameFile(ctx context.Context, request RenameFileRequestObject) (RenameFileResponseObject, error) {
	entry, err := s.deps.Files.Rename(ctx, target(ctx, request.WorkspaceId, request.SiteId), request.Body.From, request.Body.To)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return RenameFile200JSONResponse(remoteEntryDTO(entry)), nil
}

// DeleteFile removes a remote file or directory.
func (s *server) DeleteFile(ctx context.Context, request DeleteFileRequestObject) (DeleteFileResponseObject, error) {
	recursive := request.Params.Recursive != nil && *request.Params.Recursive
	err := s.deps.Files.Delete(ctx, target(ctx, request.WorkspaceId, request.SiteId), request.Params.Path, recursive)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return DeleteFile204Response{}, nil
}

// parseRange reads a Range header. Only a single byte range is served; anything else, including several ranges
// (which do not parse as numbers) and a malformed header, is ignored and the whole file is sent, as RFC 9110 allows.
func parseRange(header string) files.Range {
	spec, ok := strings.CutPrefix(header, "bytes=")
	if !ok {
		return files.Range{}
	}
	first, last, ok := strings.Cut(spec, "-")
	if !ok {
		return files.Range{}
	}
	if first == "" {
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n < 0 {
			return files.Range{}
		}
		if n == 0 {
			return files.Range{Set: true, Start: -1}
		}
		return files.Range{Set: true, Suffix: n}
	}
	start, err := strconv.ParseInt(first, 10, 64)
	if err != nil || start < 0 {
		return files.Range{}
	}
	if last == "" {
		return files.Range{Set: true, Start: start, End: -1}
	}
	end, err := strconv.ParseInt(last, 10, 64)
	if err != nil || end < start {
		return files.Range{}
	}
	return files.Range{Set: true, Start: start, End: end}
}

// attachment renders the Content-Disposition of a download. The file is always an attachment: what the remote
// server holds is never shown as a page of this origin. Names with control characters or non-ASCII text are
// encoded as RFC 2231 extended parameters, so a name cannot inject headers.
func attachment(name string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}

// problemCode returns the stable code the client sees for err, for the record of a failed transfer.
func problemCode(err error) string {
	if problem, ok := asProblem(err); ok {
		return problem.code
	}
	return "internal_error"
}

// siteOf returns the identifier and the name to record for the site a request works on: those of a saved site, or
// the nil identifier and the host for a quick connection, which is not stored anywhere.
func (s *server) siteOf(ctx context.Context, t files.Target) (uuid.UUID, string, error) {
	if q, ok := s.deps.Quick.Describe(t.Actor, t.Workspace, t.Site); ok {
		return uuid.Nil, q.Name, nil
	}
	found, err := s.deps.Sites.Get(ctx, t.Actor, t.Workspace, t.Site)
	return found.ID, found.Name, err
}

// track records a transfer of the site's file. The record is created only after the request was authorized, so
// refused requests leave no trace.
func (s *server) track(ctx context.Context, t files.Target, direction transfer.Direction, p string, id *TransferIdQuery, total, offset int64) (*transfer.Tracker, context.Context, error) {
	siteID, name, err := s.siteOf(ctx, t)
	if err != nil {
		return nil, nil, err
	}
	in := transfer.BeginInput{
		UserID: t.Actor, WorkspaceID: t.Workspace, SiteID: siteID, SiteName: name, Direction: direction,
		Path: p, Name: path.Base(p), Total: total, Offset: offset,
	}
	if id != nil {
		in.ID = uuid.UUID(*id)
	}
	return s.deps.Transfers.Begin(ctx, in)
}

// DownloadFile streams a remote file, or a range of it. Whole downloads, and ranges that name a transfer, are
// recorded as transfers; other ranges, such as a browser seeking in a video, are not.
func (s *server) DownloadFile(ctx context.Context, request DownloadFileRequestObject) (DownloadFileResponseObject, error) {
	var requested files.Range
	if request.Params.Range != nil {
		requested = parseRange(*request.Params.Range)
	}
	t := target(ctx, request.WorkspaceId, request.SiteId)

	var tracker *transfer.Tracker
	if !requested.Set || request.Params.TransferId != nil {
		if err := s.deps.Files.Check(ctx, t, workspace.ActionDownload, request.Params.Path); err != nil {
			return downloadFailure(err)
		}
		var err error
		if tracker, ctx, err = s.track(ctx, t, transfer.Download, request.Params.Path, request.Params.TransferId, -1, 0); err != nil {
			return downloadFailure(err)
		}
		t.Observer = tracker
	}

	d, err := s.deps.Files.Download(ctx, t, request.Params.Path, requested)
	if err != nil {
		if tracker != nil {
			tracker.Finish(err, problemCode(err))
		}
		return downloadFailure(err)
	}
	var id *uuid.UUID
	if tracker != nil {
		d.Body = tracker.Download(d.Body, problemCode)
		trackedID := tracker.ID()
		id = &trackedID
	}
	ranges, disposition, modified := "bytes", attachment(d.Entry.Name), d.Entry.ModTime.UTC().Format(http.TimeFormat)
	if !d.Partial {
		return DownloadFile200ApplicationoctetStreamResponse{
			Body: d.Body, ContentLength: d.Length(),
			Headers: DownloadFile200ResponseHeaders{AcceptRanges: &ranges, ContentDisposition: &disposition, LastModified: &modified, XTransferId: id},
		}, nil
	}
	contentRange := "bytes " + strconv.FormatInt(d.Start, 10) + "-" + strconv.FormatInt(d.End, 10) + "/" + strconv.FormatInt(d.Entry.Size, 10)
	return DownloadFile206ApplicationoctetStreamResponse{
		Body: d.Body, ContentLength: d.Length(),
		Headers: DownloadFile206ResponseHeaders{AcceptRanges: &ranges, ContentDisposition: &disposition, ContentRange: &contentRange, LastModified: &modified, XTransferId: id},
	}, nil
}

// downloadFailure answers a download that could not start: with its problem, or as an internal error.
func downloadFailure(err error) (DownloadFileResponseObject, error) {
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	return nil, err
}

// uploadModes maps the public upload modes to write modes.
var uploadModes = map[UploadFileParamsMode]remotefs.WriteMode{
	"create":    remotefs.WriteCreate,
	"overwrite": remotefs.WriteOverwrite,
	"resume":    remotefs.WriteResume,
}

// uploadFailure answers an upload that could not start or finish: with its problem, or as an internal error.
func uploadFailure(err error) (UploadFileResponseObject, error) {
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	return nil, err
}

// UploadFile streams the request body to a remote file and records the transfer.
func (s *server) UploadFile(ctx context.Context, request UploadFileRequestObject) (UploadFileResponseObject, error) {
	mode, offset, size := remotefs.WriteCreate, int64(0), int64(-1)
	if m := request.Params.Mode; m != nil {
		var ok bool
		if mode, ok = uploadModes[*m]; !ok {
			return domainProblem{status: http.StatusUnprocessableEntity, code: "invalid_mode"}, nil
		}
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	if request.Params.Size != nil {
		size = *request.Params.Size
	}
	t := target(ctx, request.WorkspaceId, request.SiteId)
	if err := s.deps.Files.Check(ctx, t, workspace.ActionUpload, request.Params.Path); err != nil {
		return uploadFailure(err)
	}
	tracker, runCtx, err := s.track(ctx, t, transfer.Upload, request.Params.Path, request.Params.TransferId, size, offset)
	if err != nil {
		return uploadFailure(err)
	}
	t.Observer = tracker
	entry, err := s.deps.Files.Upload(runCtx, t, request.Params.Path, mode, offset, request.Body)
	tracker.Finish(err, problemCode(err))
	if err != nil {
		return uploadFailure(err)
	}
	id := tracker.ID()
	return UploadFile201JSONResponse{Body: remoteEntryDTO(entry), Headers: UploadFile201ResponseHeaders{XTransferId: &id}}, nil
}
