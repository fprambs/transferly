package httpapi

import (
	"context"

	"github.com/fprambs/transferly/internal/version"
)

// GetVersion answers the version of this instance.
func (s *server) GetVersion(context.Context, GetVersionRequestObject) (GetVersionResponseObject, error) {
	return GetVersion200JSONResponse{Version: version.Version}, nil
}
