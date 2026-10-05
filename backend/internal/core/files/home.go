package files

import (
	"context"

	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/workspace"
)

// Home returns the folder a connection starts in when its site names none: the folder its server opened the session in, as an
// SFTP or FTP client shows with pwd, or the root for storages that have no such folder. Asking needs only to browse.
func (s *Service) Home(ctx context.Context, t Target) (string, error) {
	return run(ctx, s, t, workspace.ActionBrowse, nil, func(fsys remotefs.RemoteFS) (string, error) {
		if homer, ok := fsys.(remotefs.Homer); ok {
			return homer.Home(ctx)
		}
		return "/", nil
	})
}
