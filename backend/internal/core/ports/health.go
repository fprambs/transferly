// Package ports declares the interfaces the core requires from the outside world.
package ports

import "context"

// ReadinessChecker reports whether an external dependency can serve traffic.
type ReadinessChecker interface {
	// Ping returns nil when the dependency is reachable.
	Ping(ctx context.Context) error
}
