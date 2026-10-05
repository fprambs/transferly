package sftpfs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// Sudo is the sudo mode of a session (ADR-0027): the SFTP server of the host runs through sudo as another user, so every operation
// acts as that user, root by default.
type Sudo struct {
	// User is who sudo runs the SFTP server as.
	User string
	// Password answers sudo when it asks for one; empty works only where the account may use sudo without a password.
	Password string
}

// The texts sudo and the command write to standard error, which tell the session how sudo went.
const (
	sudoPrompt = "transferly-sudo-password:"
	sudoReady  = "transferly-sudo-ready"
)

// sftpServers are the places where systems keep the SFTP server of OpenSSH: Debian and Ubuntu, Red Hat and its family, Arch,
// macOS and BSDs.
var sftpServers = []string{
	"/usr/lib/openssh/sftp-server", "/usr/libexec/openssh/sftp-server", "/usr/lib/ssh/sftp-server",
	"/usr/libexec/sftp-server", "/usr/lib/sftp-server", "/usr/local/libexec/sftp-server",
}

// sudoCommand is the command line that starts the SFTP server through sudo as user: sudo reads the password, if it asks for one,
// from standard input with the known prompt, and the first SFTP server found says it is ready before it takes over.
func sudoCommand(user string) string {
	places := make([]string, len(sftpServers))
	for i, p := range sftpServers {
		places[i] = quote(p)
	}
	script := "for p in " + strings.Join(places, " ") + `; do if [ -x "$p" ]; then echo ` + sudoReady + ` >&2; exec "$p"; fi; done; echo 'no sftp-server' >&2; exit 127`
	return "sudo -S -p " + quote(sudoPrompt) + " -u " + quote(user) + " -- sh -c " + quote(script)
}

// sudoEvent is what standard error said: sudo asked for the password, the SFTP server is ready, or the command ended.
type sudoEvent int

const (
	askedPassword sudoEvent = iota
	serverReady
	commandEnded
)

// watchSudo reads the standard error of the command and tells, in order, each time sudo asks for the password and when the SFTP
// server is ready; then it keeps reading and dropping what comes, so the server never blocks on it. When standard error ends
// first, it tells that the command ended, with what it wrote.
func watchSudo(stderr io.Reader, events chan<- sudoEvent, said *bytes.Buffer) {
	buf := make([]byte, 4096)
	scanned := 0
	for {
		n, err := stderr.Read(buf)
		if said.Len() < maxStderr {
			said.Write(buf[:n])
		}
		for {
			text := said.String()[scanned:]
			prompt, ready := strings.Index(text, sudoPrompt), strings.Index(text, sudoReady)
			switch {
			case ready >= 0 && (prompt < 0 || ready < prompt):
				events <- serverReady
				_, _ = io.Copy(io.Discard, stderr)
				return
			case prompt >= 0:
				scanned += prompt + len(sudoPrompt)
				events <- askedPassword
				continue
			}
			break
		}
		if err != nil {
			events <- commandEnded
			return
		}
	}
}

// sudoClient starts the SFTP server of the host through sudo on a new session of client and returns the SFTP client that talks to
// it. A server that does not run commands, sudo that refuses the account or its password, and a host without an SFTP server fail
// with [remotefs.ErrElevationRefused], before anything else is sent; the wait is bounded by timeout and ctx.
func sudoClient(ctx context.Context, client *ssh.Client, s Sudo, timeout time.Duration, opts ...sftp.ClientOption) (*sftp.Client, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("%w: the server opened no session: %w", remotefs.ErrElevationRefused, err)
	}
	stdin, _ := session.StdinPipe()
	stdout, _ := session.StdoutPipe()
	stderr, _ := session.StderrPipe()
	if err := session.Start(sudoCommand(s.User)); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("%w: the server runs no commands: %w", remotefs.ErrElevationRefused, err)
	}
	events, said := make(chan sudoEvent, 4), &bytes.Buffer{}
	go watchSudo(stderr, events, said)
	refuse := func(reason string) error {
		_ = session.Close()
		return fmt.Errorf("%w: %s", remotefs.ErrElevationRefused, reason)
	}
	if timeout <= 0 {
		timeout = time.Minute
	}
	limit := time.NewTimer(timeout)
	defer limit.Stop()
	asked := false
	for {
		select {
		case event := <-events:
			switch event {
			case serverReady:
				c, err := sftp.NewClientPipe(stdout, stdin, opts...)
				if err != nil {
					_ = session.Close()
					return nil, fmt.Errorf("starting the sftp server through sudo: %w: %w", remotefs.ErrConnectionLost, err)
				}
				return c, nil
			case askedPassword:
				if asked {
					return nil, refuse("sudo refused the password")
				}
				if s.Password == "" {
					return nil, refuse("sudo asks for a password and none is set")
				}
				asked = true
				_, _ = io.WriteString(stdin, s.Password+"\n")
			case commandEnded:
				return nil, refuse(strings.TrimSpace(said.String()))
			}
		case <-limit.C:
			_ = session.Close()
			return nil, fmt.Errorf("%w: sudo did not answer within %v", remotefs.ErrConnectionLost, timeout)
		case <-ctx.Done():
			_ = session.Close()
			return nil, ctx.Err()
		}
	}
}
