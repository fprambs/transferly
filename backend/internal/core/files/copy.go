package files

import (
	"context"
	"fmt"

	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// CopyLocation says where the two ends of a copy are.
type CopyLocation struct {
	// Source is the site the file is read from and the path of the file.
	Source Target
	// SourcePath is the path of the file to copy.
	SourcePath string
	// Destination is the site the file is written to; its observer follows the copy.
	Destination Target
	// DestinationPath is the path of the copy.
	DestinationPath string
}

// sameFile reports whether both ends of a copy name the same file.
func (l CopyLocation) sameFile() bool {
	return l.Source.Site == l.Destination.Site && l.SourcePath == l.DestinationPath
}

// CheckCopy verifies, before any data moves, that the user may read the source and write the destination and that the
// paths are usable. Copying a file onto itself is refused.
func (s *Service) CheckCopy(ctx context.Context, l CopyLocation) error {
	if err := s.Check(ctx, l.Source, workspace.ActionDownload, l.SourcePath); err != nil {
		return err
	}
	if err := s.Check(ctx, l.Destination, workspace.ActionUpload, l.DestinationPath); err != nil {
		return err
	}
	if l.sameFile() {
		return fmt.Errorf("%w: a file cannot be copied onto itself", remotefs.ErrInvalid)
	}
	return nil
}

// totalObserver reports a known total to the observer of a destination whose upload does not know it.
type totalObserver struct {
	inner Observer
	total int64
}

// Started tells the inner observer the total of the source instead of the unknown size of an upload.
func (o totalObserver) Started(int64) { started(o.inner, o.total) }

// Progress forwards the bytes that moved.
func (o totalObserver) Progress(n int64) { progress(o.inner, n) }

// Copy streams a file of one site into a file of another, or of the same one, through the server. The destination
// follows mode as an upload does: create refuses an existing file, overwrite replaces it. A copy that fails leaves the
// partial destination for the caller to deal with.
func (s *Service) Copy(ctx context.Context, l CopyLocation, mode remotefs.WriteMode) (remotefs.Entry, error) {
	if err := s.CheckCopy(ctx, l); err != nil {
		return remotefs.Entry{}, err
	}
	source := l.Source
	source.Observer = nil
	d, err := s.Download(ctx, source, l.SourcePath, Range{})
	if err != nil {
		return remotefs.Entry{}, err
	}
	defer func() { _ = d.Body.Close() }()
	destination := l.Destination
	destination.Observer = totalObserver{inner: l.Destination.Observer, total: d.Length()}
	return s.Upload(ctx, destination, l.DestinationPath, mode, 0, d.Body)
}
