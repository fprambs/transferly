package ftpfs

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// accountServer starts a minimal FTP server that asks for the account "billing" (reply 332), after PASS or, when early is true,
// right after USER, as the servers that charge the transfers of each account do. It returns the address to connect to.
func accountServer(t *testing.T, early bool) string {
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
			go serveAccount(conn, early)
		}
	}()
	return listener.Addr().String()
}

// serveAccount talks to one client until it leaves.
func serveAccount(conn net.Conn, early bool) {
	defer func() { _ = conn.Close() }()
	reply := func(format string, args ...any) { _, _ = fmt.Fprintf(conn, format+"\r\n", args...) }
	reply("220 accounting")
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		command, argument, _ := strings.Cut(scanner.Text(), " ")
		switch strings.ToUpper(command) {
		case "USER":
			if early {
				reply("332 account please")
			} else {
				reply("331 password please")
			}
		case "PASS":
			reply("332 account please")
		case "ACCT":
			if argument == "billing" {
				reply("230 welcome")
			} else {
				reply("530 unknown account")
			}
		case "FEAT":
			reply("211 no features")
		case "TYPE", "OPTS", "NOOP":
			reply("200 ok")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 not here")
		}
	}
}

// TestDial_Account verifies logging in to servers that ask for an account (4.8e).
//
// Scenarios:
//   - the account is sent with ACCT when the server asks for it after the password, or right after the user name
//   - without an account, or with a wrong one, the login is an authentication failure
func TestDial_Account(t *testing.T) {
	for _, early := range []bool{false, true} {
		address := accountServer(t, early)
		fsys, err := Dial(t.Context(), Config{Address: address, Username: "u", Password: "p", Account: "billing", Timeout: 5 * time.Second})
		if err != nil {
			t.Fatalf("Dial() asked early=%v = %v", early, err)
		}
		_ = fsys.Close()
		for _, account := range []string{"", "other"} {
			if _, err := Dial(t.Context(), Config{Address: address, Username: "u", Password: "p", Account: account, Timeout: 5 * time.Second}); !isErr(err, remotefs.ErrAuth) {
				t.Errorf("Dial() with the account %q, asked early=%v = %v; want ErrAuth", account, early, err)
			}
		}
	}
}
