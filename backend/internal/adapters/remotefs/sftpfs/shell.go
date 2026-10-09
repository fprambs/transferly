package sftpfs

import (
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

var _ remotefs.Sheller = (*FS)(nil)

// terminalType is the terminal the shell is told it runs in, which the xterm.js of the interface emulates.
const terminalType = "xterm-256color"

// terminalModes are the modes of the pseudo terminal: echo on, and the usual line speeds.
var terminalModes = ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 115200, ssh.TTY_OP_OSPEED: 115200}

// shellCommand is the command that starts the login shell of the account, in the folder dir when it is given: the shell runs in
// place of the one that read the command, so its exit is the session's.
func shellCommand(dir string) (string, error) {
	start := `exec "${SHELL:-/bin/sh}" -l`
	if dir == "" {
		return start, nil
	}
	folder, err := arg(dir)
	if err != nil {
		return "", err
	}
	return "cd " + folder + " 2>/dev/null; " + start, nil
}

// terminal is a shell on a session with a pseudo terminal, whose standard output and error both arrive through the terminal.
type terminal struct {
	session    *ssh.Session
	in         io.WriteCloser
	out        io.Reader
	stopCancel func() bool
}

// Read implements io.Reader.
func (t *terminal) Read(p []byte) (int, error) { return t.out.Read(p) }

// Write implements io.Writer.
func (t *terminal) Write(p []byte) (int, error) { return t.in.Write(p) }

// Resize implements [remotefs.Terminal].
func (t *terminal) Resize(cols, rows int) error { return t.session.WindowChange(rows, cols) }

// Wait implements [remotefs.Terminal]: the exit status of the shell, or -1 when it ended without one, as when it was closed.
func (t *terminal) Wait() (int, error) {
	err := t.session.Wait()
	var exit *ssh.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		return exit.ExitStatus(), nil
	}
	return -1, err
}

// Close implements io.Closer.
func (t *terminal) Close() error {
	t.stopCancel()
	return t.session.Close()
}

// Shell implements [remotefs.Sheller] with a new session of the connection that asks for a pseudo terminal and starts the login
// shell of the account. A server the probe found runs no commands, such as one that forces internal-sftp and would answer with
// SFTP, is unsupported. In sudo mode the shell still runs as the account, which may use sudo in it.
func (f *FS) Shell(ctx context.Context, dir string, cols, rows int) (remotefs.Terminal, error) {
	if err := f.guard(ctx); err != nil {
		return nil, err
	}
	command, err := shellCommand(dir)
	if err != nil {
		return nil, err
	}
	stopCancel := context.AfterFunc(ctx, func() {
		_ = f.Close()
	})
	opened := false
	defer func() {
		if !opened {
			stopCancel()
		}
	}()
	if tools, err := f.Tools(ctx); err != nil || !tools.Shell {
		return nil, fmt.Errorf("shell: %w: the server runs no commands for this account", remotefs.ErrUnsupported)
	}
	session, err := f.ssh.NewSession()
	if err != nil {
		return nil, fmt.Errorf("shell: %w: %w", remotefs.ErrUnsupported, err)
	}
	in, _ := session.StdinPipe()
	out, _ := session.StdoutPipe()
	if err := session.RequestPty(terminalType, rows, cols, terminalModes); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("shell: %w: the server gives no terminal: %w", remotefs.ErrUnsupported, err)
	}
	if err := session.Start(command); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("shell: %w: the server runs no commands: %w", remotefs.ErrUnsupported, err)
	}
	opened = true
	return &terminal{session: session, in: in, out: out, stopCancel: stopCancel}, nil
}
