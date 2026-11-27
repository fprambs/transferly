package mcpaccess

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// List reads metadata for a scoped target during one unlock generation.
// The read function must use the connection pool belonging to that generation.
func (u *Unlocks) List(
	ctx context.Context, link uuid.UUID, window *Window, scope Scope, target Target,
	check CheckBrowse, read func(context.Context, Target) ([]remotefs.Entry, error),
) (Listing, error) {
	if err := u.Admit(link, window, false); err != nil {
		return Listing{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	stopCancellation := context.AfterFunc(window.Context(), cancel)
	defer stopCancellation()
	first := true
	return ReadListing(ctx, func(ctx context.Context) error {
		if ctx.Err() != nil {
			return ErrUnavailable
		}
		if err := scope.Authorize(ctx, target, check); err != nil {
			return err
		}
		err := u.Admit(link, window, first)
		first = false
		return err
	}, func(ctx context.Context) ([]remotefs.Entry, error) {
		return read(ctx, target)
	})
}
