package archives

import (
	"bytes"
	"encoding/binary"
)

// zipIndexBudget bounds index materialization before archive/zip allocates its entry objects.
const zipIndexBudget = 16 << 20

// checkZipIndex validates ZIP and ZIP64 directory bounds before a parser reads the directory.
func checkZipIndex(src Source) error {
	size := src.Size()
	if size < 22 {
		return ErrNotArchive
	}
	n := min(size, 65557)
	tail := make([]byte, n)
	if _, err := src.ReadAt(tail, size-n); err != nil {
		return ErrNotArchive
	}
	at := bytes.LastIndex(tail, []byte{'P', 'K', 5, 6})
	if at < 0 || at+22 > len(tail) {
		return ErrNotArchive
	}
	count := uint64(binary.LittleEndian.Uint16(tail[at+10:]))
	length := uint64(binary.LittleEndian.Uint32(tail[at+12:]))
	offset := uint64(binary.LittleEndian.Uint32(tail[at+16:]))
	if count == 65535 || length == 0xffffffff || offset == 0xffffffff {
		if at < 20 || binary.LittleEndian.Uint32(tail[at-20:]) != 0x07064b50 {
			return ErrNotArchive
		}
		location := binary.LittleEndian.Uint64(tail[at-12:])
		if size < 56 || location > uint64(size)-56 {
			return ErrNotArchive
		}
		header := make([]byte, 56)
		if _, err := src.ReadAt(header, int64(location)); err != nil || binary.LittleEndian.Uint32(header) != 0x06064b50 {
			return ErrNotArchive
		}
		count = binary.LittleEndian.Uint64(header[32:])
		length = binary.LittleEndian.Uint64(header[40:])
		offset = binary.LittleEndian.Uint64(header[48:])
	}
	if count > 100000 || length > zipIndexBudget {
		return ErrTooLarge
	}
	if offset > uint64(size) || length > uint64(size)-offset {
		return ErrNotArchive
	}
	return nil
}
