package sftpfs_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// shellServer starts a server that runs commands, as the probe tells, and whose shell answers each line typed with "got: " and the
// line, ending with status 7 at the line "bye"; it records the command that started the shell.
func shellServer(t *testing.T, opt sftptest.Options) (*sftptest.Server, chan string) {
	t.Helper()
	started := make(chan string, 8)
	opt.Exec = func(_ string, stdout, _ io.Writer) int {
		_, _ = io.WriteString(stdout, "transferly-probe\n")
		return 0
	}
	if opt.Shell == nil && !opt.NoShellCommand {
		opt.Shell = func(command string, term io.ReadWriter) int {
			started <- command
			lines := bufio.NewScanner(term)
			for lines.Scan() {
				switch lines.Text() {
				case "bye":
					return 7
				case "done":
					return 0
				}
				_, _ = io.WriteString(term, "got: "+lines.Text()+"\n")
			}
			return 0
		}
	}
	if opt.NoShellCommand {
		opt.Shell = func(string, io.ReadWriter) int { return 0 }
	}
	return startServer(t, opt), started
}

// TestFS_Shell verifies the terminals of a connection (ADR-0027).
//
// Use cases:
//   - a shell starts in the folder asked for, quoted, or where the server starts sessions; it runs what is typed, takes resizes and
//     ends with its status, zero included
//   - a relative folder is invalid
//   - a server that runs no commands, gives no terminal, refuses the command of the shell or opens no session is unsupported, and
//     a closed connection is lost
//   - a shell closed before it ends has no status
func TestFS_Shell(t *testing.T) {
	s, started := shellServer(t, sftptest.Options{})
	fsys := dial(t, s)
	term, err := fsys.Shell(t.Context(), "/srv/it's", 90, 20)
	if err != nil {
		t.Fatalf("Shell() = %v", err)
	}
	_, _ = io.WriteString(term, "hello\n")
	reply := bufio.NewReader(term)
	if line, _ := reply.ReadString('\n'); line != "got: hello\n" {
		t.Errorf("the shell answered %q", line)
	}
	if err := term.Resize(100, 30); err != nil {
		t.Errorf("Resize() = %v", err)
	}
	_, _ = io.WriteString(term, "bye\n")
	_, _ = io.Copy(io.Discard, reply)
	if code, err := term.Wait(); code != 7 || err != nil {
		t.Errorf("Wait() = %d, %v; want 7", code, err)
	}
	if want, got := `cd '/srv/it'\''s' 2>/dev/null; exec "${SHELL:-/bin/sh}" -l`, <-started; got != want {
		t.Errorf("the shell started with %q", got)
	}
	if got := strings.Join(s.Windows(), ","); !strings.HasPrefix(got, "90x20,100x30") {
		t.Errorf("windows = %s", got)
	}

	home, err := fsys.Shell(t.Context(), "", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if got := <-started; got != `exec "${SHELL:-/bin/sh}" -l` {
		t.Errorf("the shell without a folder started with %q", got)
	}
	_, _ = io.WriteString(home, "done\n")
	_, _ = io.Copy(io.Discard, home)
	if code, err := home.Wait(); code != 0 || err != nil {
		t.Errorf("Wait() of a shell that ended well = %d, %v", code, err)
	}
	closed, err := fsys.Shell(t.Context(), "/", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	_ = closed.Close()
	if code, err := closed.Wait(); code != -1 || err == nil {
		t.Errorf("Wait() of a closed shell = %d, %v; want -1 and an error", code, err)
	}
	if _, err := fsys.Shell(t.Context(), "relative", 80, 24); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Shell(relative) = %v, want ErrInvalid", err)
	}

	noCommands := dial(t, startServer(t, sftptest.Options{}))
	noTerminal := dial(t, startServer(t, sftptest.Options{Exec: func(_ string, stdout, _ io.Writer) int {
		_, _ = io.WriteString(stdout, "transferly-probe\n")
		return 0
	}}))
	refusing, _ := shellServer(t, sftptest.Options{NoShellCommand: true})
	for name, err := range map[string]error{
		"no commands":   second(noCommands.Shell(t.Context(), "/", 80, 24)),
		"no terminal":   second(noTerminal.Shell(t.Context(), "/", 80, 24)),
		"refused shell": second(dial(t, refusing).Shell(t.Context(), "/", 80, 24)),
	} {
		if !errors.Is(err, remotefs.ErrUnsupported) {
			t.Errorf("Shell() with %s = %v, want ErrUnsupported", name, err)
		}
	}
	dropped, _ := shellServer(t, sftptest.Options{})
	gone := dial(t, dropped)
	if _, err := gone.Tools(t.Context()); err != nil {
		t.Fatal(err)
	}
	dropped.DropConnections()
	if _, err := gone.Shell(t.Context(), "/", 80, 24); !errors.Is(err, remotefs.ErrUnsupported) {
		t.Errorf("Shell() on a dropped connection = %v, want ErrUnsupported", err)
	}
	_ = fsys.Close()
	if _, err := fsys.Shell(t.Context(), "/", 80, 24); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Shell() after Close = %v, want ErrConnectionLost", err)
	}
}

// TestShellCancellation closes a borrowed SSH connection on revocation and unblocks a waiting shell reader.
func TestShellCancellation(t *testing.T) {
	server, started := shellServer(t, sftptest.Options{})
	fsys := dial(t, server)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	term, err := fsys.Shell(ctx, "/", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, term)
		done <- err
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("revoked shell reader remained blocked")
	}
	if _, err := fsys.Stat(t.Context(), "/"); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Fatalf("revoked connection: %v", err)
	}
	_ = term.Close()
}
