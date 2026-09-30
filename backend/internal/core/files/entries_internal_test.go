package files

import (
	"context"
	"errors"
	"io"
	"testing"
)

// TestRemoteReader_PastTheEnd verifies that a read reaching past the end of the file returns what the file holds and says it ended.
//
// Use cases:
//   - a read that starts inside the file and runs past its end returns the bytes up to the end with io.EOF
//   - a read that starts at the end returns nothing with io.EOF, without opening the file
func TestRemoteReader_PastTheEnd(t *testing.T) {
	r := &remoteReader{ctx: context.Background(), size: 3, blocks: map[int64][]byte{0: []byte("abc")}}
	buffer := make([]byte, 8)
	if n, err := r.ReadAt(buffer, 1); n != 2 || !errors.Is(err, io.EOF) || string(buffer[:n]) != "bc" {
		t.Errorf("ReadAt() = %d, %v", n, err)
	}
	if n, err := r.ReadAt(buffer, 3); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("ReadAt() at the end = %d, %v", n, err)
	}
}
