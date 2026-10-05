package files

import (
	"context"
	"fmt"
	"io"
	"path"
	"sync"

	"github.com/fprambs/transferly/internal/core/archives"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/workspace"
)

const (
	// blockSize is the size of the blocks an archive is read and cached in.
	blockSize = 64 << 10
	// cachedBlocks is how many blocks of an archive stay cached while it is read.
	cachedBlocks = 64
	// maxNesting is the most archives nested in one another a request goes through.
	maxNesting = 4
	// defaultNestedBytes is the most bytes of an archive held in memory that are read: one nested in another, or one on a server whose
	// sizes are not trusted.
	defaultNestedBytes = 64 << 20
)

// WithNestedLimit returns the service with another cap on the bytes of an archive held in memory while it is read: one nested in another,
// or one on a server whose sizes are not trusted, which may always hold as many bytes as a buffered download. It exists for tests and for installations with unusual archives.
func (s *Service) WithNestedLimit(bytes int64) *Service {
	s.nested = bytes
	return s
}

// remoteReader reads a remote file by offset in blocks, keeping the last blocks and the open stream: reading in order streams the file
// once, and the reads of a ZIP or 7z index at its end fetch only that end.
type remoteReader struct {
	ctx    context.Context
	fsys   remotefs.RemoteFS
	path   string
	size   int64
	mu     sync.Mutex
	blocks map[int64][]byte
	order  []int64
	stream io.ReadCloser
	next   int64
	err    error
}

// ReadAt implements [io.ReaderAt].
func (r *remoteReader) ReadAt(p []byte, off int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n := 0
	for n < len(p) && off+int64(n) < r.size {
		at := off + int64(n)
		block, err := r.block(at / blockSize)
		if err != nil {
			return n, err
		}
		n += copy(p[n:], block[at%blockSize:])
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// block returns a block of the file, from the cache, from the open stream when it is next, or from a stream opened at it.
func (r *remoteReader) block(index int64) ([]byte, error) {
	if cached, ok := r.blocks[index]; ok {
		return cached, nil
	}
	start := index * blockSize
	if r.stream == nil || r.next != start {
		r.closeStream()
		stream, err := r.fsys.OpenRead(r.ctx, r.path, start)
		if err != nil {
			r.err = err
			return nil, err
		}
		r.stream, r.next = stream, start
	}
	data := make([]byte, min(blockSize, r.size-start))
	if _, err := io.ReadFull(r.stream, data); err != nil {
		r.err = err
		r.closeStream()
		return nil, err
	}
	r.next += int64(len(data))
	if len(r.order) == cachedBlocks {
		delete(r.blocks, r.order[0])
		r.order = r.order[1:]
	}
	r.blocks[index], r.order = data, append(r.order, index)
	return data, nil
}

// closeStream closes the open stream, if any.
func (r *remoteReader) closeStream() {
	if r.stream != nil {
		_ = r.stream.Close()
		r.stream = nil
	}
}

// openedArchive is an archive being read on a borrowed connection: a remote file read by offset or held in memory, or an archive
// nested in it and held in memory.
type openedArchive struct {
	src    archives.Source
	name   string
	lease  *connections.Lease
	remote *remoteReader
}

// release closes the remote file and gives the connection back, discarded when reading the file failed as a lost connection.
func (o *openedArchive) release() {
	if o.remote == nil {
		o.lease.Release(nil)
		return
	}
	o.remote.mu.Lock()
	defer o.remote.mu.Unlock()
	o.remote.closeStream()
	o.lease.Release(o.remote.err)
}

// openArchive borrows a connection, checks the archive is a file and opens it, going down the chain of nested archives in inner. A small
// archive, or one on a server whose sizes are not trusted, is read whole into memory; a larger one is read by offset. Each nested archive
// is read into memory, up to its cap.
func (s *Service) openArchive(ctx context.Context, t Target, p string, inner []string, names ...string) (*openedArchive, error) {
	if len(inner) > maxNesting {
		return nil, fmt.Errorf("%w: archives nest deeper than %d", archives.ErrTooLarge, maxNesting)
	}
	lease, err := s.begin(ctx, t, workspace.ActionDownload, append(append([]string{p}, inner...), names...)...)
	if err != nil {
		return nil, err
	}
	entry, err := lease.FS().Stat(ctx, p)
	if err == nil && entry.IsDir() {
		err = fmt.Errorf("%w: %s is a directory", remotefs.ErrInvalid, p)
	}
	if err != nil {
		lease.Release(err)
		return nil, err
	}
	opened := &openedArchive{name: p, lease: lease}
	if entry.Size <= s.buffered || !remotefs.SizesTrusted(lease.FS()) || !sizeHolds(ctx, lease.FS(), p, entry.Size) {
		data, err := readWhole(ctx, lease.FS(), p, entry.Size, max(s.nested, s.buffered))
		if err != nil {
			lease.Release(err)
			return nil, err
		}
		opened.src = archives.Bytes(data)
	} else {
		opened.remote = &remoteReader{ctx: ctx, fsys: lease.FS(), path: p, size: entry.Size, blocks: map[int64][]byte{}}
		opened.src = archives.FromReaderAt(opened.remote, entry.Size)
	}
	for _, name := range inner {
		data, err := readNested(opened.src, opened.name, name, s.nested)
		if err != nil {
			opened.release()
			return nil, err
		}
		opened.src, opened.name = archives.Bytes(data), name
	}
	return opened, nil
}

// readWhole reads a remote archive into memory up to the end of the file, whatever size the server reported, refusing one over limit
// bytes, and tells the connection how many bytes came. A ZIP or 7z index sits at the end of the file, so it is found where the bytes
// end and not where a wrong size says.
func readWhole(ctx context.Context, fsys remotefs.RemoteFS, p string, reported, limit int64) ([]byte, error) {
	if reported > limit {
		return nil, archives.ErrTooLarge
	}
	rc, err := fsys.OpenRead(ctx, p, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(&countingReader{ctx: ctx, src: io.LimitReader(rc, limit+1)})
	if err != nil {
		return nil, err
	}
	remotefs.NoteWholeRead(fsys, reported, int64(len(data)))
	if int64(len(data)) > limit {
		return nil, archives.ErrTooLarge
	}
	return data, nil
}

// readNested reads an archive nested in another into memory, refusing one over limit bytes.
func readNested(src archives.Source, archive, name string, limit int64) ([]byte, error) {
	rc, size, err := archives.Open(src, archive, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	if size > limit {
		return nil, archives.ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, archives.ErrTooLarge
	}
	return data, nil
}

// ArchiveEntries lists the entries of the archive at p or, when inner names a chain of archives each nested in the one before, of the
// last of them. It needs the download right, since the listing reads the file (ADR-0018).
func (s *Service) ArchiveEntries(ctx context.Context, t Target, p string, inner []string) (archives.Listing, error) {
	opened, err := s.openArchive(ctx, t, p, inner)
	if err != nil {
		return archives.Listing{}, err
	}
	defer opened.release()
	return archives.List(opened.src, opened.name)
}

// ArchiveEntry is an open file entry of an archive. Closing its body gives the connection back.
type ArchiveEntry struct {
	// Name is the base name of the entry.
	Name string
	// Size is its size once extracted, or -1 when the archive does not tell it.
	Size int64
	// Body is the stream of its bytes; the caller closes it.
	Body io.ReadCloser
}

// entryBody reads an entry of an archive and gives the connection back when closed.
type entryBody struct {
	io.ReadCloser
	opened *openedArchive
	once   sync.Once
}

// Close implements [io.Closer].
func (b *entryBody) Close() error {
	var err error
	b.once.Do(func() {
		err = b.ReadCloser.Close()
		b.opened.release()
	})
	return err
}

// OpenArchiveEntry opens a file entry of the archive at p, or of the last archive of the chain in inner, for reading. Its bytes are capped
// at the size the archive declares, and the caps of [archives] apply.
func (s *Service) OpenArchiveEntry(ctx context.Context, t Target, p string, inner []string, entry string) (*ArchiveEntry, error) {
	opened, err := s.openArchive(ctx, t, p, inner, entry)
	if err != nil {
		return nil, err
	}
	rc, size, err := archives.Open(opened.src, opened.name, entry)
	if err != nil {
		opened.release()
		return nil, err
	}
	return &ArchiveEntry{Name: path.Base(entry), Size: size, Body: &entryBody{ReadCloser: rc, opened: opened}}, nil
}
