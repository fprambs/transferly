package archives

import (
	"encoding/binary"
	"errors"
	"testing"
)

// zipFooter builds only the ZIP metadata needed to test allocation admission before parsing entries.
func zipFooter(size int) []byte {
	b := make([]byte, size)
	copy(b[size-22:], []byte{'P', 'K', 5, 6})
	return b
}

// TestZipIndexBudget exercises malformed offsets and ZIP64 metadata without allocating hostile entry counts.
func TestZipIndexBudget(t *testing.T) {
	for _, b := range [][]byte{nil, make([]byte, 23)} {
		if err := checkZipIndex(Bytes(b)); !errors.Is(err, ErrNotArchive) {
			t.Fatal(err)
		}
	}
	large := zipFooter(22)
	binary.LittleEndian.PutUint32(large[12:], zipIndexBudget+1)
	if _, _, err := listZip(Bytes(large)); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, _, err := openZip(Bytes(large), "entry"); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	outside := zipFooter(22)
	binary.LittleEndian.PutUint32(outside[16:], 23)
	if err := checkZipIndex(Bytes(outside)); !errors.Is(err, ErrNotArchive) {
		t.Fatal(err)
	}
	missing := zipFooter(22)
	binary.LittleEndian.PutUint16(missing[10:], 65535)
	if err := checkZipIndex(Bytes(missing)); !errors.Is(err, ErrNotArchive) {
		t.Fatal(err)
	}
	b := zipFooter(98)
	copy(b[:4], []byte{'P', 'K', 6, 6})
	copy(b[56:60], []byte{'P', 'K', 6, 7})
	binary.LittleEndian.PutUint16(b[86:], 65535)
	if err := checkZipIndex(Bytes(b)); err != nil {
		t.Fatal(err)
	}
	bad := append([]byte(nil), b...)
	binary.LittleEndian.PutUint64(bad[64:], 100)
	if err := checkZipIndex(Bytes(bad)); !errors.Is(err, ErrNotArchive) {
		t.Fatal(err)
	}
	bad = append([]byte(nil), b...)
	bad[0] = 0
	if err := checkZipIndex(Bytes(bad)); !errors.Is(err, ErrNotArchive) {
		t.Fatal(err)
	}
	bad = append([]byte(nil), b...)
	binary.LittleEndian.PutUint64(bad[32:], 100001)
	if err := checkZipIndex(Bytes(bad)); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
}

// TestZipInvalidDirectory checks a directory with admitted bounds but invalid entry records.
func TestZipInvalidDirectory(t *testing.T) {
	b := zipFooter(68)
	binary.LittleEndian.PutUint16(b[56:], 1)
	binary.LittleEndian.PutUint32(b[58:], 46)
	if _, _, err := listZip(Bytes(b)); !errors.Is(err, ErrNotArchive) {
		t.Fatal(err)
	}
	if _, _, err := openZip(Bytes(b), "entry"); !errors.Is(err, ErrNotArchive) {
		t.Fatal(err)
	}
}
