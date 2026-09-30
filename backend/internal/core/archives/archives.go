// Package archives lists and reads the entries of archives for the file viewers (ADR-0018), without extracting anything to disk:
// ZIP and 7z by their index, read by offset, and TAR (plain or compressed with gzip, bzip2, xz or zstd), single compressed files and
// RAR as streams. Every read has caps against decompression bombs and hostile archives.
package archives

import (
	"errors"
	"io"
	"path"
	"strings"
	"time"
)

// Format is the kind of archive.
type Format string

// The formats read.
const (
	Zip      Format = "zip"
	SevenZip Format = "7z"
	Rar      Format = "rar"
	Tar      Format = "tar"
	TarGzip  Format = "tar.gz"
	TarBzip2 Format = "tar.bz2"
	TarXz    Format = "tar.xz"
	TarZstd  Format = "tar.zst"
	Gzip     Format = "gz"
	Bzip2    Format = "bz2"
	Xz       Format = "xz"
	Zstd     Format = "zst"
)

// EntryType is what an entry is.
type EntryType string

// The types of entry.
const (
	File      EntryType = "file"
	Directory EntryType = "directory"
	Symlink   EntryType = "symlink"
)

// Entry is one entry of an archive.
type Entry struct {
	// Name is its path inside the archive, with slashes, as the archive stores it.
	Name string
	// Type is what it is.
	Type EntryType
	// Size is its size once extracted, or -1 when the archive does not tell it.
	Size int64
	// Packed is its size inside the archive, or -1 when the archive does not tell it.
	Packed int64
	// Modified is when it was last changed, or the zero time.
	Modified time.Time
}

// Listing is the contents of an archive.
type Listing struct {
	// Format is the kind of archive.
	Format Format
	// Entries are its entries in the order of the archive, at most ten thousand.
	Entries []Entry
	// Truncated says the archive holds more entries than the listing.
	Truncated bool
}

// Source is an archive to read: by offset for the formats with an index, and as a stream from an offset for the others.
type Source interface {
	io.ReaderAt
	// Size is the size of the archive, in bytes.
	Size() int64
	// Stream reads the archive from offset to its end.
	Stream(offset int64) (io.ReadCloser, error)
}

// The caps of every read, variables so the tests can lower them.
var (
	// maxEntries is the most entries a listing holds.
	maxEntries = 10_000
	// maxEntryBytes is the most bytes an entry is read to, once extracted.
	maxEntryBytes int64 = 512 << 20
	// maxScanBytes is the most bytes a stream is decompressed to while looking for an entry or listing a TAR.
	maxScanBytes int64 = 1 << 30
)

const (
	// maxRatio is the largest compression ratio an entry may declare, past a megabyte, before it is taken for a bomb.
	maxRatio = 200
	// maxWindow bounds the memory the xz and zstd decoders may ask for.
	maxWindow = 64 << 20
)

var (
	// ErrNotArchive means the file is not an archive this package reads.
	ErrNotArchive = errors.New("not an archive the viewer reads")
	// ErrEntryNotFound means the archive has no file entry of that name.
	ErrEntryNotFound = errors.New("the archive has no such entry")
	// ErrTooLarge means an entry, or the work to reach it, is larger than the caps.
	ErrTooLarge = errors.New("the archive entry is too large to read")
	// ErrBomb means an entry declares a compression ratio no real file has.
	ErrBomb = errors.New("the archive entry looks like a decompression bomb")
	// ErrEncrypted means the entry, or the whole archive, is protected with a password.
	ErrEncrypted = errors.New("the archive is protected with a password")
)

// suffixes maps the endings of names to the compressed formats they tell, longest first.
var suffixes = []struct {
	suffix string
	format Format
}{
	{".tar.gz", TarGzip}, {".tgz", TarGzip}, {".tar.bz2", TarBzip2}, {".tbz2", TarBzip2}, {".tbz", TarBzip2},
	{".tar.xz", TarXz}, {".txz", TarXz}, {".tar.zst", TarZstd}, {".tzst", TarZstd},
}

// Detect tells the format of an archive from its first bytes, and its name for compressed TARs, which a compressed single file shares
// its signature with.
func Detect(name string, head []byte) (Format, error) {
	lower := strings.ToLower(name)
	starts := func(prefix string) bool { return strings.HasPrefix(string(head), prefix) }
	compressed := func(single Format) Format {
		for _, s := range suffixes {
			if strings.HasSuffix(lower, s.suffix) && strings.HasSuffix(string(s.format), string(single)) {
				return s.format
			}
		}
		return single
	}
	switch {
	case starts("PK\x03\x04"), starts("PK\x05\x06"):
		return Zip, nil
	case starts("7z\xbc\xaf\x27\x1c"):
		return SevenZip, nil
	case starts("Rar!\x1a\x07"):
		return Rar, nil
	case starts("\x1f\x8b"):
		return compressed(Gzip), nil
	case starts("BZh"):
		return compressed(Bzip2), nil
	case starts("\xfd7zXZ\x00"):
		return compressed(Xz), nil
	case starts("\x28\xb5\x2f\xfd"):
		return compressed(Zstd), nil
	case len(head) >= 262 && string(head[257:262]) == "ustar":
		return Tar, nil
	}
	return "", ErrNotArchive
}

// singleName is the name of the file inside a compressed single file: its own name without the compression suffix.
func singleName(name string) string {
	base := path.Base(name)
	if dot := strings.LastIndexByte(base, '.'); dot > 0 {
		return base[:dot]
	}
	return base + ".out"
}

// capped reads at most limit bytes and fails with [ErrTooLarge] past them, instead of pretending the file ended.
type capped struct {
	r     io.Reader
	left  int64
	close func() error
}

// Read implements [io.Reader].
func (c *capped) Read(p []byte) (int, error) {
	if c.left <= 0 {
		var probe [1]byte
		if n, _ := c.r.Read(probe[:]); n > 0 {
			return 0, ErrTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// Close implements [io.Closer].
func (c *capped) Close() error {
	return c.close()
}

// checkDeclared refuses an entry whose declared size is over the caps, or whose declared ratio is one no real file has.
func checkDeclared(size, packed int64) error {
	switch {
	case size > maxEntryBytes:
		return ErrTooLarge
	case packed > 0 && size > 1<<20 && size/packed > maxRatio:
		return ErrBomb
	}
	return nil
}

// List lists the entries of an archive.
func List(src Source, name string) (Listing, error) {
	format, err := detectSource(src, name)
	if err != nil {
		return Listing{}, err
	}
	var entries []Entry
	var truncated bool
	switch format {
	case Zip:
		entries, truncated, err = listZip(src)
	case SevenZip:
		entries, truncated, err = listSevenZip(src)
	default:
		entries, truncated, err = listStream(src, name, format)
	}
	if err != nil {
		return Listing{}, err
	}
	return Listing{Format: format, Entries: entries, Truncated: truncated}, nil
}

// Open reads one file entry of an archive, capped at 512 MiB, and tells its size, or -1 when the archive does not.
func Open(src Source, name, entry string) (io.ReadCloser, int64, error) {
	format, err := detectSource(src, name)
	if err != nil {
		return nil, 0, err
	}
	switch format {
	case Zip:
		return openZip(src, entry)
	case SevenZip:
		return openSevenZip(src, entry)
	}
	return openStream(src, name, format, entry)
}

// detectSource reads the first bytes of an archive and tells its format.
func detectSource(src Source, name string) (Format, error) {
	head := make([]byte, 512)
	n, err := src.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return Detect(name, head[:n])
}

// add appends an entry to a listing unless it is full, and tells whether it was full.
func add(entries *[]Entry, entry Entry) bool {
	if len(*entries) >= maxEntries {
		return true
	}
	*entries = append(*entries, entry)
	return false
}
