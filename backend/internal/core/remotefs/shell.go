package remotefs

import (
	"context"
	"io"
)

// Terminal is an interactive shell on a server (ADR-0027): what it prints is read from it, and what is typed is written to it.
type Terminal interface {
	io.Reader
	io.Writer
	// Resize tells the shell the size of the screen, in columns and rows.
	Resize(cols, rows int) error
	// Wait waits for the shell to end and returns its exit status.
	Wait() (int, error)
	// Close ends the shell.
	Close() error
}

// Sheller is implemented by connections that open interactive shells on their servers, as SSH servers that let the account run
// commands do.
type Sheller interface {
	// Shell opens a shell in the folder dir, or where the server starts sessions when dir is empty, on a screen of cols columns and
	// rows rows. A server that runs no commands fails with [ErrUnsupported].
	Shell(ctx context.Context, dir string, cols, rows int) (Terminal, error)
}
