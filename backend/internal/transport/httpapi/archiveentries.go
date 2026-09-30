package httpapi

import (
	"bufio"
	"context"
	"errors"
	"io"

	"github.com/fprambs/sentinel/internal/core/archives"
)

// innerChain returns the chain of nested archives of a request, or none.
func innerChain(inner *[]string) []string {
	if inner == nil {
		return nil
	}
	return *inner
}

// archiveEntryDTO converts an entry of an archive to its public form; a missing time is left out.
func archiveEntryDTO(e archives.Entry) ArchiveEntry {
	dto := ArchiveEntry{Name: e.Name, Type: ArchiveEntryType(e.Type), Size: e.Size, Packed: e.Packed}
	if !e.Modified.IsZero() {
		modified := e.Modified.UTC()
		dto.Modified = &modified
	}
	return dto
}

// ListArchiveEntries lists the entries of a remote archive, or of an archive nested in it (ADR-0018).
func (s *server) ListArchiveEntries(ctx context.Context, request ListArchiveEntriesRequestObject) (ListArchiveEntriesResponseObject, error) {
	t := target(ctx, request.WorkspaceId, request.SiteId)
	listing, err := s.deps.Files.ArchiveEntries(ctx, t, request.Params.Path, innerChain(request.Params.Inner))
	if err != nil {
		return problemOr[ListArchiveEntriesResponseObject](err)
	}
	entries := make([]ArchiveEntry, len(listing.Entries))
	for i, e := range listing.Entries {
		entries[i] = archiveEntryDTO(e)
	}
	return ListArchiveEntries200JSONResponse{Format: ArchiveListingFormat(listing.Format), Entries: entries, Truncated: listing.Truncated}, nil
}

// DownloadArchiveEntry streams one file entry of a remote archive. Its first bytes are read before answering, so an entry that turns out
// to be encrypted or damaged only once decoded, as 7-Zip entries do, is answered with its problem instead of a broken download.
func (s *server) DownloadArchiveEntry(ctx context.Context, request DownloadArchiveEntryRequestObject) (DownloadArchiveEntryResponseObject, error) {
	t := target(ctx, request.WorkspaceId, request.SiteId)
	entry, err := s.deps.Files.OpenArchiveEntry(ctx, t, request.Params.Path, innerChain(request.Params.Inner), request.Params.Entry)
	if err != nil {
		return problemOr[DownloadArchiveEntryResponseObject](err)
	}
	peeked := bufio.NewReader(entry.Body)
	if _, err := peeked.Peek(1); err != nil && !errors.Is(err, io.EOF) {
		_ = entry.Body.Close()
		return problemOr[DownloadArchiveEntryResponseObject](err)
	}
	disposition := attachment(entry.Name)
	return DownloadArchiveEntry200ApplicationoctetStreamResponse{
		Body: struct {
			io.Reader
			io.Closer
		}{peeked, entry.Body},
		ContentLength: max(entry.Size, 0),
		Headers:       DownloadArchiveEntry200ResponseHeaders{ContentDisposition: &disposition},
	}, nil
}

// problemOr answers a request that failed: with the problem of its error, as a response of type R, or as an internal error.
func problemOr[R any](err error) (R, error) {
	if problem, ok := asProblem(err); ok {
		return any(problem).(R), nil
	}
	var zero R
	return zero, err
}
