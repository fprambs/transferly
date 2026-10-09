package files

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/workspace"
)

// ErrCopyConnectionLimit means a streamed same-site copy cannot reserve both its reader and writer.
var ErrCopyConnectionLimit = errors.New("copy requires two connections")

// Policy says what a copy does with a file that already exists at the destination.
type Policy int

const (
	// PolicyFail stops the copy with [remotefs.ErrExists].
	PolicyFail Policy = iota
	// PolicyOverwrite replaces the file.
	PolicyOverwrite
	// PolicySkip leaves the file as it is and moves on.
	PolicySkip
	// PolicyNewer replaces the file only when the source was modified after it.
	PolicyNewer
	// PolicyRename keeps both: the copy gets a free name such as "report (1).txt".
	PolicyRename
)

// maxRenameAttempts is how many names PolicyRename tries before giving up.
const maxRenameAttempts = 1000

// CopyLocation says where the two ends of a copy are.
type CopyLocation struct {
	// Source is the site the file or folder is read from.
	Source Target
	// SourcePath is the path of the file or folder to copy.
	SourcePath string
	// Destination is the site the copy is written to; its observer follows the copy.
	Destination Target
	// DestinationPath is the path of the copy, including its name.
	DestinationPath string
	// Verify reads every copied file back and compares its SHA-256 with the bytes that were sent; a difference fails the copy
	// with [ErrChecksumMismatch].
	Verify bool
}

// sameFile reports whether both ends of a copy name the same file.
func (l CopyLocation) sameFile() bool {
	return l.Source.Site == l.Destination.Site && path.Clean(l.SourcePath) == path.Clean(l.DestinationPath)
}

// intoItself reports whether the destination lies inside the source, which would copy a folder into itself forever.
func (l CopyLocation) intoItself() bool {
	return l.Source.Site == l.Destination.Site && strings.HasPrefix(path.Clean(l.DestinationPath), strings.TrimSuffix(path.Clean(l.SourcePath), "/")+"/")
}

// CheckCopy verifies, before any data moves, that the user may read the source and write the destination and that the
// paths are usable. Copying onto itself, or a folder into itself, is refused.
func (s *Service) CheckCopy(ctx context.Context, l CopyLocation) error {
	if err := s.Check(ctx, l.Source, workspace.ActionDownload, l.SourcePath); err != nil {
		return err
	}
	if err := s.Check(ctx, l.Destination, workspace.ActionUpload, l.DestinationPath); err != nil {
		return err
	}
	if l.sameFile() || l.intoItself() {
		return fmt.Errorf("%w: a file or folder cannot be copied onto or into itself", remotefs.ErrInvalid)
	}
	return nil
}

// fileObserver passes the bytes of a file to the observer of a copy. The start of the file is announced with its known size
// when the file is the whole copy, and left out when the file is one of many.
type fileObserver struct {
	inner    Observer
	total    int64
	announce bool
}

// Started tells the inner observer the size of the source, which the upload does not know, when the file is the whole copy.
func (o fileObserver) Started(int64) {
	if o.announce {
		started(o.inner, o.total)
	}
}

// Progress forwards the bytes that moved.
func (o fileObserver) Progress(n int64) { progress(o.inner, n) }

// Copy copies a file, or a folder with everything below it, from one site to another or within one. What happens to
// files that already exist at the destination follows the policy; folders that exist are merged into. A copy that fails
// leaves what was copied so far. The entry returned is the copy, or the destination folder.
func (s *Service) Copy(ctx context.Context, l CopyLocation, policy Policy) (remotefs.Entry, error) {
	if err := s.CheckCopy(ctx, l); err != nil {
		return remotefs.Entry{}, err
	}
	release, err := s.copies.pair(ctx, l)
	if err != nil {
		return remotefs.Entry{}, err
	}
	defer release()
	source, err := s.Stat(ctx, l.Source, l.SourcePath)
	if err != nil {
		return remotefs.Entry{}, err
	}
	if !source.IsDir() {
		return s.copyFile(ctx, l, source, policy, l.Destination.Observer, true)
	}
	started(l.Destination.Observer, -1)
	if err := s.copyTree(ctx, l, 0, &treeWalk{limits: s.limits}, policy); err != nil {
		return remotefs.Entry{}, err
	}
	return s.Stat(ctx, l.Destination, l.DestinationPath)
}

// copyTree copies the folder of l into its destination, creating the destination when it does not exist.
func (s *Service) copyTree(ctx context.Context, l CopyLocation, depth int, walk *treeWalk, policy Policy) error {
	if err := walk.enter(depth); err != nil {
		return err
	}
	existing, err := s.Stat(ctx, l.Destination, l.DestinationPath)
	switch {
	case err == nil && !existing.IsDir():
		return fmt.Errorf("%w: %s is a file", remotefs.ErrExists, l.DestinationPath)
	case err == nil:
	case isNotFound(err):
		if _, err := s.Mkdir(ctx, l.Destination, l.DestinationPath); err != nil {
			return err
		}
	default:
		return err
	}
	entries, err := s.List(ctx, l.Source, l.SourcePath)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for _, entry := range entries {
		if !safeName(entry.Name) || (!entry.IsDir() && !entry.Mode.IsRegular()) {
			continue
		}
		if err := walk.add(); err != nil {
			return err
		}
		child := l
		child.SourcePath, child.DestinationPath = path.Join(l.SourcePath, entry.Name), path.Join(l.DestinationPath, entry.Name)
		if entry.IsDir() {
			err = s.copyTree(ctx, child, depth+1, walk, policy)
		} else {
			_, err = s.copyFile(ctx, child, entry, policy, l.Destination.Observer, false)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// freeName returns the first name of the form "name (n).ext" that does not exist in the destination folder.
func (s *Service) freeName(ctx context.Context, t Target, p string) (string, error) {
	ext := path.Ext(p)
	stem := strings.TrimSuffix(p, ext)
	for n := 1; n <= maxRenameAttempts; n++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, n, ext)
		if _, err := s.Stat(ctx, t, candidate); err != nil {
			if isNotFound(err) {
				return candidate, nil
			}
			return "", err
		}
	}
	return "", fmt.Errorf("%w: no free name for %s", remotefs.ErrExists, p)
}

// isNotFound reports whether an error says a path does not exist.
func isNotFound(err error) bool { return errors.Is(err, remotefs.ErrNotFound) }

// copyFile copies one file according to the policy; a skipped file answers with the file that stayed. whole says the copy
// is only this file, whose start then reports its size. Within one site, the server copies the file itself when it can
// (ADR-0027), unless the copy is verified, which needs the bytes.
func (s *Service) copyFile(ctx context.Context, l CopyLocation, source remotefs.Entry, policy Policy, obs Observer, whole bool) (remotefs.Entry, error) {
	mode := remotefs.WriteCreate
	destination := l.DestinationPath
	if policy != PolicyFail {
		existing, err := s.Stat(ctx, l.Destination, destination)
		if err == nil {
			switch {
			case policy == PolicySkip, policy == PolicyNewer && !source.ModTime.After(existing.ModTime):
				return existing, nil
			case policy == PolicyRename:
				if destination, err = s.freeName(ctx, l.Destination, destination); err != nil {
					return remotefs.Entry{}, err
				}
			default:
				mode = remotefs.WriteOverwrite
			}
		} else if !isNotFound(err) {
			return remotefs.Entry{}, err
		}
	}
	if l.Source.Site == l.Destination.Site && !l.Verify {
		done, err := s.serverCopy(ctx, l.Destination, l.SourcePath, destination, mode == remotefs.WriteOverwrite)
		if err != nil {
			return remotefs.Entry{}, err
		}
		if done {
			if whole {
				started(obs, source.Size)
			}
			progress(obs, source.Size)
			return s.Stat(ctx, l.Destination, destination)
		}
	}
	from := l.Source
	from.Observer = nil
	d, err := s.Download(ctx, from, l.SourcePath, Range{})
	if err != nil {
		return remotefs.Entry{}, err
	}
	defer func() { _ = d.Body.Close() }()
	if l.Source.key() == l.Destination.key() {
		if budget, ok := s.pool.(interface {
			MaxConnections(context.Context, connections.Key) int
		}); ok && budget.MaxConnections(ctx, l.Source.key()) < 2 {
			if _, buffered := d.Body.(*bufferedBody); !buffered {
				return remotefs.Entry{}, fmt.Errorf("%w: %w", ErrCopyConnectionLimit, remotefs.ErrUnsupported)
			}
		}
	}
	to := l.Destination
	to.Observer = fileObserver{inner: obs, total: d.Length(), announce: whole}
	body := io.Reader(d.Body)
	sent := sha256.New()
	if l.Verify {
		body = io.TeeReader(d.Body, sent)
	}
	entry, err := s.Upload(ctx, to, destination, mode, 0, body)
	if err == nil && l.Verify {
		err = s.verifyCopy(ctx, l.Destination, destination, sent)
	}
	return entry, err
}
