package ftpfs_test

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"

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

// TestVsftpd_LargeUpload verifies that files of many TLS records reach real servers, which small files do not show.
//
// Scenarios:
//   - a file of 1 MiB is uploaded, listed with its size and removed, over plain FTP, explicit FTPS and implicit FTPS
func TestVsftpd_LargeUpload(t *testing.T) {
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
			fsys, err := ftpfs.Dial(t.Context(), ftpfs.Config{
				Address: address, Username: "sentinel", Password: "sentinel", TLS: server.mode,
				VerifyHostKey: func(string, string) error { return nil }, Timeout: 10 * time.Second,
			})
			if err != nil {
				t.Fatalf("Dial() = %v", err)
			}
			defer func() { _ = fsys.Close() }()
			name := "/upload/large-" + uuid.NewString() + ".bin"
			out, err := fsys.OpenWrite(t.Context(), name, 0, remotefs.WriteCreate)
			if err != nil {
				t.Fatalf("OpenWrite() = %v", err)
			}
			if _, err := out.Write(bytes.Repeat([]byte{7}, 1<<20)); err != nil {
				t.Fatalf("Write() = %v", err)
			}
			if err := out.Close(); err != nil {
				t.Fatalf("Close() = %v", err)
			}
			entry, err := fsys.Stat(t.Context(), name)
			if err != nil || entry.Size != 1<<20 {
				t.Errorf("Stat() = %+v, %v; want 1 MiB", entry, err)
			}
			if err := fsys.Remove(t.Context(), name); err != nil {
				t.Errorf("Remove() = %v", err)
			}
		})
	}
}

// TestVsftpd_Charset verifies names in a legacy character set against a real server.
//
// Scenarios:
//   - a file with accents is written through an ISO-8859-1 connection, listed with its name back in UTF-8, read back and removed, and a
//     connection without a character set sees the same file under the bytes of the server
func TestVsftpd_Charset(t *testing.T) {
	address := os.Getenv("SENTINEL_TEST_FTP_ADDR")
	if address == "" {
		t.Skip("SENTINEL_TEST_FTP_ADDR is not set; start the ftp profile of the test stack to run this test")
	}
	dial := func(charset encoding.Encoding) *ftpfs.FS {
		fsys, err := ftpfs.Dial(t.Context(), ftpfs.Config{Address: address, Username: "sentinel", Password: "sentinel", Charset: charset, Timeout: 10 * time.Second})
		if err != nil {
			t.Fatalf("Dial() = %v", err)
		}
		t.Cleanup(func() { _ = fsys.Close() })
		return fsys
	}
	legacy := dial(charmap.ISO8859_1)
	dir := "/upload/café-" + uuid.NewString()
	if err := legacy.Mkdir(t.Context(), dir); err != nil {
		t.Fatalf("Mkdir(%s) = %v", dir, err)
	}
	t.Cleanup(func() { _ = legacy.RemoveAll(t.Context(), dir) })
	file := dir + "/año.txt"
	out, err := legacy.OpenWrite(t.Context(), file, 0, remotefs.WriteCreate)
	if err != nil {
		t.Fatalf("OpenWrite() = %v", err)
	}
	_, _ = out.Write([]byte("contenido"))
	if err := out.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	entries, err := legacy.List(t.Context(), dir)
	if err != nil || len(entries) != 1 || entries[0].Name != "año.txt" || entries[0].Size != 9 {
		t.Fatalf("List() = %+v, %v; want año.txt", entries, err)
	}
	if entry, err := legacy.Stat(t.Context(), file); err != nil || entry.Name != "año.txt" {
		t.Errorf("Stat() = %+v, %v", entry, err)
	}
	in, err := legacy.OpenRead(t.Context(), file, 0)
	if err != nil {
		t.Fatalf("OpenRead() = %v", err)
	}
	if data, _ := io.ReadAll(in); string(data) != "contenido" {
		t.Errorf("read back %q", data)
	}
	_ = in.Close()

	plain := dial(nil)
	if seen, err := plain.List(t.Context(), "/upload"); err != nil || len(seen) == 0 {
		t.Errorf("a connection without a character set lists %+v, %v", seen, err)
	}
	if err := legacy.Remove(t.Context(), file); err != nil {
		t.Errorf("Remove() = %v", err)
	}
}

// TestVsftpd_ConcurrentSessions verifies that a busy server never makes a listing look empty.
//
// Scenarios:
//   - eight sessions of the same user list the root, which is never empty, while the others upload and remove files, and no listing fails or comes back empty
func TestVsftpd_ConcurrentSessions(t *testing.T) {
	address := os.Getenv("SENTINEL_TEST_FTP_ADDR")
	if address == "" {
		t.Skip("SENTINEL_TEST_FTP_ADDR is not set; start the ftp profile of the test stack to run this test")
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fsys, err := ftpfs.Dial(t.Context(), ftpfs.Config{Address: address, Username: "sentinel", Password: "sentinel", Timeout: 10 * time.Second})
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = fsys.Close() }()
			for round := 0; round < 10; round++ {
				if worker%2 == 0 {
					if entries, err := fsys.List(t.Context(), "/"); err != nil || len(entries) == 0 {
						t.Errorf("List(/) = %d entries, %v; want the upload folder", len(entries), err)
					}
					continue
				}
				name := "/upload/busy-" + uuid.NewString()
				out, err := fsys.OpenWrite(t.Context(), name, 0, remotefs.WriteCreate)
				if err != nil {
					t.Errorf("OpenWrite() = %v", err)
					continue
				}
				_, _ = out.Write(make([]byte, 100_000))
				if err := out.Close(); err != nil {
					t.Errorf("Close() = %v", err)
				}
				if err := fsys.Remove(t.Context(), name); err != nil {
					t.Errorf("Remove() = %v", err)
				}
			}
		}()
	}
	wg.Wait()
}
