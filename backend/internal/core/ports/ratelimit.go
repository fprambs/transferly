package ports

import "time"

// RateLimiter decides whether an actor identified by a key may perform one more action now.
type RateLimiter interface {
	// Allow consumes one unit for key. When it is refused, retryAfter tells how long to wait.
	Allow(key string) (allowed bool, retryAfter time.Duration)
}
