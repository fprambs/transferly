package files

import (
	"context"
	"crypto/md5"  //nolint:gosec // offered to compare with published sums, not for security
	"crypto/sha1" //nolint:gosec // offered to compare with published sums, not for security
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// ErrUnsupportedAlgorithm means the checksum algorithm is not one of those offered.
var ErrUnsupportedAlgorithm = errors.New("unsupported checksum algorithm")

// ErrChecksumMismatch means a copy does not hold the bytes of its source.
var ErrChecksumMismatch = errors.New("checksum mismatch")

// algorithms are the checksums that can be computed, by name.
var algorithms = map[string]func() hash.Hash{
	"md5":    md5.New,
	"sha1":   sha1.New,
	"sha256": sha256.New,
	"sha512": sha512.New,
}

// verifyAlgorithm is the checksum that verifies copies.
const verifyAlgorithm = "sha256"

// Checksum is the digest of a remote file.
type Checksum struct {
	// Algorithm is the name of the algorithm: md5, sha1, sha256 or sha512.
	Algorithm string
	// Sum is the digest in lower case hexadecimal.
	Sum string
	// Size is the number of bytes that were read.
	Size int64
}

// Checksum reads a remote file through the server and returns its digest. Nothing is stored; the file is streamed through the
// hash, so the cost is reading the whole file, and canceling the context stops it.
func (s *Service) Checksum(ctx context.Context, t Target, p, algorithm string) (Checksum, error) {
	newHash, ok := algorithms[algorithm]
	if !ok {
		return Checksum{}, fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, algorithm)
	}
	lease, err := s.begin(ctx, t, workspace.ActionDownload, p)
	if err != nil {
		return Checksum{}, err
	}
	sum, err := digest(ctx, lease.FS(), p, newHash())
	lease.Release(err)
	if err != nil {
		return Checksum{}, err
	}
	return Checksum{Algorithm: algorithm, Sum: hex.EncodeToString(sum.hash.Sum(nil)), Size: sum.size}, nil
}

// digested is the outcome of hashing a file.
type digested struct {
	hash hash.Hash
	size int64
}

// digest streams a file through a hash.
func digest(ctx context.Context, fsys remotefs.RemoteFS, p string, h hash.Hash) (digested, error) {
	entry, err := fsys.Stat(ctx, p)
	if err == nil && entry.IsDir() {
		err = fmt.Errorf("%w: %s is a directory", remotefs.ErrInvalid, p)
	}
	if err != nil {
		return digested{}, err
	}
	src, err := fsys.OpenRead(ctx, p, 0)
	if err != nil {
		return digested{}, err
	}
	n, err := io.Copy(h, &countingReader{ctx: ctx, src: src})
	if closeErr := src.Close(); err == nil {
		err = closeErr
	}
	return digested{hash: h, size: n}, err
}

// verifyCopy reads the copy back and compares it with the digest of the bytes that were sent.
func (s *Service) verifyCopy(ctx context.Context, t Target, p string, sent hash.Hash) error {
	got, err := s.Checksum(ctx, t, p, verifyAlgorithm)
	if err != nil {
		return err
	}
	if got.Sum != hex.EncodeToString(sent.Sum(nil)) {
		return fmt.Errorf("%w: %s does not hold the bytes of its source", ErrChecksumMismatch, p)
	}
	return nil
}
