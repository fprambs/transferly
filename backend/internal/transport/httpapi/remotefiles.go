package httpapi

import (
	"context"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/activity"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/transfer"
	"github.com/fprambs/transferly/internal/core/workspace"
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

// ListFiles lists a remote directory, telling the connection log of the site the folder it reads once the connection is
// open, and how that went (ADR-0011): listed, failed with its code, or canceled because the client gave up the request.
func (s *server) ListFiles(ctx context.Context, request ListFilesRequestObject) (ListFilesResponseObject, error) {
	t := target(ctx, request.WorkspaceId, request.SiteId)
	logTarget := connlog.Target{UserID: t.Actor, WorkspaceID: t.Workspace, SiteID: t.Site}
	t.Ready = func() {
		s.deps.ConnectionLog.Record(logTarget, connlog.Info, connlog.Listing, map[string]string{"path": request.Params.Path})
	}
	entries, err := s.deps.Files.List(ctx, t, request.Params.Path)
	switch {
	case err != nil && ctx.Err() != nil:
		s.deps.ConnectionLog.Record(logTarget, connlog.Warn, connlog.Canceled, map[string]string{"path": request.Params.Path})
	case err != nil:
		s.deps.ConnectionLog.Record(logTarget, connlog.Error, connlog.Failed, map[string]string{"path": request.Params.Path, "code": problemCode(err)})
	default:
		s.deps.ConnectionLog.Record(logTarget, connlog.Info, connlog.Listed, map[string]string{"path": request.Params.Path, "count": strconv.Itoa(len(entries))})
	}
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

// audit adds what the user did on a connection to the activity log, with the outcome as a stable code. A connection that cannot
// be described, and a log that cannot be written, leave no entry: they must not fail what was done.
func (s *server) audit(ctx context.Context, t files.Target, kind activity.Kind, p, detail string, err error) {
	siteID, name, _, siteErr := s.siteOf(ctx, t)
	if siteErr != nil {
		return
	}
	code := ""
	if err != nil {
		code = problemCode(err)
	}
	_ = s.deps.Activity.Record(ctx, activity.Entry{
		UserID: t.Actor, WorkspaceID: t.Workspace, SiteID: siteID, SiteName: name, Kind: kind, Path: p, Detail: detail, ErrorCode: code,
	})
}

// CreateDirectory creates a remote directory.
func (s *server) CreateDirectory(ctx context.Context, request CreateDirectoryRequestObject) (CreateDirectoryResponseObject, error) {
	t := target(ctx, request.WorkspaceId, request.SiteId)
	entry, err := s.deps.Files.Mkdir(ctx, t, request.Body.Path)
	s.audit(ctx, t, activity.Mkdir, request.Body.Path, "", err)
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
	t := target(ctx, request.WorkspaceId, request.SiteId)
	entry, err := s.deps.Files.Rename(ctx, t, request.Body.From, request.Body.To)
	s.audit(ctx, t, activity.Rename, request.Body.From, request.Body.To, err)
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
	t := target(ctx, request.WorkspaceId, request.SiteId)
	err := s.deps.Files.Delete(ctx, t, request.Params.Path, recursive)
	s.audit(ctx, t, activity.Delete, request.Params.Path, "", err)
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

// siteOf returns the identifier, the name to record and the speed limit for the site a request works on: those of a saved
// site, or the nil identifier, the host and no limit for a quick connection, which is not stored anywhere.
func (s *server) siteOf(ctx context.Context, t files.Target) (uuid.UUID, string, int64, error) {
	if q, ok := s.deps.Quick.Describe(t.Actor, t.Workspace, t.Site); ok {
		return uuid.Nil, q.Name, 0, nil
	}
	found, err := s.deps.Sites.Get(ctx, t.Actor, t.Workspace, t.Site)
	return found.ID, found.Name, found.SpeedLimit, err
}

// track records a transfer of the site's file, shown under label. The record is created only after the request was authorized, so
// refused requests leave no trace.
func (s *server) track(ctx context.Context, t files.Target, direction transfer.Direction, p, label string, id *TransferIdQuery, total, offset int64) (*transfer.Tracker, context.Context, error) {
	siteID, name, speedLimit, err := s.siteOf(ctx, t)
	if err != nil {
		return nil, nil, err
	}
	in := transfer.BeginInput{
		UserID: t.Actor, WorkspaceID: t.Workspace, SiteID: siteID, Route: t.Site, RouteBandwidth: speedLimit, SiteName: name, Direction: direction,
		Path: p, Name: label, Total: total, Offset: offset,
	}
	if id != nil {
		in.ID = uuid.UUID(*id)
	}
	tracker, runCtx, err := s.deps.Transfers.Begin(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	if err := tracker.Admit(runCtx); err != nil {
		tracker.Finish(err, problemCode(err))
		return nil, nil, err
	}
	return tracker, runCtx, nil
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
		if tracker, ctx, err = s.track(ctx, t, transfer.Download, request.Params.Path, path.Base(request.Params.Path), request.Params.TransferId, -1, 0); err != nil {
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
	if offset < 0 {
		return domainProblem{status: http.StatusUnprocessableEntity, code: "invalid_offset"}, nil
	}
	if request.Params.Size != nil && size < 0 {
		return domainProblem{status: http.StatusUnprocessableEntity, code: "invalid_size"}, nil
	}
	t := target(ctx, request.WorkspaceId, request.SiteId)
	if err := s.deps.Files.Check(ctx, t, workspace.ActionUpload, request.Params.Path); err != nil {
		return uploadFailure(err)
	}
	tracker, runCtx, err := s.track(ctx, t, transfer.Upload, request.Params.Path, path.Base(request.Params.Path), request.Params.TransferId, size, offset)
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

// DownloadArchive streams a remote folder as a ZIP file and records the transfer.
func (s *server) DownloadArchive(ctx context.Context, request DownloadArchiveRequestObject) (DownloadArchiveResponseObject, error) {
	t := target(ctx, request.WorkspaceId, request.SiteId)
	if err := s.deps.Files.Check(ctx, t, workspace.ActionDownload, request.Params.Path); err != nil {
		return archiveFailure(err)
	}
	tracker, runCtx, err := s.track(ctx, t, transfer.Download, request.Params.Path, path.Base(request.Params.Path)+".zip", request.Params.TransferId, -1, 0)
	if err != nil {
		return archiveFailure(err)
	}
	t.Observer = tracker
	archive, err := s.deps.Files.Archive(runCtx, t, request.Params.Path)
	if err != nil {
		tracker.Finish(err, problemCode(err))
		return archiveFailure(err)
	}
	id := tracker.ID()
	disposition := attachment(archive.Name + ".zip")
	return DownloadArchive200ApplicationzipResponse{
		Body:    tracker.Stream(archive.Body, problemCode),
		Headers: DownloadArchive200ResponseHeaders{ContentDisposition: &disposition, XTransferId: &id},
	}, nil
}

// archiveFailure answers an archive that could not start: with its problem, or as an internal error.
func archiveFailure(err error) (DownloadArchiveResponseObject, error) {
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	return nil, err
}

// GetChecksum computes the digest of a remote file.
func (s *server) GetChecksum(ctx context.Context, request GetChecksumRequestObject) (GetChecksumResponseObject, error) {
	algorithm := "sha256"
	if request.Params.Algorithm != nil {
		algorithm = string(*request.Params.Algorithm)
	}
	t := target(ctx, request.WorkspaceId, request.SiteId)
	sum, err := s.deps.Files.Checksum(ctx, t, request.Params.Path, algorithm)
	s.audit(ctx, t, activity.Checksum, request.Params.Path, algorithm, err)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return GetChecksum200JSONResponse{Algorithm: ChecksumAlgorithm(sum.Algorithm), Checksum: sum.Sum, Size: sum.Size}, nil
}

// listOffset returns how far from UTC the listing times of a connection are read today, from the zone it has.
func (s *server) listOffset(ctx context.Context, t files.Target) (time.Duration, error) {
	found, ok := s.deps.Quick.Describe(t.Actor, t.Workspace, t.Site)
	if !ok {
		var err error
		if found, err = s.deps.Sites.Get(ctx, t.Actor, t.Workspace, t.Site); err != nil {
			return 0, err
		}
	}
	location := time.UTC
	if loaded, err := time.LoadLocation(found.ListTimezone); err == nil {
		location = loaded
	}
	_, seconds := time.Now().In(location).Zone()
	return time.Duration(seconds) * time.Second, nil
}

// suggestedZone names an IANA zone with a whole number of hours as offset, or "" when there is none. The signs of the Etc zones are inverted.
func suggestedZone(offset time.Duration) string {
	hours := int(offset / time.Hour)
	switch {
	case offset%time.Hour != 0:
		return ""
	case hours == 0:
		return "UTC"
	case hours > 0:
		return "Etc/GMT-" + strconv.Itoa(hours)
	default:
		return "Etc/GMT+" + strconv.Itoa(-hours)
	}
}

// ProbeTimeZone finds the time zone of the listings of an FTP server by writing and reading back a small file.
func (s *server) ProbeTimeZone(ctx context.Context, request ProbeTimeZoneRequestObject) (ProbeTimeZoneResponseObject, error) {
	t := target(ctx, request.WorkspaceId, request.SiteId)
	current, err := s.listOffset(ctx, t)
	if err != nil {
		return probeFailure(err)
	}
	probe, err := s.deps.Files.ProbeTimeOffset(ctx, t, request.Params.Path, current)
	if err != nil {
		return probeFailure(err)
	}
	return probeResponse(probe), nil
}

// probeFailure answers a probe that could not run: with its problem, or as an internal error.
func probeFailure(err error) (ProbeTimeZoneResponseObject, error) {
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	return nil, err
}

// probeResponse describes what a probe learned, suggesting a zone only when the times are not exact.
func probeResponse(probe files.TimeProbe) ProbeTimeZone200JSONResponse {
	out := ProbeTimeZone200JSONResponse{Exact: probe.Exact, OffsetMinutes: int(probe.Offset / time.Minute)}
	if zone := suggestedZone(probe.Offset); !probe.Exact && zone != "" {
		out.SuggestedZone = &zone
	}
	return out
}

// DisconnectSite closes the pooled connections of the caller to a site or quick connection, and tells the connection
// log of the site. A site the caller cannot see is not found.
func (s *server) DisconnectSite(ctx context.Context, request DisconnectSiteRequestObject) (DisconnectSiteResponseObject, error) {
	t := target(ctx, request.WorkspaceId, request.SiteId)
	_, _, _, err := s.siteOf(ctx, t)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.deps.Connections.CloseKey(connections.Key{UserID: t.Actor, WorkspaceID: t.Workspace, SiteID: t.Site})
	s.deps.ConnectionLog.Record(connlog.Target{UserID: t.Actor, WorkspaceID: t.Workspace, SiteID: t.Site}, connlog.Info, connlog.Disconnected, map[string]string{})
	return DisconnectSite204Response{}, nil
}

// VisitDisconnectSiteResponse implements [DisconnectSiteResponseObject].
func (p domainProblem) VisitDisconnectSiteResponse(w http.ResponseWriter) error { return p.write(w) }

// ListOpenConnections answers the sites and quick connections of a workspace to which the caller has connections open
// (4.11). A workspace the caller does not belong to is not found.
func (s *server) ListOpenConnections(ctx context.Context, request ListOpenConnectionsRequestObject) (ListOpenConnectionsResponseObject, error) {
	p, _ := principalFrom(ctx)
	_, err := s.deps.Workspaces.Authorize(ctx, p.User.ID, request.WorkspaceId, workspace.ActionBrowse)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return ListOpenConnections200JSONResponse{SiteIds: s.deps.Connections.Open(p.User.ID, request.WorkspaceId)}, nil
}

// VisitListOpenConnectionsResponse implements [ListOpenConnectionsResponseObject].
func (p domainProblem) VisitListOpenConnectionsResponse(w http.ResponseWriter) error {
	return p.write(w)
}
