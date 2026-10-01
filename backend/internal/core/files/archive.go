package files

import (
	"archive/zip"
	"context"
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

const (
	// defaultTreeDepth is how deep a tree may go before an archive or a copy of it is refused; it also stops loops through links.
	defaultTreeDepth = 64
	// defaultTreeEntries is how many files and folders an archive or a copy of a tree may hold.
	defaultTreeEntries = 100_000
)

// ErrTreeTooLarge means a tree holds more entries, or goes deeper, than an archive or a copy may handle.
var ErrTreeTooLarge = errors.New("tree too large")

// treeLimits are the bounds of a walk over a remote tree.
type treeLimits struct {
	depth   int
	entries int
}

// WithTreeLimits returns the service with other bounds for the trees it archives and copies: how many levels deep and how
// many entries in total. It exists for tests and for installations with unusual trees.
func (s *Service) WithTreeLimits(depth, entries int) *Service {
	s.limits = treeLimits{depth: depth, entries: entries}
	return s
}

// treeWalk counts what a walk over a tree visited and stops it at the bounds.
type treeWalk struct {
	limits treeLimits
	count  int
}

// enter checks that a folder at a depth may be entered.
func (w *treeWalk) enter(depth int) error {
	if depth > w.limits.depth {
		return fmt.Errorf("%w: deeper than %d levels", ErrTreeTooLarge, w.limits.depth)
	}
	return nil
}

// add counts one more entry and checks the total.
func (w *treeWalk) add() error {
	if w.count++; w.count > w.limits.entries {
		return fmt.Errorf("%w: more than %d entries", ErrTreeTooLarge, w.limits.entries)
	}
	return nil
}

// Archive is a folder of a remote site streamed as a ZIP file.
type Archive struct {
	// Name is the name of the folder, which names the archive.
	Name string
	// Body is the ZIP stream; the caller closes it, which gives the connection back.
	Body io.ReadCloser
}

// archiveBody is the reading end of the pipe the archive is written to.
type archiveBody struct {
	reader *io.PipeReader
	done   chan error
	lease  *connections.Lease
	closed bool
}

// Read reads the ZIP as it is produced.
func (a *archiveBody) Read(p []byte) (int, error) { return a.reader.Read(p) }

// Close stops the archive, waits for the writer and gives the connection back; a failure of the connection discards it.
func (a *archiveBody) Close() error {
	if a.closed {
		return nil
	}
	a.closed = true
	_ = a.reader.Close()
	err := <-a.done
	if errors.Is(err, io.ErrClosedPipe) {
		err = nil
	}
	a.lease.Release(err)
	return nil
}

// Archive streams the files of a folder and of its subfolders as a ZIP. Links and other special files are left out, names
// that could climb out of the folder when unpacked are refused, and a tree beyond the limits fails with
// [ErrTreeTooLarge]. The observer follows the bytes read from the server.
func (s *Service) Archive(ctx context.Context, t Target, dir string) (*Archive, error) {
	lease, err := s.begin(ctx, t, workspace.ActionDownload, dir)
	if err != nil {
		return nil, err
	}
	entry, err := lease.FS().Stat(ctx, dir)
	if err == nil && !entry.IsDir() {
		err = fmt.Errorf("%w: %s", remotefs.ErrNotDirectory, dir)
	}
	if err != nil {
		lease.Release(err)
		return nil, err
	}
	started(t.Observer, -1)
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := writeArchive(ctx, lease.FS(), dir, writer, t.Observer, s.limits)
		_ = writer.CloseWithError(err)
		done <- err
	}()
	name := path.Base(dir)
	if name == "/" || name == "." {
		name = "archive"
	}
	return &Archive{Name: name, Body: &archiveBody{reader: reader, done: done, lease: lease}}, nil
}

// writeArchive writes the tree below dir as a ZIP.
func writeArchive(ctx context.Context, fsys remotefs.RemoteFS, dir string, out io.Writer, obs Observer, limits treeLimits) error {
	w := zip.NewWriter(out)
	if err := addTree(ctx, fsys, w, dir, "", 0, &treeWalk{limits: limits}, obs); err != nil {
		return err
	}
	return w.Close()
}

// safeName tells whether a name is one plain element that cannot lead out of its folder.
func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00")
}

// addTree adds the entries of a folder to the archive under a prefix.
func addTree(ctx context.Context, fsys remotefs.RemoteFS, w *zip.Writer, dir, prefix string, depth int, walk *treeWalk, obs Observer) error {
	if err := walk.enter(depth); err != nil {
		return err
	}
	entries, err := fsys.List(ctx, dir)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for _, entry := range entries {
		if !safeName(entry.Name) || (!entry.Mode.IsDir() && !entry.Mode.IsRegular()) {
			continue
		}
		if err := walk.add(); err != nil {
			return err
		}
		child := path.Join(dir, entry.Name)
		if entry.IsDir() {
			header := &zip.FileHeader{Name: prefix + entry.Name + "/", Method: zip.Store, Modified: entry.ModTime}
			if _, err := w.CreateHeader(header); err != nil {
				return err
			}
			if err := addTree(ctx, fsys, w, child, prefix+entry.Name+"/", depth+1, walk, obs); err != nil {
				return err
			}
			continue
		}
		if err := addFile(ctx, fsys, w, child, prefix+entry.Name, entry, obs); err != nil {
			return err
		}
	}
	return nil
}

// addFile copies a file into the archive.
func addFile(ctx context.Context, fsys remotefs.RemoteFS, w *zip.Writer, remote, name string, entry remotefs.Entry, obs Observer) error {
	src, err := fsys.OpenRead(ctx, remote, 0)
	if err != nil {
		return err
	}
	dst, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: entry.ModTime})
	if err == nil {
		_, err = io.Copy(dst, &countingReader{ctx: ctx, src: src, obs: obs})
	}
	if closeErr := src.Close(); err == nil {
		err = closeErr
	}
	return err
}
