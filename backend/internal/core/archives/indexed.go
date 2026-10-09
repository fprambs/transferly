package archives

import (
	"archive/zip"
	"errors"
	"io"
	"io/fs"

	"github.com/bodgit/sevenzip"
)

// typeOf tells the type of an entry from its mode.
func typeOf(mode fs.FileMode) EntryType {
	switch {
	case mode.IsDir():
		return Directory
	case mode&fs.ModeSymlink != 0:
		return Symlink
	}
	return File
}

// listZip lists a ZIP archive from its central directory, read by offset at the end of the file.
func listZip(src Source) ([]Entry, bool, error) {
	if err := checkZipIndex(src); err != nil {
		return nil, false, err
	}
	reader, err := zip.NewReader(src, src.Size())
	if err != nil {
		return nil, false, ErrNotArchive
	}
	var entries []Entry
	for _, f := range reader.File {
		if add(&entries, Entry{Name: f.Name, Type: typeOf(f.Mode()), Size: int64(f.UncompressedSize64), Packed: int64(f.CompressedSize64), Modified: f.Modified}) {
			return entries, true, nil
		}
	}
	return entries, false, nil
}

// openZip reads one file entry of a ZIP archive, refusing encrypted entries and entries over the caps before decompressing them.
func openZip(src Source, name string) (io.ReadCloser, int64, error) {
	if err := checkZipIndex(src); err != nil {
		return nil, 0, err
	}
	reader, err := zip.NewReader(src, src.Size())
	if err != nil {
		return nil, 0, ErrNotArchive
	}
	for _, f := range reader.File {
		if f.Name != name || typeOf(f.Mode()) != File {
			continue
		}
		size := int64(f.UncompressedSize64)
		if f.Flags&0x1 != 0 {
			return nil, 0, ErrEncrypted
		}
		if err := checkDeclared(size, int64(f.CompressedSize64)); err != nil {
			return nil, 0, err
		}
		rc, err := f.Open()
		if err != nil {
			return nil, 0, ErrNotArchive
		}
		return &capped{r: rc, left: size, close: rc.Close}, size, nil
	}
	return nil, 0, ErrEntryNotFound
}

// sevenZipFailure tells a failure of a 7z archive that involves encryption apart from one the viewer does not read.
func sevenZipFailure(err error) error {
	var failure *sevenzip.ReadError
	if errors.As(err, &failure) && failure.Encrypted {
		return ErrEncrypted
	}
	return ErrNotArchive
}

// sevenZipEntry reads a 7z entry, telling a read that fails because the entry is encrypted apart; 7-Zip only notices a missing
// password once it decrypts the data.
type sevenZipEntry struct {
	io.ReadCloser
}

// Read implements [io.Reader].
func (e sevenZipEntry) Read(p []byte) (int, error) {
	n, err := e.ReadCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, sevenZipFailure(err)
	}
	return n, err
}

// openSevenZipReader opens a 7z archive, telling one with encrypted headers apart.
func openSevenZipReader(src Source) (*sevenzip.Reader, error) {
	reader, err := sevenzip.NewReader(src, src.Size())
	if err != nil {
		return nil, sevenZipFailure(err)
	}
	return reader, nil
}

// listSevenZip lists a 7z archive from its header, read by offset at the end of the file.
func listSevenZip(src Source) ([]Entry, bool, error) {
	reader, err := openSevenZipReader(src)
	if err != nil {
		return nil, false, err
	}
	var entries []Entry
	for _, f := range reader.File {
		if add(&entries, Entry{Name: f.Name, Type: typeOf(f.Mode()), Size: int64(f.UncompressedSize), Packed: -1, Modified: f.Modified}) {
			return entries, true, nil
		}
	}
	return entries, false, nil
}

// openSevenZip reads one file entry of a 7z archive.
func openSevenZip(src Source, name string) (io.ReadCloser, int64, error) {
	reader, err := openSevenZipReader(src)
	if err != nil {
		return nil, 0, err
	}
	for _, f := range reader.File {
		if f.Name != name || typeOf(f.Mode()) != File {
			continue
		}
		size := int64(f.UncompressedSize)
		if err := checkDeclared(size, -1); err != nil {
			return nil, 0, err
		}
		rc, err := f.Open()
		if err != nil {
			return nil, 0, sevenZipFailure(err)
		}
		return &capped{r: sevenZipEntry{rc}, left: size, close: rc.Close}, size, nil
	}
	return nil, 0, ErrEntryNotFound
}
