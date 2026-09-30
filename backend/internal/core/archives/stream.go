package archives

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"io"

	"github.com/klauspost/compress/zstd"
	"github.com/nwaples/rardecode/v2"
	"github.com/ulikunitz/xz"
)

// counted counts the bytes decompressed from a stream and fails with [ErrTooLarge] past [maxScanBytes], so neither listing a TAR nor
// looking for an entry decompresses without end.
type counted struct {
	r    io.Reader
	read int64
}

// Read implements [io.Reader].
func (c *counted) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += int64(n)
	if c.read > maxScanBytes {
		return n, ErrTooLarge
	}
	return n, err
}

// decompressor opens the decompressed stream of a compressed format, with the memory of xz and zstd bounded; it returns the stream as it
// is for TAR.
func decompressor(format Format, raw io.Reader) (io.Reader, func(), error) {
	data := raw
	done := func() {}
	var err error
	switch format {
	case Gzip, TarGzip:
		var r *gzip.Reader
		r, err = gzip.NewReader(raw)
		data, done = r, func() { _ = r.Close() }
	case Bzip2, TarBzip2:
		data = bzip2.NewReader(raw)
	case Xz, TarXz:
		data, err = xz.ReaderConfig{DictCap: maxWindow}.NewReader(raw)
	case Zstd, TarZstd:
		var r *zstd.Decoder
		r, err = zstd.NewReader(raw, zstd.WithDecoderMaxWindow(maxWindow), zstd.WithDecoderConcurrency(1))
		data, done = r, func() { r.Close() }
	}
	if err != nil {
		return nil, nil, ErrNotArchive
	}
	return data, done, nil
}

// isTar tells whether a format is a TAR, plain or compressed.
func isTar(format Format) bool {
	return format == Tar || format == TarGzip || format == TarBzip2 || format == TarXz || format == TarZstd
}

// opened is a stream of an archive being read, with how to close all of it.
type opened struct {
	data  io.Reader
	close func() error
}

// openData opens the decompressed stream of an archive from its start, counted against [maxScanBytes].
func openData(src Source, format Format) (*opened, error) {
	raw, err := src.Stream(0)
	if err != nil {
		return nil, err
	}
	data, done, err := decompressor(format, raw)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return &opened{data: &counted{r: data}, close: func() error { done(); return raw.Close() }}, nil
}

// tarType tells the type of a TAR entry, or false for entries that are not files, folders or links.
func tarType(flag byte) (EntryType, bool) {
	switch flag {
	case tar.TypeReg:
		return File, true
	case tar.TypeDir:
		return Directory, true
	case tar.TypeSymlink, tar.TypeLink:
		return Symlink, true
	}
	return "", false
}

// rarEntry describes an entry of a RAR archive.
func rarEntry(header *rardecode.FileHeader) Entry {
	entry := Entry{Name: header.Name, Type: File, Size: header.UnPackedSize, Packed: header.PackedSize, Modified: header.ModificationTime}
	if header.IsDir {
		entry.Type = Directory
	}
	if header.UnKnownSize {
		entry.Size = -1
	}
	return entry
}

// tarEntry describes an entry of a TAR archive, or tells false for entries that are not files, folders or links.
func tarEntry(header *tar.Header) (Entry, bool) {
	kind, ok := tarType(header.Typeflag)
	return Entry{Name: header.Name, Type: kind, Size: header.Size, Packed: -1, Modified: header.ModTime}, ok
}

// streamFailure keeps the caps of a failed read, tells an encrypted RAR apart and calls anything else an archive the viewer does not
// read.
func streamFailure(err error) error {
	switch {
	case errors.Is(err, ErrTooLarge):
		return err
	case errors.Is(err, rardecode.ErrArchiveEncrypted), errors.Is(err, rardecode.ErrArchivedFileEncrypted):
		return ErrEncrypted
	}
	return ErrNotArchive
}

// walker steps through the entries of a TAR or a RAR archive, telling each one, whether to skip it, and the reader of its data.
type walker func() (entry Entry, skip bool, data io.Reader, encrypted bool, err error)

// newWalker opens the walker of a TAR or a RAR archive over its decompressed stream.
func newWalker(format Format, data io.Reader) (walker, error) {
	if format == Rar {
		reader, err := rardecode.NewReader(data, rardecode.MaxDictionarySize(maxWindow))
		if err != nil {
			return nil, ErrNotArchive
		}
		return func() (Entry, bool, io.Reader, bool, error) {
			header, err := reader.Next()
			if err != nil {
				return Entry{}, false, nil, false, err
			}
			return rarEntry(header), false, reader, header.Encrypted, nil
		}, nil
	}
	reader := tar.NewReader(data)
	return func() (Entry, bool, io.Reader, bool, error) {
		header, err := reader.Next()
		if err != nil {
			return Entry{}, false, nil, false, err
		}
		entry, ok := tarEntry(header)
		return entry, !ok, reader, false, nil
	}, nil
}

// listStream lists a TAR, a RAR or a compressed single file by reading it from its start.
func listStream(src Source, name string, format Format) ([]Entry, bool, error) {
	stream, err := openData(src, format)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = stream.close() }()
	if format != Rar && !isTar(format) {
		return []Entry{{Name: singleName(name), Type: File, Size: -1, Packed: src.Size()}}, false, nil
	}
	next, err := newWalker(format, stream.data)
	if err != nil {
		return nil, false, err
	}
	var entries []Entry
	for {
		entry, skip, _, _, err := next()
		switch {
		case errors.Is(err, io.EOF):
			return entries, false, nil
		case err != nil:
			return nil, false, streamFailure(err)
		case skip:
			continue
		}
		if add(&entries, entry) {
			return entries, true, nil
		}
	}
}

// openStream reads one file entry of a TAR, a RAR or a compressed single file, reading the archive from its start up to it.
func openStream(src Source, name string, format Format, entry string) (io.ReadCloser, int64, error) {
	stream, err := openData(src, format)
	if err != nil {
		return nil, 0, err
	}
	fail := func(err error) (io.ReadCloser, int64, error) {
		_ = stream.close()
		return nil, 0, err
	}
	if format != Rar && !isTar(format) {
		if entry != singleName(name) {
			return fail(ErrEntryNotFound)
		}
		return &capped{r: stream.data, left: maxEntryBytes, close: stream.close}, -1, nil
	}
	next, err := newWalker(format, stream.data)
	if err != nil {
		return fail(err)
	}
	for {
		found, skip, data, encrypted, err := next()
		switch {
		case errors.Is(err, io.EOF):
			return fail(ErrEntryNotFound)
		case err != nil:
			return fail(streamFailure(err))
		case skip, found.Name != entry, found.Type != File:
			continue
		case encrypted:
			return fail(ErrEncrypted)
		}
		if err := checkDeclared(found.Size, found.Packed); err != nil {
			return fail(err)
		}
		limit := found.Size
		if limit < 0 {
			limit = maxEntryBytes
		}
		return &capped{r: data, left: limit, close: stream.close}, found.Size, nil
	}
}
