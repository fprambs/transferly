// Package connections keeps the connections to remote servers that users work with: a bounded pool per user
// and site, kept alive while idle, replaced when they break and re-established with backoff.
package connections

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// ErrClosed means the pool was closed, because the manager stopped or the user's connections were closed.
var ErrClosed = errors.New("connections closed")

// Key identifies a pool: one user working with one site of a workspace. Connections are never shared between
// users, so a user cannot use a connection that another user authenticated with credentials they cannot open.
type Key struct {
	// UserID is the user the connections belong to.
	UserID uuid.UUID
	// WorkspaceID is the workspace of the site.
	WorkspaceID uuid.UUID
	// SiteID is the site to connect to.
	SiteID uuid.UUID
}

// Connector opens a new connection to the site of a key.
type Connector interface {
	// Connect returns a ready file system. session identifies the user's session, whose unlocked vault key
	// opens the credentials of the site. Errors that mean the network failed wrap [remotefs.ErrConnectionLost].
	Connect(ctx context.Context, key Key, session string) (remotefs.RemoteFS, error)
}

// Config tunes the manager. Zero values select the defaults.
type Config struct {
	// MaxPerSite is the most connections one user keeps open to one site; 4 by default, which the throughput
	// spike showed to be worth it on high latency links.
	MaxPerSite int
	// IdleTimeout closes connections unused for this long; 5 minutes by default.
	IdleTimeout time.Duration
	// KeepAlive is how long a connection may sit idle before [Manager.Sweep] pings it; 30 seconds by default.
	KeepAlive time.Duration
	// ValidateAfter makes [Manager.Acquire] ping an idle connection unused for this long before handing it out;
	// 10 seconds by default.
	ValidateAfter time.Duration
	// PingTimeout bounds each ping; 10 seconds by default.
	PingTimeout time.Duration
	// Attempts is the number of tries to connect, the first included; 4 by default.
	Attempts int
	// InitialBackoff is the wait after the first failed attempt, doubled after each one; 200 milliseconds by default.
	InitialBackoff time.Duration
	// MaxBackoff caps the wait between attempts; 5 seconds by default.
	MaxBackoff time.Duration
	// Sleep waits for d or until ctx ends; nil uses a timer. Tests replace it.
	Sleep func(ctx context.Context, d time.Duration) error
	// Jitter returns a number in [0, 1) that spreads each wait by up to 20 percent; nil uses no jitter.
	Jitter func() float64
}

// withDefaults fills in the zero values.
func (c Config) withDefaults() Config {
	defaults := []struct {
		field *time.Duration
		value time.Duration
	}{
		{&c.IdleTimeout, 5 * time.Minute}, {&c.KeepAlive, 30 * time.Second}, {&c.ValidateAfter, 10 * time.Second},
		{&c.PingTimeout, 10 * time.Second}, {&c.InitialBackoff, 200 * time.Millisecond}, {&c.MaxBackoff, 5 * time.Second},
	}
	for _, d := range defaults {
		if *d.field <= 0 {
			*d.field = d.value
		}
	}
	if c.MaxPerSite <= 0 {
		c.MaxPerSite = 4
	}
	if c.Attempts <= 0 {
		c.Attempts = 4
	}
	if c.Sleep == nil {
		c.Sleep = sleep
	}
	if c.Jitter == nil {
		c.Jitter = func() float64 { return 0.5 }
	}
	return c
}

// sleep waits for d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// conn is one pooled connection.
type conn struct {
	fs       remotefs.RemoteFS
	lastUsed time.Time
	lastPing time.Time
}

// pool holds the connections of one key. Every field but users is guarded by mu; users is guarded by the
// mutex of the manager and counts the acquisitions in progress, which keep the pool from being forgotten.
type pool struct {
	mu      sync.Mutex
	idle    []*conn
	total   int
	changed chan struct{}
	closed  bool
	users   int
}

// notify wakes every goroutine waiting for a change; the caller holds the lock.
func (p *pool) notify() {
	close(p.changed)
	p.changed = make(chan struct{})
}

// Manager pools connections. It is safe for concurrent use.
type Manager struct {
	connector Connector
	clock     ports.Clock
	cfg       Config

	mu     sync.Mutex
	pools  map[Key]*pool
	closed bool
}

// New returns a manager that opens connections with connector.
func New(connector Connector, clock ports.Clock, cfg Config) *Manager {
	return &Manager{connector: connector, clock: clock, cfg: cfg.withDefaults(), pools: map[Key]*pool{}}
}

// pool returns the pool of a key for an acquisition, creating it, or nil when the manager is closed.
// The caller gives it back with [Manager.unuse].
func (m *Manager) pool(key Key) *pool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	p, ok := m.pools[key]
	if !ok {
		p = &pool{changed: make(chan struct{})}
		m.pools[key] = p
	}
	p.users++
	return p
}

// unuse ends an acquisition that started with [Manager.pool].
func (m *Manager) unuse(p *pool) {
	m.mu.Lock()
	p.users--
	m.mu.Unlock()
}

// Lease is a connection borrowed from the pool. Give it back with [Lease.Release].
type Lease struct {
	m    *Manager
	p    *pool
	c    *conn
	once sync.Once
}

// FS returns the connection. It must not be used after [Lease.Release].
func (l *Lease) FS() remotefs.RemoteFS { return l.c.fs }

// Release gives the connection back. Pass the error of the last operation: a lost connection discards the
// connection instead of pooling it. Releasing twice has no effect.
func (l *Lease) Release(err error) {
	l.once.Do(func() { l.m.release(l.p, l.c, err) })
}

// release returns a connection to its pool, or closes it when it broke or the pool is gone.
func (m *Manager) release(p *pool, c *conn, err error) {
	c.lastUsed = m.clock.Now()
	if remotefs.Retryable(err) {
		m.drop(p, c)
		return
	}
	m.putIdle(p, c)
}

// putIdle adds a connection to the idle list, or closes it when the pool is gone.
func (m *Manager) putIdle(p *pool, c *conn) {
	p.mu.Lock()
	if p.closed {
		p.total--
		p.notify()
		p.mu.Unlock()
		_ = c.fs.Close()
		return
	}
	p.idle = append(p.idle, c)
	p.notify()
	p.mu.Unlock()
}

// drop closes a connection that left the idle list and frees its place in the pool.
func (m *Manager) drop(p *pool, c *conn) {
	p.mu.Lock()
	p.total--
	p.notify()
	p.mu.Unlock()
	_ = c.fs.Close()
}

// ping checks that a connection still answers.
func (m *Manager) ping(ctx context.Context, c *conn) error {
	ctx, cancel := context.WithTimeout(ctx, m.cfg.PingTimeout)
	defer cancel()
	return c.fs.Ping(ctx)
}

// Acquire borrows a connection for the site of key, reusing an idle one, opening a new one while the pool has
// room, or waiting for one to be released. session identifies the user's session for opening credentials.
func (m *Manager) Acquire(ctx context.Context, key Key, session string) (*Lease, error) {
	p := m.pool(key)
	if p == nil {
		return nil, ErrClosed
	}
	defer m.unuse(p)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p.mu.Lock()
		switch {
		case p.closed:
			p.mu.Unlock()
			return nil, ErrClosed
		case len(p.idle) > 0:
			c := p.idle[len(p.idle)-1]
			p.idle = p.idle[:len(p.idle)-1]
			p.mu.Unlock()
			if m.clock.Now().Sub(c.lastUsed) >= m.cfg.ValidateAfter && m.ping(ctx, c) != nil {
				m.drop(p, c)
				continue
			}
			return &Lease{m: m, p: p, c: c}, nil
		case p.total < m.cfg.MaxPerSite:
			p.total++
			p.mu.Unlock()
			fsys, err := m.connect(ctx, key, session)
			if err != nil {
				p.mu.Lock()
				p.total--
				p.notify()
				p.mu.Unlock()
				return nil, err
			}
			return &Lease{m: m, p: p, c: &conn{fs: fsys, lastUsed: m.clock.Now()}}, nil
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// connect opens a connection, retrying network failures with exponential backoff and jitter.
// Failures that repeating cannot fix, such as bad credentials or a changed host key, are returned at once.
func (m *Manager) connect(ctx context.Context, key Key, session string) (remotefs.RemoteFS, error) {
	delay := m.cfg.InitialBackoff
	for attempt := 1; ; attempt++ {
		fsys, err := m.connector.Connect(ctx, key, session)
		if err == nil {
			return fsys, nil
		}
		if attempt >= m.cfg.Attempts || !remotefs.Retryable(err) || ctx.Err() != nil {
			return nil, err
		}
		wait := time.Duration(float64(delay) * (0.8 + 0.4*m.cfg.Jitter()))
		if err := m.cfg.Sleep(ctx, wait); err != nil {
			return nil, err
		}
		delay = min(delay*2, m.cfg.MaxBackoff)
	}
}

// With runs fn on a pooled connection and releases it. When fn fails because the connection was lost it runs
// once more on a fresh connection, so fn must be safe to repeat.
func (m *Manager) With(ctx context.Context, key Key, session string, fn func(remotefs.RemoteFS) error) error {
	for attempt := 0; ; attempt++ {
		lease, err := m.Acquire(ctx, key, session)
		if err != nil {
			return err
		}
		err = fn(lease.FS())
		lease.Release(err)
		if err == nil || !remotefs.Retryable(err) || attempt > 0 {
			return err
		}
	}
}

// Stats reports how many connections a key has idle and in total, borrowed ones included.
func (m *Manager) Stats(key Key) (idle, total int) {
	m.mu.Lock()
	p, ok := m.pools[key]
	m.mu.Unlock()
	if !ok {
		return 0, 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.idle), p.total
}

// Sweep closes connections idle for longer than the idle timeout, pings those idle for longer than the keepalive
// interval and closes the ones that do not answer, and forgets pools without connections. It returns how many
// connections it closed. [Manager.Run] calls it periodically.
func (m *Manager) Sweep(ctx context.Context) int {
	m.mu.Lock()
	pools := make([]*pool, 0, len(m.pools))
	for _, p := range m.pools {
		pools = append(pools, p)
	}
	m.mu.Unlock()

	closed := 0
	for _, p := range pools {
		closed += m.sweepPool(ctx, p)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for key, p := range m.pools {
		p.mu.Lock()
		if p.total == 0 && p.users == 0 {
			delete(m.pools, key)
		}
		p.mu.Unlock()
	}
	return closed
}

// sweepPool applies the idle and keepalive rules to one pool.
func (m *Manager) sweepPool(ctx context.Context, p *pool) int {
	now := m.clock.Now()
	var expired, check []*conn
	p.mu.Lock()
	keep := p.idle[:0]
	for _, c := range p.idle {
		switch {
		case now.Sub(c.lastUsed) >= m.cfg.IdleTimeout:
			expired = append(expired, c)
		case now.Sub(later(c.lastUsed, c.lastPing)) >= m.cfg.KeepAlive:
			check = append(check, c)
		default:
			keep = append(keep, c)
		}
	}
	p.idle = keep
	p.mu.Unlock()

	closed := 0
	for _, c := range expired {
		m.drop(p, c)
		closed++
	}
	for _, c := range check {
		if m.ping(ctx, c) != nil {
			m.drop(p, c)
			closed++
			continue
		}
		c.lastPing = m.clock.Now()
		m.putIdle(p, c)
	}
	return closed
}

// later returns the more recent of two instants.
func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// Run sweeps every interval until ctx ends, then closes every connection.
func (m *Manager) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.Close()
			return
		case <-ticker.C:
			m.Sweep(ctx)
		}
	}
}

// closePools closes the given pools: idle connections now, borrowed ones when they are released.
func (m *Manager) closePools(pools []*pool) {
	for _, p := range pools {
		p.mu.Lock()
		idle := p.idle
		p.idle = nil
		p.total -= len(idle)
		p.closed = true
		p.notify()
		p.mu.Unlock()
		for _, c := range idle {
			_ = c.fs.Close()
		}
	}
}

// CloseUser closes the connections of a user, for example when they lock their vault or sign out.
// Connections in use are closed when released, and callers waiting for one get [ErrClosed].
func (m *Manager) CloseUser(userID uuid.UUID) {
	m.mu.Lock()
	var doomed []*pool
	for key, p := range m.pools {
		if key.UserID == userID {
			doomed = append(doomed, p)
			delete(m.pools, key)
		}
	}
	m.mu.Unlock()
	m.closePools(doomed)
}

// Close closes every connection and makes later acquisitions fail with [ErrClosed].
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	doomed := make([]*pool, 0, len(m.pools))
	for key, p := range m.pools {
		doomed = append(doomed, p)
		delete(m.pools, key)
	}
	m.mu.Unlock()
	m.closePools(doomed)
}
