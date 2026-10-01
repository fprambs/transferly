package ftpfs

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/core/prompts"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// otpChallenge is what the one-time password server replies to USER.
const otpChallenge = "otp-md5 99 seed1 ext, password or response required"

// otpServer starts a minimal FTP server that challenges USER as one-time password servers do and takes "one-time" as
// the answer. It returns the address to connect to.
func otpServer(t *testing.T) string {
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
			go serveOTP(conn)
		}
	}()
	return listener.Addr().String()
}

// serveOTP talks to one client until it leaves.
func serveOTP(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reply := func(format string, args ...any) { _, _ = fmt.Fprintf(conn, format+"\r\n", args...) }
	reply("220 one-time passwords")
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		command, argument, _ := strings.Cut(scanner.Text(), " ")
		switch strings.ToUpper(command) {
		case "USER":
			reply("331 %s", otpChallenge)
		case "PASS":
			if argument == "one-time" {
				reply("230 welcome")
			} else {
				reply("530 wrong response")
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

// TestDial_Prompt verifies asking the person connecting for the password (ADR-0016).
//
// Use cases:
//   - the password is asked with one hidden question whose instruction is the server's reply to USER, and the answer
//     signs in, even when answering takes longer than the connect timeout, which the answer window extends
//   - the stored password is not used when the site asks
//   - a prompt that fails, declined or unanswered, and a wrong answer are authentication failures
//   - an answer, user, password or account with a line break or NUL is refused, so it cannot smuggle a command into the login
func TestDial_Prompt(t *testing.T) {
	address := otpServer(t)
	var asked prompts.Challenge
	answer := func(c prompts.Challenge) ([]string, error) {
		asked = c
		time.Sleep(300 * time.Millisecond)
		return []string{"one-time"}, nil
	}
	fsys, err := Dial(t.Context(), Config{Address: address, Username: "u", Password: "stored", Prompt: answer, PromptWait: 5 * time.Second, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	_ = fsys.Close()
	if asked.Instruction != otpChallenge || len(asked.Questions) != 1 || asked.Questions[0].Echo || asked.Questions[0].Text != "Password:" {
		t.Errorf("asked = %+v", asked)
	}

	for name, prompt := range map[string]func(prompts.Challenge) ([]string, error){
		"declined": func(prompts.Challenge) ([]string, error) { return nil, prompts.ErrCanceled },
		"wrong":    func(prompts.Challenge) ([]string, error) { return []string{"stored"}, nil },
	} {
		if _, err := Dial(t.Context(), Config{Address: address, Username: "u", Prompt: prompt, Timeout: 5 * time.Second}); !isErr(err, remotefs.ErrAuth) {
			t.Errorf("%s: Dial() = %v; want ErrAuth", name, err)
		}
	}
	smuggling := func(prompts.Challenge) ([]string, error) { return []string{"one-time\r\nDELE /x"}, nil }
	if _, err := Dial(t.Context(), Config{Address: address, Username: "u", Prompt: smuggling, Timeout: 5 * time.Second}); !isErr(err, remotefs.ErrAuth) {
		t.Errorf("an answer with a line break = %v; want ErrAuth", err)
	}
	for _, cfg := range []Config{{Username: "u\r\nDELE /x"}, {Username: "u", Password: "p\nQUIT"}, {Username: "u", Account: "a\x00"}} {
		cfg.Address, cfg.Timeout = address, 5*time.Second
		if _, err := Dial(t.Context(), cfg); !isErr(err, remotefs.ErrAuth) {
			t.Errorf("Dial(%q, %q, %q) = %v; want ErrAuth before connecting", cfg.Username, cfg.Password, cfg.Account, err)
		}
	}
	if got := classify(fmt.Errorf("%w: %w", remotefs.ErrAuth, prompts.ErrExpired)); !errors.Is(got, prompts.ErrExpired) || errors.Is(got, remotefs.ErrConnectionLost) {
		t.Errorf("classify(refused prompt) = %v; want it unchanged", got)
	}
}
