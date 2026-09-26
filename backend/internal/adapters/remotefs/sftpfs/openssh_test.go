package sftpfs_test

import (
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/sftpfs"
	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// TestConformance_OpenSSH runs the shared suite against a real OpenSSH server, to catch status codes that the
// in-process server does not imitate. It is skipped unless SENTINEL_TEST_SFTP_ADDR names a server that has a
// writable directory called upload for the user sentinel, as the sftp service of the testenv profile does.
func TestConformance_OpenSSH(t *testing.T) {
	address := os.Getenv("SENTINEL_TEST_SFTP_ADDR")
	if address == "" {
		t.Skip("SENTINEL_TEST_SFTP_ADDR is not set; start the testenv profile to run this test")
	}
	remotefstest.Run(t, func(t *testing.T) remotefs.RemoteFS {
		fsys, err := sftpfs.Dial(t.Context(), sftpfs.Config{Address: address, Username: "sentinel", Password: "sentinel", VerifyHostKey: trustAll})
		if err != nil {
			t.Fatalf("Dial() = %v", err)
		}
		root := "/upload/" + uuid.NewString()
		if err := fsys.Mkdir(t.Context(), root); err != nil {
			t.Fatalf("Mkdir(%s) = %v", root, err)
		}
		return remotefstest.Prefixed{Inner: fsys, Prefix: root}
	})
}
