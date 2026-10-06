package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// TestSiteDraft tries a site as a form holds it, without saving it: it connects with the settings and secrets given, the stored
// secrets of the site it edits filling the ones left out, lists the folder the server opens sessions in, or the default folder of
// the site, and disconnects. Attempts are limited like quick connections, so it cannot be used to sweep servers.
func (s *server) TestSiteDraft(ctx context.Context, request TestSiteDraftRequestObject) (TestSiteDraftResponseObject, error) {
	p, _ := principalFrom(ctx)
	if ok, wait := s.deps.Limits.QuickConnect.Allow("quick:" + p.User.ID.String()); !ok {
		return TestSiteDraft429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	settings, secrets := siteInput(request.Body.Site)
	id := uuid.Nil
	if request.Body.SiteId != nil {
		id = *request.Body.SiteId
	}
	draft, opened, err := s.deps.Sites.Draft(ctx, p.User.ID, request.WorkspaceId, id, vaultKey(p), settings, secrets)
	if err == nil {
		var fsys remotefs.RemoteFS
		key := connections.Key{UserID: p.User.ID, WorkspaceID: request.WorkspaceId, SiteID: draft.ID}
		if fsys, err = s.deps.Probe.Probe(ctx, key, vaultKey(p), draft, opened); err == nil {
			defer func() { _ = fsys.Close() }()
			dir := draft.RemoteDir
			if homer, ok := fsys.(remotefs.Homer); ok && dir == "" {
				dir, err = homer.Home(ctx)
			}
			if dir == "" {
				dir = "/"
			}
			var entries []remotefs.Entry
			if err == nil {
				if entries, err = fsys.List(ctx, dir); err == nil {
					return TestSiteDraft200JSONResponse{Path: dir, Entries: len(entries)}, nil
				}
			}
		}
	}
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	return nil, err
}
