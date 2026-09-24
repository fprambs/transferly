package httpapi

import (
	"context"
	"time"
)

const readinessTimeout = 2 * time.Second

// server implements [StrictServerInterface] on top of the core ports.
type server struct {
	deps Deps
}

// newServer returns the strict server for the given dependencies.
func newServer(d Deps) *server {
	return &server{deps: d}
}

// GetHealth answers liveness probes; it never touches dependencies.
func (s *server) GetHealth(context.Context, GetHealthRequestObject) (GetHealthResponseObject, error) {
	return GetHealth200JSONResponse{Status: Ok}, nil
}

// GetReadiness answers readiness probes by pinging the readiness checker within a timeout.
// Failures are logged and reported as 503 without exposing internal details.
func (s *server) GetReadiness(ctx context.Context, _ GetReadinessRequestObject) (GetReadinessResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()

	if err := s.deps.Readiness.Ping(ctx); err != nil {
		s.deps.Logger.WarnContext(ctx, "readiness check failed", "error", err)
		return GetReadiness503JSONResponse{Status: Unavailable}, nil
	}
	return GetReadiness200JSONResponse{Status: Ok}, nil
}
