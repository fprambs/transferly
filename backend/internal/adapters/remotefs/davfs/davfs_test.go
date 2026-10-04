package davfs_test

import (
	"net/http"
	"testing"

	"github.com/fprambs/transferly/internal/adapters/remotefs/davfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/davfs/davtest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// dial opens the root of a test server with its credentials, and closes it when the test ends.
func dial(t *testing.T, s *davtest.Server) *davfs.FS {
	t.Helper()
	fsys, err := davfs.Dial(t.Context(), davfs.Config{Endpoint: s.Endpoint(), Username: davtest.Username, Password: davtest.Password, Client: &http.Client{}})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	t.Cleanup(func() { _ = fsys.Close() })
	return fsys
}

// TestConformance runs the shared RemoteFS suite against the WebDAV adapter and golang.org/x/net/webdav.
func TestConformance(t *testing.T) {
	remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS { return dial(t, davtest.Start(t, davtest.Options{})) })
}
