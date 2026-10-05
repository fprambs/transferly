package remotefs

import (
	"context"
	"path"
	"strings"
)

// Homer is implemented by connections whose server opens each session in a folder of its own, as SFTP and FTP servers do with
// the home folder of the account. Without it, a connection starts at the root.
type Homer interface {
	// Home returns the absolute path of the folder the server opened the session in, as pwd tells it.
	Home(ctx context.Context) (string, error)
}

// HomeOf returns the folder a server answered as its working folder, cleaned, or the root when the answer is not an absolute
// path.
func HomeOf(answer string) string {
	if !strings.HasPrefix(answer, "/") {
		return "/"
	}
	return path.Clean(answer)
}
