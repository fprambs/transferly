package sevenzip

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/bodgit/sevenzip/internal/lzma2"
	"github.com/bodgit/sevenzip/internal/ppmd"
	"github.com/bodgit/sevenzip/internal/util"
)

// TestAllocationBudgets verifies hostile metadata is rejected before allocating large slices or codec dictionaries.
func TestAllocationBudgets(t *testing.T) {
	if _, err := readUint64Bounded(bytes.NewReader([]byte{0xc1, 0xa1, 0x86}), true); err == nil {
		t.Fatal("accepted 100001 metadata elements")
	}
	if _, err := readFolder(util.ByteReadCloser(io.NopCloser(bytes.NewReader([]byte{5})))); err == nil {
		t.Fatal("accepted excessive codec layers")
	}
	if _, err := lzma2.NewReader([]byte{40}, 0, []io.ReadCloser{io.NopCloser(bytes.NewReader(nil))}); err == nil {
		t.Fatal("accepted multi-gigabyte dictionary")
	}
	properties := make([]byte, 5)
	properties[0] = 6
	binary.LittleEndian.PutUint32(properties[1:], 128<<20)
	if _, err := ppmd.NewReader(properties, 1, []io.ReadCloser{io.NopCloser(bytes.NewReader(nil))}); err == nil {
		t.Fatal("accepted oversized PPMd dictionary")
	}
}
