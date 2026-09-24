package ports

import "time"

// Clock provides the current time. Implementations return instants in UTC.
type Clock interface {
	// Now returns the current instant in UTC.
	Now() time.Time
}
