package ftpfs

import (
	"fmt"

	"golang.org/x/text/encoding"

	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// codec converts the file names the client works with, which are UTF-8, to and from the character set the server uses. The zero
// value, and a nil encoding, leave names as they are.
type codec struct {
	encoding encoding.Encoding
}

// server converts a UTF-8 path to the bytes the server expects, held in a string. A name with characters the character set cannot hold
// is [remotefs.ErrInvalid].
func (c codec) server(p string) (string, error) {
	if c.encoding == nil {
		return p, nil
	}
	out, err := c.encoding.NewEncoder().String(p)
	if err != nil {
		return "", fmt.Errorf("%w: the name has characters the server character set cannot hold", remotefs.ErrInvalid)
	}
	return out, nil
}

// client converts a name the server sent to UTF-8; bytes that are not valid in the character set become the replacement character.
func (c codec) client(name string) string {
	if c.encoding == nil {
		return name
	}
	out, _ := c.encoding.NewDecoder().String(name)
	return out
}
