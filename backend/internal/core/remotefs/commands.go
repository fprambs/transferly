package remotefs

import (
	"context"
	"errors"
)

// ErrUnsupported means the server cannot do an operation on its own, so the caller falls back to moving the data itself.
var ErrUnsupported = errors.New("not supported by the server")

// Archive formats a server may create and extract.
const (
	// FormatZip is a ZIP archive.
	FormatZip = "zip"
	// FormatTarGz is a tar archive compressed with gzip.
	FormatTarGz = "tar.gz"
)

// Tools tells what a server can do on its own (ADR-0027).
type Tools struct {
	// Shell is true when the server runs commands for the account, so a terminal can open on it.
	Shell bool
	// Copy is true when files can be copied on the server.
	Copy bool
	// Checksums lists the algorithms the server computes, among md5, sha1, sha256 and sha512.
	Checksums []string
	// Archive lists the formats the server creates, among [FormatZip] and [FormatTarGz].
	Archive []string
	// Extract lists the formats the server extracts.
	Extract []string
	// Usage is true when the server measures the space a folder takes.
	Usage bool
}

// Commands is implemented by connections that can work on the server itself instead of moving the data through Transferly
// (ADR-0027), such as SFTP servers that let the account run commands. Each method fails with [ErrUnsupported] when the server
// cannot do it, and callers then fall back to the operations of [RemoteFS].
type Commands interface {
	// Tools reports what the server can do. It probes the server once and keeps the answer for the life of the connection.
	Tools(ctx context.Context) (Tools, error)
	// ServerCopy copies the file from to the path to on the server, replacing a file there only when overwrite is true.
	ServerCopy(ctx context.Context, from, to string, overwrite bool) error
	// ServerChecksum returns the digest of a file, in lower case hexadecimal, computed by the server with algorithm.
	ServerChecksum(ctx context.Context, path, algorithm string) (string, error)
	// ServerUsage returns the bytes the file or folder at path takes on the disk of the server.
	ServerUsage(ctx context.Context, path string) (int64, error)
	// ServerArchive packs the entries names of the folder dir into a new archive at target, in format.
	ServerArchive(ctx context.Context, dir string, names []string, target, format string) error
	// ServerExtract unpacks the archive at archive, in format, into the existing folder dir, never replacing files there.
	ServerExtract(ctx context.Context, archive, format, dir string) error
}
