package ftpfs

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// answer is what the scripted server sends for a LIST of one path: a refusal code, or a listing.
type answer struct {
	code    int
	message string
	listing string
}

// scripted starts a minimal FTP server that has only LIST, like the oldest servers, and answers each LIST from a table
// keyed by the path. It exists for behaviors the full test server cannot imitate, such as listing a missing path with an
// empty answer. It returns the address to connect to.
func scripted(t *testing.T, lists map[string]answer) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serve(t, conn, lists)
		}
	}()
	return listener.Addr().String()
}

// serve talks to one client until it leaves.
func serve(t *testing.T, conn net.Conn, lists map[string]answer) {
	t.Helper()
	defer func() { _ = conn.Close() }()
	reply := func(format string, args ...any) { _, _ = fmt.Fprintf(conn, format+"\r\n", args...) }
	reply("220 scripted")
	var data net.Listener
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		command, argument, _ := strings.Cut(scanner.Text(), " ")
		switch strings.ToUpper(command) {
		case "USER":
			reply("331 password please")
		case "PASS":
			reply("230 welcome")
		case "FEAT":
			reply("211-Features:\r\n UTF8\r\n211 End")
		case "OPTS", "TYPE", "NOOP":
			reply("200 ok")
		case "EPSV":
			listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
			if err != nil {
				reply("500 no data port")
				continue
			}
			data = listener
			reply("229 Entering Extended Passive Mode (|||%d|)", listener.Addr().(*net.TCPAddr).Port)
		case "LIST":
			result, known := lists[argument]
			if !known || data == nil {
				reply("550 not scripted")
				continue
			}
			if result.code != 0 {
				reply("%d %s", result.code, result.message)
				continue
			}
			reply("150 here it comes")
			if peer, err := data.Accept(); err == nil {
				_, _ = peer.Write([]byte(result.listing))
				_ = peer.Close()
			}
			_ = data.Close()
			data = nil
			reply("226 done")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 not implemented")
		}
	}
}

// TestList_ServersWithOnlyList verifies the checks that make LIST answers trustworthy.
//
// Scenarios:
//   - a missing directory that such a server lists as empty is ErrNotFound, because its parent does not show it
//   - an existing empty directory is listed as empty
//   - a failure of the check itself is reported, not turned into an empty listing
func TestList_ServersWithOnlyList(t *testing.T) {
	address := scripted(t, map[string]answer{
		"/":        {listing: "drwxr-xr-x 2 u g 4096 Jan  1 12:00 kept\r\n"},
		"/ghost":   {},
		"/kept":    {},
		"/blocked": {},
	})
	fsys, err := Dial(t.Context(), Config{Address: address, Username: "u", Password: "p", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	t.Cleanup(func() { _ = fsys.Close() })

	if _, err := fsys.List(t.Context(), "/ghost"); !isErr(err, remotefs.ErrNotFound) {
		t.Errorf("List of a missing directory listed as empty = %v; want ErrNotFound", err)
	}
	if entries, err := fsys.List(t.Context(), "/kept"); err != nil || len(entries) != 0 {
		t.Errorf("List of an empty directory = %v, %v; want no entries", entries, err)
	}
}

// TestList_CheckFailure verifies that a failing directory check is reported.
func TestList_CheckFailure(t *testing.T) {
	address := scripted(t, map[string]answer{
		"/":     {code: 550, message: "denied"},
		"/dir":  {},
		"/dir2": {},
	})
	fsys, err := Dial(t.Context(), Config{Address: address, Username: "u", Password: "p", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	t.Cleanup(func() { _ = fsys.Close() })

	if _, err := fsys.List(t.Context(), "/dir"); !isErr(err, remotefs.ErrPermission) {
		t.Errorf("List with a failing check = %v; want ErrPermission", err)
	}
}
