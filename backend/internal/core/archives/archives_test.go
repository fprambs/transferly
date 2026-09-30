package archives

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/nwaples/rardecode/v2"
)

// samples is the folder of archive samples the viewers are tried with, shared with the test stack.
const samples = "../../../../testenv/preview-samples/archives"

// broken is an archive whose reads fail: every read by offset, or only the streams.
type broken struct {
	Source
	offsets bool
}

// errBroken is the failure of a [broken] archive.
var errBroken = errors.New("the storage failed")

// ReadAt implements [io.ReaderAt].
func (b broken) ReadAt(p []byte, off int64) (int, error) {
	if b.offsets {
		return 0, errBroken
	}
	return b.Source.ReadAt(p, off)
}

// Stream implements [Source].
func (b broken) Stream(int64) (io.ReadCloser, error) {
	return nil, errBroken
}

// failing is an archive whose storage fails for the bytes from start to end: reads by offset that touch them, and streams that reach
// them.
type failing struct {
	Source
	start, end int64
}

// ReadAt implements [io.ReaderAt].
func (f failing) ReadAt(p []byte, off int64) (int, error) {
	if off < f.end && off+int64(len(p)) > f.start {
		return 0, errBroken
	}
	return f.Source.ReadAt(p, off)
}

// Stream implements [Source].
func (f failing) Stream(offset int64) (io.ReadCloser, error) {
	return io.NopCloser(io.MultiReader(io.NewSectionReader(f.Source, offset, f.start-offset), iotest.ErrReader(errBroken))), nil
}

// inMemory wraps bytes as an archive.
func inMemory(data []byte) Source {
	return Bytes(data)
}

// sample reads an archive sample.
func sample(t *testing.T, name string) Source {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(samples, name))
	if err != nil {
		t.Fatal(err)
	}
	return inMemory(data)
}

// original reads the sample an archive entry was built from.
func original(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(samples, "..", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// lower sets a cap for one test.
func lower[T any](t *testing.T, target *T, value T) {
	t.Helper()
	previous := *target
	*target = value
	t.Cleanup(func() { *target = previous })
}

// readEntry opens an entry and reads it whole, telling the size the archive declared.
func readEntry(src Source, name, entry string) ([]byte, int64, error) {
	rc, size, err := Open(src, name, entry)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	return data, size, err
}

// rarVint encodes a RAR5 variable-length integer.
func rarVint(value uint64) []byte {
	return binary.AppendUvarint(nil, value)
}

// rarFile is an entry of a hand-written RAR5 archive.
type rarFile struct {
	name      string
	data      []byte
	dir       bool
	unknown   bool
	encrypted bool
}

// rarBlock encodes a RAR5 header block.
func rarBlock(kind, flags uint64, extra []byte, dataSize int, fields []byte) []byte {
	body := append(rarVint(kind), rarVint(flags)...)
	if flags&0x01 != 0 {
		body = append(body, rarVint(uint64(len(extra)))...)
	}
	if flags&0x02 != 0 {
		body = append(body, rarVint(uint64(dataSize))...)
	}
	body = append(append(body, fields...), extra...)
	sized := append(rarVint(uint64(len(body))), body...)
	return append(binary.LittleEndian.AppendUint32(nil, crc32.ChecksumIEEE(sized)), sized...)
}

// rarArchive writes a RAR5 archive with the stored method, as the sample generator does, with the flags the samples lack.
func rarArchive(files ...rarFile) []byte {
	out := []byte("Rar!\x1a\x07\x01\x00")
	out = append(out, rarBlock(1, 0, nil, 0, rarVint(0))...)
	for _, f := range files {
		flags := uint64(0x04)
		if f.dir {
			flags |= 0x01
		}
		if f.unknown {
			flags |= 0x08
		}
		fields := append(rarVint(flags), rarVint(uint64(len(f.data)))...)
		fields = append(fields, rarVint(0o644)...)
		fields = binary.LittleEndian.AppendUint32(fields, crc32.ChecksumIEEE(f.data))
		fields = append(append(append(fields, rarVint(0)...), rarVint(1)...), rarVint(uint64(len(f.name)))...)
		fields = append(fields, f.name...)
		var extra []byte
		headerFlags := uint64(0x02)
		if f.encrypted {
			record := append(append(rarVint(1), rarVint(0)...), rarVint(0)...)
			record = append(record, make([]byte, 33)...)
			record[3] = 15
			extra = append(rarVint(uint64(len(record))), record...)
			headerFlags |= 0x01
		}
		out = append(append(out, rarBlock(2, headerFlags, extra, len(f.data), fields)...), f.data...)
	}
	return append(out, rarBlock(5, 0, nil, 0, rarVint(0))...)
}

// tarArchive writes a TAR with the given headers, each file holding its name as contents.
func tarArchive(t *testing.T, headers ...*tar.Header) []byte {
	t.Helper()
	var buffer bytes.Buffer
	w := tar.NewWriter(&buffer)
	for _, h := range headers {
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(h.Name))
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			_, _ = w.Write([]byte(h.Name))
		}
	}
	_ = w.Close()
	return buffer.Bytes()
}

// zipArchive writes a ZIP with the given entries, each built by its function.
func zipArchive(t *testing.T, entries ...func(*zip.Writer) error) []byte {
	t.Helper()
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	for _, entry := range entries {
		if err := entry(w); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	return buffer.Bytes()
}

// TestDetect verifies that formats are told by their first bytes, and compressed TARs by their names too.
//
// Use cases:
//   - ZIP (and an empty ZIP), 7z, RAR and TAR are told by their signatures, whatever their names
//   - gzip, bzip2, xz and zstd are single compressed files unless the name ends as a compressed TAR of that same compression
//   - a name ending as a TAR of another compression does not change what the bytes say
//   - bytes of no archive are refused
func TestDetect(t *testing.T) {
	ustar := make([]byte, 262)
	copy(ustar[257:], "ustar")
	cases := []struct {
		name, head string
		want       Format
	}{
		{"a.bin", "PK\x03\x04", Zip}, {"a.zip", "PK\x05\x06", Zip}, {"a", "7z\xbc\xaf\x27\x1c", SevenZip}, {"a", "Rar!\x1a\x07\x01", Rar},
		{"a", string(ustar), Tar}, {"a.gz", "\x1f\x8b", Gzip}, {"A.TGZ", "\x1f\x8b", TarGzip}, {"a.tar.gz", "\x1f\x8b", TarGzip},
		{"a.bz2", "BZh9", Bzip2}, {"a.tar.bz2", "BZh9", TarBzip2}, {"a.tbz", "BZh9", TarBzip2}, {"a.txz", "\xfd7zXZ\x00", TarXz},
		{"a.xz", "\xfd7zXZ\x00", Xz}, {"a.zst", "\x28\xb5\x2f\xfd", Zstd}, {"a.tzst", "\x28\xb5\x2f\xfd", TarZstd},
		{"a.tar.gz", "BZh9", Bzip2},
	}
	for _, c := range cases {
		got, err := Detect(c.name, []byte(c.head))
		if err != nil || got != c.want {
			t.Errorf("Detect(%q) = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
	if _, err := Detect("a.zip", []byte("hello")); !errors.Is(err, ErrNotArchive) {
		t.Errorf("text: %v", err)
	}
}

// TestSamples verifies that every project sample lists the same tree and reads each file as the sample it was built from.
//
// Use cases:
//   - ZIP, 7z, RAR, TAR and TARs compressed with gzip, bzip2, xz and zstd list their folders, files and links
//   - each file reads byte for byte as the original, and tells the size the archive declares
//   - a compressed single file lists one entry named after it, of unknown size, and reads as the original
func TestSamples(t *testing.T) {
	files := map[string]string{
		"project/README.md": "markdown/README.md", "project/src/code.go": "text/code.go", "project/data/people.csv": "data/people.csv",
		"project/images/sample.png": "images/sample.png",
	}
	for _, name := range []string{"project.zip", "project.7z", "project.rar", "project.tar", "project.tar.gz", "project.tar.bz2", "project.tar.xz", "project.tar.zst"} {
		src := sample(t, name)
		listing, err := List(src, name)
		if err != nil || listing.Truncated {
			t.Fatalf("%s: %v %v", name, err, listing.Truncated)
		}
		kinds := map[string]EntryType{}
		for _, e := range listing.Entries {
			kinds[strings.TrimSuffix(e.Name, "/")] = e.Type
		}
		if kinds["project/src"] != Directory && name != "project.rar" {
			t.Errorf("%s: folders %v", name, kinds)
		}
		for entry, from := range files {
			if kinds[entry] != File {
				t.Errorf("%s: %s is %q", name, entry, kinds[entry])
			}
			data, size, err := readEntry(src, name, entry)
			want := original(t, from)
			if err != nil || !bytes.Equal(data, want) || size != int64(len(want)) {
				t.Errorf("%s: %s read %d bytes of %d (declared %d): %v", name, entry, len(data), len(want), size, err)
			}
		}
	}
	if kinds := mustList(t, "project.tar"); kinds[len(kinds)-1].Type != Symlink {
		t.Errorf("link: %+v", kinds[len(kinds)-1])
	}
	for _, name := range []string{"server.log.gz", "server.log.bz2", "server.log.xz", "server.log.zst"} {
		entries := mustList(t, name)
		if len(entries) != 1 || entries[0].Name != "server.log" || entries[0].Size != -1 {
			t.Errorf("%s: %+v", name, entries)
		}
		data, size, err := readEntry(sample(t, name), name, "server.log")
		if err != nil || size != -1 || !bytes.Equal(data, original(t, "text/server.log")) {
			t.Errorf("%s: %d bytes, %d, %v", name, len(data), size, err)
		}
	}
}

// mustList lists a sample.
func mustList(t *testing.T, name string) []Entry {
	t.Helper()
	listing, err := List(sample(t, name), name)
	if err != nil {
		t.Fatal(err)
	}
	return listing.Entries
}

// TestHostile verifies that hostile samples are listed as they are or refused, never read past the caps.
//
// Use cases:
//   - a ZIP bomb lists its entry but refuses to read it, from the ratio it declares
//   - hostile names are listed as the archive stores them, for the viewer to show as text
//   - a listing stops at the cap of entries and says so
//   - a ZIP cut in half and a 7z with encrypted headers are refused, and so are the entries of an encrypted ZIP and 7z
func TestHostile(t *testing.T) {
	if _, _, err := readEntry(sample(t, "bomb.zip"), "bomb.zip", "zeros.bin"); !errors.Is(err, ErrBomb) {
		t.Errorf("bomb: %v", err)
	}
	if entries := mustList(t, "hostile-paths.zip"); entries[0].Name != "../../../etc/passwd" || len(entries) != 5 {
		t.Errorf("paths: %+v", entries)
	}
	listing, err := List(sample(t, "many-entries.zip"), "many-entries.zip")
	if err != nil || !listing.Truncated || len(listing.Entries) != maxEntries {
		t.Errorf("many: %d %v %v", len(listing.Entries), listing.Truncated, err)
	}
	for name, want := range map[string]error{"truncated.zip": ErrNotArchive, "secret.7z": ErrEncrypted} {
		if _, err := List(sample(t, name), name); !errors.Is(err, want) {
			t.Errorf("%s: %v", name, err)
		}
		if _, _, err := Open(sample(t, name), name, "x"); !errors.Is(err, want) {
			t.Errorf("%s: open %v", name, err)
		}
	}
	for _, entry := range []string{"project/README.md", "project/src/code.go"} {
		if _, _, err := readEntry(sample(t, "secret-entries.7z"), "secret-entries.7z", entry); !errors.Is(err, ErrEncrypted) {
			t.Errorf("7z %s: %v", entry, err)
		}
	}
	if _, _, err := readEntry(sample(t, "secret.zip"), "secret.zip", "project/README.md"); !errors.Is(err, ErrEncrypted) {
		t.Errorf("zip: %v", err)
	}
}

// TestIndexed verifies the edges of the ZIP and 7z readers.
//
// Use cases:
//   - ZIP links are listed as links, and an entry of an unknown compression method is refused
//   - folders and missing names are not found, and an entry over the size cap is refused
//   - a damaged 7z is refused
//   - listings of 7z stop at the cap too
func TestIndexed(t *testing.T) {
	seven, _ := os.ReadFile(filepath.Join(samples, "project.7z"))
	if _, err := List(inMemory(seven[:len(seven)/2]), "a.7z"); !errors.Is(err, ErrNotArchive) {
		t.Errorf("damaged 7z: %v", err)
	}
	archive := zipArchive(t,
		func(w *zip.Writer) error {
			h := &zip.FileHeader{Name: "link"}
			h.SetMode(fs.ModeSymlink | 0o777)
			_, err := w.CreateHeader(h)
			return err
		},
		func(w *zip.Writer) error {
			_, err := w.CreateRaw(&zip.FileHeader{Name: "odd", Method: 99, CompressedSize64: 1, UncompressedSize64: 1})
			return err
		},
		func(w *zip.Writer) error {
			f, err := w.Create("big")
			_, _ = f.Write(make([]byte, 64))
			return err
		},
	)
	if entries, _, _ := listZip(inMemory(archive)); entries[0].Type != Symlink {
		t.Errorf("link: %+v", entries[0])
	}
	if _, _, err := Open(inMemory(archive), "a.zip", "odd"); !errors.Is(err, ErrNotArchive) {
		t.Errorf("method: %v", err)
	}
	for _, missing := range []string{"link", "none"} {
		if _, _, err := Open(inMemory(archive), "a.zip", missing); !errors.Is(err, ErrEntryNotFound) {
			t.Errorf("zip %s: %v", missing, err)
		}
		if _, _, err := Open(sample(t, "project.7z"), "a.7z", missing); !errors.Is(err, ErrEntryNotFound) {
			t.Errorf("7z %s: %v", missing, err)
		}
	}
	if _, _, err := Open(sample(t, "project.7z"), "a.7z", "project/src/"); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("7z folder: %v", err)
	}
	lower(t, &maxEntryBytes, 32)
	if _, _, err := Open(inMemory(archive), "a.zip", "big"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("zip cap: %v", err)
	}
	if _, _, err := Open(sample(t, "project.7z"), "a.7z", "project/src/code.go"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("7z cap: %v", err)
	}
	lower(t, &maxEntries, 2)
	for _, name := range []string{"project.zip", "project.7z", "project.rar", "project.tar"} {
		if listing, err := List(sample(t, name), name); err != nil || !listing.Truncated || len(listing.Entries) != 2 {
			t.Errorf("%s: %d %v %v", name, len(listing.Entries), listing.Truncated, err)
		}
	}
}

// TestStreams verifies the edges of the TAR, RAR and compressed single file readers.
//
// Use cases:
//   - TAR entries that are not files, folders or links are left out, and hard links are listed as links
//   - RAR folders, entries of unknown size and encrypted entries are listed, and encrypted ones refuse to open
//   - an entry of unknown size reads up to the cap of an entry
//   - a TAR entry over the size cap is refused, and decompressing past the scan cap fails
//   - a single compressed file has only its own entry, and broken headers of every compression are not archives
//   - damaged TAR and RAR archives are refused while listing and opening
func TestStreams(t *testing.T) {
	archive := tarArchive(t,
		&tar.Header{Name: "fifo", Typeflag: tar.TypeFifo},
		&tar.Header{Name: "hard", Typeflag: tar.TypeLink, Linkname: "file"},
		&tar.Header{Name: "file", Typeflag: tar.TypeReg},
	)
	listing, err := List(inMemory(archive), "a.tar")
	if err != nil || len(listing.Entries) != 2 || listing.Entries[0].Type != Symlink {
		t.Errorf("tar: %+v %v", listing.Entries, err)
	}
	for _, entry := range []string{"fifo", "hard", "none"} {
		if _, _, err := Open(inMemory(archive), "a.tar", entry); !errors.Is(err, ErrEntryNotFound) {
			t.Errorf("tar %s: %v", entry, err)
		}
	}
	rar := rarArchive(
		rarFile{name: "folder", dir: true},
		rarFile{name: "stream.txt", data: []byte("unknown size"), unknown: true},
		rarFile{name: "secret.txt", data: []byte("0123456789abcdef"), encrypted: true},
	)
	listing, err = List(inMemory(rar), "a.rar")
	if err != nil || len(listing.Entries) != 3 || listing.Entries[0].Type != Directory || listing.Entries[1].Size != -1 {
		t.Errorf("rar: %+v %v", listing.Entries, err)
	}
	if data, size, err := readEntry(inMemory(rar), "a.rar", "stream.txt"); err != nil || size != -1 || string(data) != "unknown size" {
		t.Errorf("rar unknown: %q %d %v", data, size, err)
	}
	if _, _, err := Open(inMemory(rar), "a.rar", "secret.txt"); !errors.Is(err, ErrEncrypted) {
		t.Errorf("rar secret: %v", err)
	}
	if _, _, err := Open(inMemory(rar), "a.rar", "folder"); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("rar folder: %v", err)
	}
	if _, _, err := Open(sample(t, "server.log.gz"), "server.log.gz", "other"); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("single: %v", err)
	}
	damaged := map[string][]byte{
		"a.gz": []byte("\x1f\x8b\x00"), "a.xz": []byte("\xfd7zXZ\x00\x00\x00\x00\x00"), "a.rar": []byte("Rar!\x1a\x07\x09"),
		"b.rar": rar[:40], "a.tar": append(archive[:600:600], bytes.Repeat([]byte{1}, 900)...),
	}
	for name, data := range damaged {
		if _, err := List(inMemory(data), name); !errors.Is(err, ErrNotArchive) {
			t.Errorf("list %s: %v", name, err)
		}
		if _, _, err := Open(inMemory(data), name, "file"); !errors.Is(err, ErrNotArchive) {
			t.Errorf("open %s: %v", name, err)
		}
	}
	lower(t, &maxEntryBytes, 3)
	if _, _, err := Open(inMemory(archive), "a.tar", "file"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("tar cap: %v", err)
	}
	if _, _, err := readEntry(sample(t, "server.log.zst"), "server.log.zst", "server.log"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("single cap: %v", err)
	}
	lower(t, &maxScanBytes, 2048)
	if _, err := List(sample(t, "project.tar.gz"), "project.tar.gz"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("scan list: %v", err)
	}
	if _, _, err := Open(sample(t, "project.tar.gz"), "project.tar.gz", "project/vendor/inner.zip"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("scan open: %v", err)
	}
}

// TestFailures verifies that failures of the storage and of the decoders are reported, not hidden.
//
// Use cases:
//   - a failed read of the first bytes, of the stream, of a ZIP index or of the data of an entry is returned as it is, not taken for a
//     damaged archive
//   - the failures a RAR decoder reports for encrypted archives mean encrypted; the caps keep their errors
//   - a capped reader stops at its limit, fails when more data follows, and ends where the data ends
func TestFailures(t *testing.T) {
	data, _ := os.ReadFile(filepath.Join(samples, "project.tar"))
	for _, src := range []broken{{Source: inMemory(data), offsets: true}, {Source: inMemory(data)}} {
		if _, err := List(src, "a.tar"); !errors.Is(err, errBroken) {
			t.Errorf("list: %v", err)
		}
		if _, _, err := Open(src, "a.tar", "x"); !errors.Is(err, errBroken) {
			t.Errorf("open: %v", err)
		}
	}
	for err, want := range map[error]error{
		rardecode.ErrArchiveEncrypted: ErrEncrypted, rardecode.ErrArchivedFileEncrypted: ErrEncrypted, ErrTooLarge: ErrTooLarge, io.ErrUnexpectedEOF: ErrNotArchive,
	} {
		if got := streamFailure(err); !errors.Is(got, want) {
			t.Errorf("streamFailure(%v) = %v", err, got)
		}
	}
	project, _ := os.ReadFile(filepath.Join(samples, "project.zip"))
	index, _ := zip.NewReader(bytes.NewReader(project), int64(len(project)))
	var people int64
	for _, f := range index.File {
		if f.Name == "project/data/people.csv" {
			people, _ = f.DataOffset()
		}
	}
	size := int64(len(project))
	if _, err := List(failing{Source: inMemory(project), start: size - 100, end: size}, "a.zip"); !errors.Is(err, errBroken) {
		t.Errorf("zip index: %v", err)
	}
	if _, _, err := readEntry(failing{Source: inMemory(project), start: people + 4096, end: people + 4097}, "a.zip", "project/data/people.csv"); !errors.Is(err, errBroken) {
		t.Errorf("zip entry: %v", err)
	}
	if _, _, err := readEntry(failing{Source: inMemory(data), start: 4096}, "a.tar", "project/vendor/inner.zip"); !errors.Is(err, errBroken) {
		t.Errorf("tar: %v", err)
	}
	over := &capped{r: strings.NewReader("abcdef"), left: 4, close: func() error { return nil }}
	if got, err := io.ReadAll(over); string(got) != "abcd" || !errors.Is(err, ErrTooLarge) {
		t.Errorf("over: %q %v", got, err)
	}
	exact := &capped{r: strings.NewReader("abcd"), left: 4, close: func() error { return nil }}
	if got, err := io.ReadAll(exact); string(got) != "abcd" || err != nil || exact.Close() != nil {
		t.Errorf("exact: %q %v", got, err)
	}
	if singleName("dir/.hidden") != ".hidden.out" || singleName("a.b.gz") != "a.b" {
		t.Error("single names")
	}
}
