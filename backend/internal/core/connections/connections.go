// Package connections keeps the connections to remote servers that users work with: a bounded pool per user
// and site, kept alive while idle, replaced when they break and re-established with backoff.
package connections

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/remotefs"
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
	// IsolationID separates an unlock generation from other pools; zero uses the shared user pool.
	IsolationID uuid.UUID
}

// Connector opens a new connection to the site of a key.
type Connector interface {
	// Connect returns a ready file system. session identifies the user's session, whose unlocked vault key
	// opens the credentials of the site. Errors that mean the network failed wrap [remotefs.ErrConnectionLost].
	Connect(ctx context.Context, key Key, session string) (remotefs.RemoteFS, error)
}

// Admitter is a [Connector] that also decides whether a session may use a connection of the pool at all, even one
// already open: a site whose password was typed in one session must not serve another session of the same user.
type Admitter interface {
	// Admit returns an error when the session may not use the connections of the key.
	Admit(ctx context.Context, key Key, session string) error
}

// Limiter is a [Connector] that also caps the connections of each site, for servers that allow few sessions or that sit
// behind a balancer whose servers have keys of their own.
type Limiter interface {
	// MaxConnections returns the most connections a user may keep open to the site of the key, or 0 for the default.
	MaxConnections(ctx context.Context, key Key) int
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
	// Grace is how long an acquisition that finds every connection of the pool busy waits for one to be released before it
	// opens another; 250 milliseconds by default. Short operations then share one connection, and servers behind a balancer,
	// each with its own key, are not reached through a new connection for every request.
	Grace time.Duration
	// Abandon is how long a connection goes on opening once no acquisition waits for it any more; 2 seconds by default. A
	// request the browser drops and sends again at once, as when the explorer moves to another folder while connecting, finds
	// the connection still opening, with the question the server asked the person connecting still shown; a request dropped for
	// good, as when its tab closes, does not leave a connection to the site behind.
	Abandon time.Duration
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
	// OnChange is told when a key gets its first open connection or loses its last one, so clients can show which
	// sites are connected (4.11); nil tells nothing. It must not block.
	OnChange func(state State)
}

// EventState is the type of the events that carry a [State].
const EventState = "connection.state"

// PublishTo returns a [Config.OnChange] that tells each change to the user of the key through sink, as an event of
// type [EventState].
func PublishTo(sink ports.EventSink) func(State) {
	return func(state State) {
		sink.Publish(ports.Event{Type: EventState, UserID: state.Key.UserID, Data: state})
	}
}

// State says whether a user has connections open to a site.
type State struct {
	// Key is the user and site.
	Key Key
	// Open says at least one connection is open.
	Open bool
}

// withDefaults fills in the zero values.
func (c Config) withDefaults() Config {
	defaults := []struct {
		field *time.Duration
		value time.Duration
	}{
		{&c.IdleTimeout, 5 * time.Minute}, {&c.KeepAlive, 30 * time.Second}, {&c.ValidateAfter, 10 * time.Second},
		{&c.PingTimeout, 10 * time.Second}, {&c.InitialBackoff, 200 * time.Millisecond}, {&c.MaxBackoff, 5 * time.Second},
		{&c.Grace, 250 * time.Millisecond}, {&c.Abandon, 2 * time.Second},
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

// dial is a connection being opened for a pool, which other acquisitions wait for instead of opening their own. Guarded
// by the mutex of the pool, waiters counts the acquisitions waiting for it, the one that started it included, finished says
// it ended, conn holds the connection once it opened until one of them takes it, and openerGone says the one that started it
// stopped waiting, which lets the others take it; cancel stops it.
type dial struct {
	done       chan struct{}
	err        error
	cancel     context.CancelFunc
	waiters    int
	finished   bool
	conn       *conn
	openerGone bool
	aborted    bool
}

// pool holds the connections of one key. Every field but users is guarded by mu; users is guarded by the
// mutex of the manager and counts the acquisitions in progress, which keep the pool from being forgotten.
type pool struct {
	key     Key
	mu      sync.Mutex
	idle    []*conn
	total   int
	dialing *dial
	changed chan struct{}
	closed  bool
	users   int

	announcing sync.Mutex
	announced  bool
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

	// life ends with [Manager.Close], which stops the connections still opening for acquisitions that went away.
	life context.Context
	end  context.CancelFunc

	mu     sync.Mutex
	pools  map[Key]*pool
	closed bool
}

// New returns a manager that opens connections with connector.
func New(connector Connector, clock ports.Clock, cfg Config) *Manager {
	life, end := context.WithCancel(context.Background())
	return &Manager{connector: connector, clock: clock, cfg: cfg.withDefaults(), life: life, end: end, pools: map[Key]*pool{}}
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
		p = &pool{key: key, changed: make(chan struct{})}
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

// release returns a connection to its pool, or closes it when it broke or the pool is gone. An operation that was
// canceled or ran out of time also closes it: FTP can only interrupt a command by cutting its connection, so what it
// leaves behind cannot be trusted for the next operation.
func (m *Manager) release(p *pool, c *conn, err error) {
	c.lastUsed = m.clock.Now()
	if remotefs.Retryable(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
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
		m.announce(p)
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
	m.announce(p)
}

// announce tells [Config.OnChange] when the pool became open or closed since it last told; announcements of one pool
// are made one at a time, so they arrive in the order the changes happened.
func (m *Manager) announce(p *pool) {
	if m.cfg.OnChange == nil {
		return
	}
	p.announcing.Lock()
	defer p.announcing.Unlock()
	p.mu.Lock()
	open := !p.closed && p.total > 0
	p.mu.Unlock()
	if open != p.announced {
		p.announced = open
		m.cfg.OnChange(State{Key: p.key, Open: open})
	}
}

// Open returns the sites of a workspace to which a user has connections open.
func (m *Manager) Open(userID, workspaceID uuid.UUID) []uuid.UUID {
	m.mu.Lock()
	var pools []*pool
	for key, p := range m.pools {
		if key.UserID == userID && key.WorkspaceID == workspaceID {
			pools = append(pools, p)
		}
	}
	m.mu.Unlock()
	open := []uuid.UUID{}
	for _, p := range pools {
		p.mu.Lock()
		if !p.closed && p.total > 0 {
			open = append(open, p.key.SiteID)
		}
		p.mu.Unlock()
	}
	return open
}

// ping checks that a connection still answers.
func (m *Manager) ping(ctx context.Context, c *conn) error {
	ctx, cancel := context.WithTimeout(ctx, m.cfg.PingTimeout)
	defer cancel()
	return c.fs.Ping(ctx)
}

// Acquire borrows a connection for the site of key, reusing an idle one, or waiting for one to be released. A new
// connection is opened when the pool has room and has none, or when every connection stayed busy for [Config.Grace].
// Only one connection of a pool is opened at a time: acquisitions that come meanwhile wait for it and share its failure,
// so a server is not asked to sign in several times at once and its log shows one connection. session identifies the
// user's session for opening credentials; a connector that is an [Admitter] may refuse the session before any
// connection is handed out.
func (m *Manager) Acquire(ctx context.Context, key Key, session string) (*Lease, error) {
	if admitter, ok := m.connector.(Admitter); ok {
		if err := admitter.Admit(ctx, key, session); err != nil {
			return nil, err
		}
	}
	p := m.pool(key)
	if p == nil {
		return nil, ErrClosed
	}
	defer m.unuse(p)
	var grace <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	graceOver := false
	limit := 0
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
		case p.dialing != nil:
			d := p.dialing
			d.waiters++
			p.mu.Unlock()
			c, err := m.await(ctx, p, d, false)
			switch {
			case err != nil:
				return nil, err
			case c != nil:
				return &Lease{m: m, p: p, c: c}, nil
			}
			continue
		case p.total == 0 || (graceOver && p.total < limit):
			dialCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
			d := &dial{done: make(chan struct{}), cancel: cancel, waiters: 1}
			p.dialing = d
			p.total++
			p.mu.Unlock()
			go m.open(dialCtx, p, d, session)
			c, err := m.await(ctx, p, d, true)
			switch {
			case err != nil:
				return nil, err
			case c != nil:
				return &Lease{m: m, p: p, c: c}, nil
			}
			continue
		}
		changed := p.changed
		total := p.total
		p.mu.Unlock()
		if limit == 0 {
			limit = m.maxOf(ctx, key)
		}
		room := total < limit
		if room && grace == nil {
			timer = time.NewTimer(m.cfg.Grace)
			grace = timer.C
		}
		select {
		case <-changed:
		case <-grace:
			graceOver = true
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// await waits, as one of its waiters, for the dial d of pool p to end, and returns the connection it opened when this waiter
// takes it, or its failure. The acquisition that started the dial, the opener, takes the connection; the others take it only
// once the opener stopped waiting. A dial stopped because nobody waited for it is no failure; with neither a connection nor a
// failure the caller looks at the pool again. An acquisition whose context ends before the dial does stops waiting at once;
// when it was the last waiter, the dial goes on for [Config.Abandon] and stops unless another acquisition comes for it.
func (m *Manager) await(ctx context.Context, p *pool, d *dial, opener bool) (*conn, error) {
	select {
	case <-d.done:
	case <-ctx.Done():
	}
	p.mu.Lock()
	d.waiters--
	if !d.finished {
		d.openerGone = d.openerGone || opener
		last := d.waiters == 0
		p.mu.Unlock()
		if last {
			time.AfterFunc(m.cfg.Abandon, func() {
				p.mu.Lock()
				abandoned := d.waiters == 0
				p.mu.Unlock()
				if abandoned {
					d.cancel()
				}
			})
		}
		return nil, ctx.Err()
	}
	var c *conn
	if opener || d.openerGone {
		c, d.conn = d.conn, nil
	}
	p.mu.Unlock()
	if c == nil && d.err != nil && !d.aborted {
		return nil, d.err
	}
	return c, nil
}

// open opens the connection of a dial that the caller registered in its pool, hands it to the acquisitions waiting for it, or
// puts it with the idle ones when none waits, and tells them. It runs on its own, with a context that keeps the values of the acquisition that started it
// but not its end: it stops when [Manager.await] finds nobody waiting for it, or when the manager closes. A request that
// goes away and comes back at once, such as a listing the browser sends again for another folder, then does not end the
// connection half way, which would withdraw a question the server asked the person connecting and fail every acquisition
// sharing it. A connection that opens once its pool is closed is closed at once.
func (m *Manager) open(ctx context.Context, p *pool, d *dial, session string) {
	stop := context.AfterFunc(m.life, d.cancel)
	defer func() {
		stop()
		d.cancel()
	}()
	fsys, err := m.connect(ctx, p.key, session)
	p.mu.Lock()
	p.dialing = nil
	d.err = err
	d.aborted = ctx.Err() != nil
	d.finished = true
	closed := err == nil && p.closed
	switch {
	case err != nil || closed:
		p.total--
	case d.waiters > 0:
		d.conn = &conn{fs: fsys, lastUsed: m.clock.Now()}
	default:
		p.idle = append(p.idle, &conn{fs: fsys, lastUsed: m.clock.Now()})
	}
	close(d.done)
	p.notify()
	p.mu.Unlock()
	if closed {
		_ = fsys.Close()
	}
	if err == nil {
		m.announce(p)
	}
}

// maxOf is the most connections the pool of a key may hold: the limit of its site when the connector is a [Limiter]
// that gives one, or [Config.MaxPerSite].
func (m *Manager) maxOf(ctx context.Context, key Key) int {
	if limiter, ok := m.connector.(Limiter); ok {
		if n := limiter.MaxConnections(ctx, key); n > 0 {
			return min(n, m.cfg.MaxPerSite)
		}
	}
	return m.cfg.MaxPerSite
}

// MaxConnections reports the connection budget used to plan operations requiring two simultaneous leases.
func (m *Manager) MaxConnections(ctx context.Context, key Key) int { return m.maxOf(ctx, key) }

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
		m.announce(p)
	}
}

// CloseKey closes the connections of one site of one user, for example when a quick connection ends or is saved as
// a site. Connections in use are closed when released.
func (m *Manager) CloseKey(key Key) {
	m.mu.Lock()
	p, ok := m.pools[key]
	delete(m.pools, key)
	m.mu.Unlock()
	if ok {
		m.closePools([]*pool{p})
	}
}

// CloseSite closes the connections of one site for every user, for example when its settings change or it is deleted,
// so no connection opened with the old settings stays in use. Connections in use are closed when released.
func (m *Manager) CloseSite(siteID uuid.UUID) {
	m.mu.Lock()
	var closing []*pool
	for key, p := range m.pools {
		if key.SiteID == siteID {
			closing = append(closing, p)
			delete(m.pools, key)
		}
	}
	m.mu.Unlock()
	m.closePools(closing)
}

// CloseIsolation closes every pool for one unlock generation. Active leases close on release.
func (m *Manager) CloseIsolation(isolationID uuid.UUID) {
	if isolationID == uuid.Nil {
		return
	}
	m.mu.Lock()
	var closing []*pool
	for key, p := range m.pools {
		if key.IsolationID == isolationID {
			closing = append(closing, p)
			delete(m.pools, key)
		}
	}
	m.mu.Unlock()
	m.closePools(closing)
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
	m.end()
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
