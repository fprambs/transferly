package postgres

import (
	"context"

	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs"
)

var _ sftpfs.StrictStore = (*Store)(nil)

// StrictServers implements [sftpfs.StrictStore].
func (s *Store) StrictServers(ctx context.Context) ([]string, error) {
	return s.queries.ListStrictServers(ctx)
}

// RememberStrictServer implements [sftpfs.StrictStore].
func (s *Store) RememberStrictServer(ctx context.Context, address string) error {
	return s.queries.RememberStrictServer(ctx, address)
}
