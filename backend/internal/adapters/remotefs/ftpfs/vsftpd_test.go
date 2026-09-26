package ftpfs_test

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/remotefs/ftpfs"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// TestConformance_Vsftpd runs the shared suite against real vsftpd servers, to catch replies that the in-process server
// does not imitate, in particular the TLS session reuse that the FTPS servers require on data connections. Each mode is
// skipped unless its variable names a server whose user sentinel can write in /upload, as the ftp, ftps and
// ftps-implicit services of the test stack (profile ftp) do.
//
// Scenarios:
//   - plain FTP (SENTINEL_TEST_FTP_ADDR), explicit FTPS (SENTINEL_TEST_FTPS_ADDR) and implicit FTPS (SENTINEL_TEST_FTPS_IMPLICIT_ADDR)
//   - the suite runs in a directory of its own, removed afterwards, so servers can be shared
func TestConformance_Vsftpd(t *testing.T) {
	for name, server := range map[string]struct {
		variable string
		mode     ftpfs.TLSMode
	}{
		"plain":         {"SENTINEL_TEST_FTP_ADDR", ftpfs.TLSNone},
		"explicit ftps": {"SENTINEL_TEST_FTPS_ADDR", ftpfs.TLSExplicit},
		"implicit ftps": {"SENTINEL_TEST_FTPS_IMPLICIT_ADDR", ftpfs.TLSImplicit},
	} {
		t.Run(name, func(t *testing.T) {
			address := os.Getenv(server.variable)
			if address == "" {
				t.Skipf("%s is not set; start the ftp profile of the test stack to run this test", server.variable)
			}
			remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS {
				fsys, err := ftpfs.Dial(t.Context(), ftpfs.Config{
					Address:       address,
					Username:      "sentinel",
					Password:      "sentinel",
					TLS:           server.mode,
					VerifyHostKey: func(string, string) error { return nil },
					Timeout:       10 * time.Second,
				})
				if err != nil {
					t.Fatalf("Dial() = %v", err)
				}
				root := "/upload/" + uuid.NewString()
				if err := fsys.Mkdir(t.Context(), root); err != nil {
					t.Fatalf("Mkdir(%s) = %v", root, err)
				}
				return remotefstest.Prefixed{Inner: fsys, Prefix: root}
			})
		})
	}
}
