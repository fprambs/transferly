// Package clock provides [ports.Clock] implementations.
package clock

import "time"

// System is a clock backed by the operating system.
type System struct{}

// Now returns the current instant in UTC.
func (System) Now() time.Time { return time.Now().UTC() }
