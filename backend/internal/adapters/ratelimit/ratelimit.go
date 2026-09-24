// Package ratelimit provides an in-memory token bucket rate limiter with bounded memory.
package ratelimit

import (
	"sync"
	"time"

	"github.com/fprambs/sentinel/internal/core/ports"
)

// Rule describes a bucket: it holds up to Burst tokens and gains one every Every.
type Rule struct {
	// Burst is the maximum number of actions allowed at once.
	Burst int
	// Every is the time it takes to earn one action back.
	Every time.Duration
}

// bucket is the state of one key.
type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter is a token bucket limiter keyed by string. It is safe for concurrent use.
//
// Memory is bounded by maxKeys: when full, idle buckets that have recovered all their tokens are dropped
// first, then the least recently used one, so an attacker cannot exhaust memory by inventing keys.
type Limiter struct {
	clock   ports.Clock
	rule    Rule
	maxKeys int
	mu      sync.Mutex
	buckets map[string]*bucket
}

var _ ports.RateLimiter = (*Limiter)(nil)

// New returns a limiter applying rule to each key, remembering at most maxKeys keys.
func New(clock ports.Clock, rule Rule, maxKeys int) *Limiter {
	return &Limiter{clock: clock, rule: rule, maxKeys: max(maxKeys, 1), buckets: map[string]*bucket{}}
}

// Allow implements [ports.RateLimiter].
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock.Now()
	b, ok := l.buckets[key]
	if !ok {
		l.makeRoom(now)
		b = &bucket{tokens: float64(l.rule.Burst), last: now}
		l.buckets[key] = b
	}
	l.refill(b, now)

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) * float64(l.rule.Every))
}

// Len returns how many keys are currently remembered.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// refill adds the tokens earned since the bucket was last used, up to the burst.
func (l *Limiter) refill(b *bucket, now time.Time) {
	earned := float64(now.Sub(b.last)) / float64(l.rule.Every)
	b.tokens = min(float64(l.rule.Burst), b.tokens+earned)
	b.last = now
}

// makeRoom frees space for one new key when the limiter is full.
func (l *Limiter) makeRoom(now time.Time) {
	if len(l.buckets) < l.maxKeys {
		return
	}
	for key, b := range l.buckets {
		l.refill(b, now)
		if b.tokens >= float64(l.rule.Burst) {
			delete(l.buckets, key)
		}
	}
	if len(l.buckets) < l.maxKeys {
		return
	}
	var oldest string
	var oldestSeen time.Time
	for key, b := range l.buckets {
		if oldest == "" || b.last.Before(oldestSeen) {
			oldest, oldestSeen = key, b.last
		}
	}
	delete(l.buckets, oldest)
}
