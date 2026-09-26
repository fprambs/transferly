// Package remotefs defines the file system port every protocol adapter implements.
package remotefs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"
)

// Precision tells how exact an entry's modification time is.
type Precision int

const (
	// PrecisionSecond means the time is exact to the second.
	PrecisionSecond Precision = iota
	// PrecisionMinute means seconds are unknown, as in some FTP LIST listings.
	PrecisionMinute
	// PrecisionDay means only the date is known.
	PrecisionDay
)

// WriteMode selects how [RemoteFS.OpenWrite] treats an existing file.
type WriteMode int

const (
	// WriteCreate creates a new file and fails with [ErrExists] when it exists.
	WriteCreate WriteMode = iota
	// WriteOverwrite creates the file or truncates it.
	WriteOverwrite
	// WriteResume writes into an existing file starting at the given offset, leaving other bytes untouched.
	WriteResume
)

// Entry describes a file or directory.
type Entry struct {
	// Name is the last element of the path.
	Name string
	// Path is the absolute, slash separated path.
	Path string
	// Size is the size in bytes; meaningful for regular files.
	Size int64
	// Mode holds the type and permission bits.
	Mode fs.FileMode
	// ModTime is the modification time in UTC.
	ModTime time.Time
	// ModTimePrecision tells how exact ModTime is.
	ModTimePrecision Precision
}

// IsDir reports whether the entry is a directory.
func (e Entry) IsDir() bool { return e.Mode.IsDir() }

// Capabilities lists the optional features a protocol supports.
type Capabilities struct {
	// Resume is true when transfers can continue from an offset.
	Resume bool
	// Rewrite is true when a resumed write may start before the end of the file and leave the bytes after it untouched.
	// Without it a resumed write must start exactly at the end of the file, as with FTP.
	Rewrite bool
	// Chmod is true when permissions can be changed.
	Chmod bool
	// Chtimes is true when modification times can be changed.
	Chtimes bool
}

// Errors returned by adapters. Adapters wrap them with context, so callers must use [errors.Is].
var (
	// ErrNotFound means the path does not exist.
	ErrNotFound = errors.New("not found")
	// ErrExists means the path already exists.
	ErrExists = errors.New("already exists")
	// ErrPermission means the operation is not allowed.
	ErrPermission = errors.New("permission denied")
	// ErrNotDirectory means a directory was expected.
	ErrNotDirectory = errors.New("not a directory")
	// ErrNotEmpty means a directory still has entries.
	ErrNotEmpty = errors.New("directory not empty")
	// ErrInvalid means an argument is not valid for the operation.
	ErrInvalid = errors.New("invalid argument")
	// ErrAuth means authentication failed.
	ErrAuth = errors.New("authentication failed")
	// ErrHostKeyMismatch means the server identity differs from the trusted one.
	ErrHostKeyMismatch = errors.New("host key mismatch")
	// ErrHostKeyUnknown means the server presented a key that is not trusted yet; the user must confirm its fingerprint.
	ErrHostKeyUnknown = errors.New("host key not trusted yet")
	// ErrConnectionLost means the connection dropped; the operation may be retried after reconnecting.
	ErrConnectionLost = errors.New("connection lost")
	// ErrQuota means a storage quota was exceeded.
	ErrQuota = errors.New("quota exceeded")
)

// Retryable reports whether an operation that failed with err may succeed after reconnecting.
// Authentication, host key and permission failures are never retried.
func Retryable(err error) bool {
	return errors.Is(err, ErrConnectionLost)
}

// RemoteFS is a file system on a remote server, seen through one protocol.
// Paths are absolute and slash separated. Implementations must be safe for concurrent use.
type RemoteFS interface {
	// List returns the entries of a directory, in no particular order.
	List(ctx context.Context, dir string) ([]Entry, error)
	// Stat describes a single path.
	Stat(ctx context.Context, path string) (Entry, error)
	// OpenRead streams a file starting at offset.
	OpenRead(ctx context.Context, path string, offset int64) (io.ReadCloser, error)
	// OpenWrite streams data into a file according to mode; offset applies to [WriteResume].
	OpenWrite(ctx context.Context, path string, offset int64, mode WriteMode) (io.WriteCloser, error)
	// Mkdir creates one directory; its parent must exist.
	Mkdir(ctx context.Context, path string) error
	// Remove deletes a file or an empty directory.
	Remove(ctx context.Context, path string) error
	// RemoveAll deletes a path and everything below it; a missing path is not an error.
	RemoveAll(ctx context.Context, path string) error
	// Rename moves a path; the target must not exist.
	Rename(ctx context.Context, from, to string) error
	// Chmod changes permission bits.
	Chmod(ctx context.Context, path string, mode fs.FileMode) error
	// Chtimes changes the modification time.
	Chtimes(ctx context.Context, path string, mtime time.Time) error
	// Capabilities reports the optional features this adapter supports.
	Capabilities() Capabilities
	// Ping checks that the connection is alive.
	Ping(ctx context.Context) error
	// Close releases the connection; later operations fail with [ErrConnectionLost].
	Close() error
}

// HostKeyError reports the key a server presented when it was not accepted. It matches [ErrHostKeyUnknown]
// or [ErrHostKeyMismatch] with [errors.Is], so callers can ask the user to confirm the fingerprint or refuse.
type HostKeyError struct {
	// Type is the key algorithm, such as ssh-ed25519.
	Type string
	// Fingerprint is the SHA-256 fingerprint of the key in OpenSSH notation.
	Fingerprint string
	// Changed is true when a different key of the same type was trusted before.
	Changed bool
}

// Error describes the rejected key.
func (e *HostKeyError) Error() string {
	if e.Changed {
		return fmt.Sprintf("%v: %s %s", ErrHostKeyMismatch, e.Type, e.Fingerprint)
	}
	return fmt.Sprintf("%v: %s %s", ErrHostKeyUnknown, e.Type, e.Fingerprint)
}

// Is matches the sentinel that corresponds to the kind of rejection.
func (e *HostKeyError) Is(target error) bool {
	if e.Changed {
		return target == ErrHostKeyMismatch
	}
	return target == ErrHostKeyUnknown
}
