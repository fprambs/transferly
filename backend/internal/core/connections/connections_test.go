package connections_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/testutil"
)

// fakeFS is a connection whose health and closing can be observed and controlled.
type fakeFS struct {
	remotefs.RemoteFS
	id       int
	pingErr  atomic.Pointer[error]
	closed   atomic.Bool
	pinged   atomic.Int32
	pingWait chan struct{}
}

// Ping fails when told to, after waiting for pingWait if it is set.
func (f *fakeFS) Ping(ctx context.Context) error {
	f.pinged.Add(1)
	if f.pingWait != nil {
		select {
		case <-f.pingWait:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := f.pingErr.Load(); err != nil {
		return *err
	}
	return nil
}

// Close records that the connection was closed.
func (f *fakeFS) Close() error {
	f.closed.Store(true)
	return nil
}

// List is not used by the manager.
func (f *fakeFS) List(context.Context, string) ([]remotefs.Entry, error) { return nil, nil }

// OpenRead is not used by the manager.
func (f *fakeFS) OpenRead(context.Context, string, int64) (io.ReadCloser, error) { return nil, nil }

// Chmod is not used by the manager.
func (f *fakeFS) Chmod(context.Context, string, fs.FileMode) error { return nil }

// breakConn makes the connection fail its pings.
func (f *fakeFS) breakConn() {
	err := error(fmt.Errorf("%w: gone", remotefs.ErrConnectionLost))
	f.pingErr.Store(&err)
}

// connector opens fake connections, failing as scripted.
type connector struct {
	mu       sync.Mutex
	opened   []*fakeFS
	failures []error
	calls    int
	block    chan struct{}
	sessions []string
}

// Connect returns the next scripted failure or a new connection.
func (c *connector) Connect(ctx context.Context, _ connections.Key, session string) (remotefs.RemoteFS, error) {
	c.mu.Lock()
	c.calls++
	c.sessions = append(c.sessions, session)
	var failure error
	if len(c.failures) > 0 {
		failure, c.failures = c.failures[0], c.failures[1:]
	}
	block := c.block
	c.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if failure != nil {
		return nil, failure
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	f := &fakeFS{id: len(c.opened) + 1}
	c.opened = append(c.opened, f)
	return f, nil
}

// connection returns the n-th opened connection, counting from 1.
func (c *connector) connection(n int) *fakeFS {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.opened[n-1]
}

// count returns the number of connections opened so far.
func (c *connector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.opened)
}

// errLost is a retryable connection failure.
var errLost = fmt.Errorf("%w: network down", remotefs.ErrConnectionLost)

// fixture bundles a manager with its collaborators.
type fixture struct {
	m      *connections.Manager
	conn   *connector
	clock  *testutil.FakeClock
	key    connections.Key
	sleeps []time.Duration
	mu     sync.Mutex
}

// newFixture returns a manager with a fake connector, a fake clock and sleeps that only record.
func newFixture(t *testing.T, cfg connections.Config) *fixture {
	t.Helper()
	f := &fixture{conn: &connector{}, clock: testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		key: connections.Key{UserID: uuid.New(), WorkspaceID: uuid.New(), SiteID: uuid.New()}}
	if cfg.Sleep == nil {
		cfg.Sleep = func(_ context.Context, d time.Duration) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.sleeps = append(f.sleeps, d)
			return nil
		}
	}
	f.m = connections.New(f.conn, f.clock, cfg)
	t.Cleanup(f.m.Close)
	return f
}

// acquire borrows a connection or fails the test.
func (f *fixture) acquire(t *testing.T) *connections.Lease {
	t.Helper()
	lease, err := f.m.Acquire(t.Context(), f.key, "session")
	if err != nil {
		t.Fatalf("Acquire() = %v", err)
	}
	return lease
}

// TestManager_Reuse verifies pooling.
//
// Scenarios:
//   - a released connection is reused instead of opening another one, and the session is given to the connector
//   - a connection released after a lost connection, a cancellation or a timeout is discarded and replaced
//   - releasing twice has no effect
//   - Stats reports idle and total connections
//   - pools of different keys are independent
func TestManager_Reuse(t *testing.T) {
	f := newFixture(t, connections.Config{})
	first := f.acquire(t)
	if first.FS() == nil {
		t.Fatal("the lease has no file system")
	}
	first.Release(nil)
	first.Release(nil)
	if idle, total := f.m.Stats(f.key); idle != 1 || total != 1 {
		t.Errorf("Stats() = %d idle, %d total; want 1, 1", idle, total)
	}
	second := f.acquire(t)
	if f.conn.count() != 1 || f.conn.sessions[0] != "session" {
		t.Errorf("connections opened = %d, sessions = %v; want the first one reused", f.conn.count(), f.conn.sessions)
	}
	second.Release(fmt.Errorf("op: %w", remotefs.ErrConnectionLost))
	if !f.conn.connection(1).closed.Load() {
		t.Error("a broken connection was not closed")
	}
	if idle, total := f.m.Stats(f.key); idle != 0 || total != 0 {
		t.Errorf("Stats() after a break = %d, %d; want 0, 0", idle, total)
	}
	third := f.acquire(t)
	if f.conn.count() != 2 {
		t.Errorf("connections opened = %d; want a replacement", f.conn.count())
	}
	third.Release(errors.New("not found"))
	if idle, _ := f.m.Stats(f.key); idle != 1 {
		t.Error("a connection released after a normal error was not pooled")
	}
	for _, stopped := range []error{context.Canceled, fmt.Errorf("upload: %w", context.DeadlineExceeded)} {
		lease := f.acquire(t)
		lease.Release(stopped)
		if idle, _ := f.m.Stats(f.key); idle != 0 {
			t.Errorf("a connection released after %v was pooled", stopped)
		}
	}
	f.acquire(t).Release(nil)

	opened := f.conn.count()
	other := f.key
	other.SiteID = uuid.New()
	lease, err := f.m.Acquire(t.Context(), other, "session")
	if err != nil || f.conn.count() != opened+1 {
		t.Errorf("another site: Acquire() = %v with %d opened; want its own connection", err, f.conn.count())
	}
	lease.Release(nil)
	if idle, total := f.m.Stats(connections.Key{}); idle != 0 || total != 0 {
		t.Errorf("Stats(unknown) = %d, %d", idle, total)
	}
}

// TestManager_Limit verifies the bound per site.
//
// Scenarios:
//   - at most MaxPerSite connections are opened, further acquisitions wait
//   - a release wakes a waiter, which gets that connection
//   - a discarded connection frees room for a waiter to open a new one
//   - a waiter gives up when its context ends
//   - a failed connection attempt frees its place
func TestManager_Limit(t *testing.T) {
	f := newFixture(t, connections.Config{MaxPerSite: 2})
	a, b := f.acquire(t), f.acquire(t)

	got := make(chan *connections.Lease, 1)
	go func() {
		lease, _ := f.m.Acquire(t.Context(), f.key, "s")
		got <- lease
	}()
	select {
	case <-got:
		t.Fatal("a third connection was handed out beyond the limit")
	case <-time.After(50 * time.Millisecond):
	}
	a.Release(nil)
	third := <-got
	if f.conn.count() != 2 || third.FS() != a.FS() {
		t.Errorf("the waiter did not get the released connection (opened %d)", f.conn.count())
	}

	go func() {
		lease, _ := f.m.Acquire(t.Context(), f.key, "s")
		got <- lease
	}()
	time.Sleep(50 * time.Millisecond)
	b.Release(errLost)
	fourth := <-got
	if f.conn.count() != 3 || fourth.FS() == b.FS() {
		t.Errorf("the waiter did not open a replacement (opened %d)", f.conn.count())
	}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := f.m.Acquire(ctx, f.key, "s"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waiting past the deadline = %v; want DeadlineExceeded", err)
	}
	third.Release(nil)
	fourth.Release(nil)

	failing := newFixture(t, connections.Config{MaxPerSite: 1, Attempts: 1})
	failing.conn.failures = []error{errors.New("refused")}
	if _, err := failing.m.Acquire(t.Context(), failing.key, "s"); err == nil {
		t.Fatal("a failing connector succeeded")
	}
	if lease, err := failing.m.Acquire(t.Context(), failing.key, "s"); err != nil {
		t.Errorf("after a failed attempt the place stayed taken: %v", err)
	} else {
		lease.Release(nil)
	}
}

// TestManager_Backoff verifies reconnection.
//
// Scenarios:
//   - network failures are retried with doubling waits, capped, with jitter, until the connection opens
//   - the attempts are limited and the last error is returned
//   - credentials, host key and other non-retryable failures are returned at once without waiting
//   - a canceled context, or a sleep that fails, stops the retries
func TestManager_Backoff(t *testing.T) {
	f := newFixture(t, connections.Config{Attempts: 5, InitialBackoff: 100 * time.Millisecond, MaxBackoff: 300 * time.Millisecond, Jitter: func() float64 { return 0.5 }})
	f.conn.failures = []error{errLost, errLost, errLost, errLost}
	lease := f.acquire(t)
	lease.Release(nil)
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond, 300 * time.Millisecond}
	if fmt.Sprint(f.sleeps) != fmt.Sprint(want) {
		t.Errorf("waits = %v; want %v", f.sleeps, want)
	}

	spread := newFixture(t, connections.Config{Attempts: 3, InitialBackoff: 100 * time.Millisecond, Jitter: func() float64 { return 0 }})
	spread.conn.failures = []error{errLost, errLost}
	spread.acquire(t).Release(nil)
	if spread.sleeps[0] != 80*time.Millisecond || spread.sleeps[1] != 160*time.Millisecond {
		t.Errorf("waits without jitter = %v; want 80ms and 160ms", spread.sleeps)
	}
	high := newFixture(t, connections.Config{Attempts: 2, InitialBackoff: 100 * time.Millisecond, Jitter: func() float64 { return 0.999 }})
	high.conn.failures = []error{errLost}
	high.acquire(t).Release(nil)
	if high.sleeps[0] < 119*time.Millisecond || high.sleeps[0] > 120*time.Millisecond {
		t.Errorf("wait with maximal jitter = %v; want about 120ms", high.sleeps[0])
	}

	exhausted := newFixture(t, connections.Config{Attempts: 3})
	exhausted.conn.failures = []error{errLost, errLost, fmt.Errorf("%w: last", remotefs.ErrConnectionLost), errLost}
	if _, err := exhausted.m.Acquire(t.Context(), exhausted.key, "s"); !errors.Is(err, remotefs.ErrConnectionLost) || exhausted.conn.calls != 3 {
		t.Errorf("Acquire() = %v after %d calls; want the last error after 3", err, exhausted.conn.calls)
	}

	for _, permanent := range []error{remotefs.ErrAuth, &remotefs.HostKeyError{Type: "ssh-ed25519", Fingerprint: "SHA256:x"}, errors.New("site not found")} {
		p := newFixture(t, connections.Config{})
		p.conn.failures = []error{permanent}
		if _, err := p.m.Acquire(t.Context(), p.key, "s"); !errors.Is(err, permanent) || p.conn.calls != 1 || len(p.sleeps) != 0 {
			t.Errorf("%v: Acquire() = %v after %d calls and %d waits; want an immediate failure", permanent, err, p.conn.calls, len(p.sleeps))
		}
	}

	stop := errors.New("sleep interrupted")
	broken := newFixture(t, connections.Config{Sleep: func(context.Context, time.Duration) error { return stop }})
	broken.conn.failures = []error{errLost}
	if _, err := broken.m.Acquire(t.Context(), broken.key, "s"); !errors.Is(err, stop) {
		t.Errorf("Acquire() with a failing sleep = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	canceled := newFixture(t, connections.Config{})
	canceled.conn.failures = []error{errLost}
	canceled.conn.block = make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	if _, err := canceled.m.Acquire(ctx, canceled.key, "s"); !errors.Is(err, context.Canceled) {
		t.Errorf("Acquire() canceled while connecting = %v", err)
	}
	if _, err := canceled.m.Acquire(ctx, canceled.key, "s"); !errors.Is(err, context.Canceled) {
		t.Errorf("Acquire() with a canceled context = %v", err)
	}

	stopping := newFixture(t, connections.Config{})
	stopCtx, stopCancel := context.WithCancel(t.Context())
	stopping.conn.failures = []error{errLost}
	stopping.m = connections.New(cancelingConnector{inner: stopping.conn, cancel: stopCancel}, stopping.clock, connections.Config{Sleep: func(context.Context, time.Duration) error { return nil }})
	t.Cleanup(stopping.m.Close)
	if _, err := stopping.m.Acquire(stopCtx, stopping.key, "s"); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Acquire() whose context ended while connecting = %v; want the connection error, not a retry", err)
	}
	if stopping.conn.calls != 1 {
		t.Errorf("connect calls = %d; want no retry after the context ended", stopping.conn.calls)
	}
}

// cancelingConnector cancels a context while it fails, to end it in the middle of a connection attempt.
type cancelingConnector struct {
	inner  *connector
	cancel context.CancelFunc
}

// Connect cancels the context and then behaves as the wrapped connector.
func (c cancelingConnector) Connect(ctx context.Context, key connections.Key, session string) (remotefs.RemoteFS, error) {
	c.cancel()
	return c.inner.Connect(ctx, key, session)
}

// TestManager_Validation verifies that stale connections are checked before use.
//
// Scenarios:
//   - a connection idle for less than ValidateAfter is handed out without a ping
//   - an older one is pinged, and reused when it answers
//   - an older one that does not answer is closed and replaced
//   - when the ping needs the context and it ends, the acquisition stops
func TestManager_Validation(t *testing.T) {
	f := newFixture(t, connections.Config{ValidateAfter: 10 * time.Second})
	f.acquire(t).Release(nil)
	f.acquire(t).Release(nil)
	if f.conn.connection(1).pinged.Load() != 0 {
		t.Error("a fresh connection was pinged")
	}

	f.clock.Advance(11 * time.Second)
	f.acquire(t).Release(nil)
	if f.conn.connection(1).pinged.Load() != 1 || f.conn.count() != 1 {
		t.Errorf("pings = %d, opened = %d; want the stale connection pinged and reused", f.conn.connection(1).pinged.Load(), f.conn.count())
	}

	f.clock.Advance(11 * time.Second)
	f.conn.connection(1).breakConn()
	f.acquire(t).Release(nil)
	if !f.conn.connection(1).closed.Load() || f.conn.count() != 2 {
		t.Errorf("a dead connection was reused (opened %d)", f.conn.count())
	}

	f.clock.Advance(11 * time.Second)
	f.conn.connection(2).pingWait = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	if _, err := f.m.Acquire(ctx, f.key, "s"); !errors.Is(err, context.Canceled) {
		t.Errorf("Acquire() while validating = %v; want context.Canceled", err)
	}
}

// TestManager_Sweep verifies keepalive and cleanup.
//
// Scenarios:
//   - connections idle past the idle timeout are closed
//   - connections idle past the keepalive interval are pinged, kept when they answer and closed when they do not
//   - a connection just pinged is not pinged again until the interval passes again
//   - pools without connections are forgotten
//   - a pool closed while a connection was being checked closes it instead of pooling it
func TestManager_Sweep(t *testing.T) {
	f := newFixture(t, connections.Config{IdleTimeout: 5 * time.Minute, KeepAlive: 30 * time.Second, MaxPerSite: 4})
	leases := []*connections.Lease{f.acquire(t), f.acquire(t), f.acquire(t)}
	for _, l := range leases {
		l.Release(nil)
	}
	if closed := f.m.Sweep(t.Context()); closed != 0 {
		t.Errorf("Sweep() right away closed %d", closed)
	}

	f.clock.Advance(31 * time.Second)
	f.conn.connection(2).breakConn()
	if closed := f.m.Sweep(t.Context()); closed != 1 {
		t.Errorf("Sweep() after the keepalive interval closed %d; want the dead one", closed)
	}
	if !f.conn.connection(2).closed.Load() || f.conn.connection(1).closed.Load() || f.conn.connection(3).closed.Load() {
		t.Error("Sweep() closed the wrong connections")
	}
	if idle, total := f.m.Stats(f.key); idle != 2 || total != 2 {
		t.Errorf("Stats() = %d, %d; want two healthy connections", idle, total)
	}
	before := f.conn.connection(1).pinged.Load()
	f.clock.Advance(10 * time.Second)
	f.m.Sweep(t.Context())
	if f.conn.connection(1).pinged.Load() != before {
		t.Error("a connection was pinged again before the interval passed")
	}

	f.clock.Advance(5 * time.Minute)
	if closed := f.m.Sweep(t.Context()); closed != 2 {
		t.Errorf("Sweep() past the idle timeout closed %d; want 2", closed)
	}
	if idle, total := f.m.Stats(f.key); idle != 0 || total != 0 {
		t.Errorf("Stats() after expiry = %d, %d", idle, total)
	}
	f.acquire(t).Release(nil)

	race := newFixture(t, connections.Config{KeepAlive: time.Second})
	race.acquire(t).Release(nil)
	race.conn.connection(1).pingWait = make(chan struct{})
	race.clock.Advance(2 * time.Second)
	done := make(chan int)
	go func() { done <- race.m.Sweep(t.Context()) }()
	time.Sleep(30 * time.Millisecond)
	race.m.CloseUser(race.key.UserID)
	close(race.conn.connection(1).pingWait)
	<-done
	if !race.conn.connection(1).closed.Load() {
		t.Error("a connection checked while its pool was closed stayed open")
	}
}

// TestManager_CloseUser verifies closing the connections of a user.
//
// Scenarios:
//   - idle connections of the user are closed, those of other users are not
//   - a connection in use is closed when released
//   - callers waiting for a connection get ErrClosed
//   - the user can connect again afterwards
//   - Close closes everything and makes Acquire fail with ErrClosed
func TestManager_CloseUser(t *testing.T) {
	f := newFixture(t, connections.Config{MaxPerSite: 1})
	otherKey := connections.Key{UserID: uuid.New(), WorkspaceID: f.key.WorkspaceID, SiteID: f.key.SiteID}
	mine := f.acquire(t)
	theirs, err := f.m.Acquire(t.Context(), otherKey, "s")
	if err != nil {
		t.Fatal(err)
	}
	theirs.Release(nil)

	waiter := make(chan error, 1)
	go func() {
		_, err := f.m.Acquire(t.Context(), f.key, "s")
		waiter <- err
	}()
	time.Sleep(30 * time.Millisecond)
	f.m.CloseUser(f.key.UserID)
	if err := <-waiter; !errors.Is(err, connections.ErrClosed) {
		t.Errorf("a waiter = %v; want ErrClosed", err)
	}
	mine.Release(nil)
	if !f.conn.connection(1).closed.Load() {
		t.Error("a connection in use was not closed on release")
	}
	if f.conn.connection(2).closed.Load() {
		t.Error("another user's connection was closed")
	}
	if idle, _ := f.m.Stats(otherKey); idle != 1 {
		t.Error("another user's pool was affected")
	}
	again := f.acquire(t)
	again.Release(nil)

	f.m.Close()
	if !f.conn.connection(2).closed.Load() {
		t.Error("Close() left a connection open")
	}
	if _, err := f.m.Acquire(t.Context(), f.key, "s"); !errors.Is(err, connections.ErrClosed) {
		t.Errorf("Acquire() after Close = %v; want ErrClosed", err)
	}
}

// TestManager_With verifies the helper that runs work on a pooled connection.
//
// Scenarios:
//   - a successful call releases the connection to the pool
//   - a lost connection makes the work run once more on a fresh connection
//   - a second loss, and errors that are not a lost connection, are returned without more retries
//   - acquisition errors are returned
func TestManager_With(t *testing.T) {
	f := newFixture(t, connections.Config{})
	if err := f.m.With(t.Context(), f.key, "s", func(remotefs.RemoteFS) error { return nil }); err != nil {
		t.Fatalf("With() = %v", err)
	}
	if idle, _ := f.m.Stats(f.key); idle != 1 {
		t.Error("With() did not release the connection")
	}

	calls := 0
	err := f.m.With(t.Context(), f.key, "s", func(remotefs.RemoteFS) error {
		calls++
		if calls == 1 {
			return errLost
		}
		return nil
	})
	if err != nil || calls != 2 || f.conn.count() != 2 {
		t.Errorf("With() = %v after %d calls and %d connections; want one retry on a new connection", err, calls, f.conn.count())
	}

	calls = 0
	if err := f.m.With(t.Context(), f.key, "s", func(remotefs.RemoteFS) error { calls++; return errLost }); !errors.Is(err, remotefs.ErrConnectionLost) || calls != 2 {
		t.Errorf("With() failing twice = %v after %d calls; want the error after 2", err, calls)
	}
	calls = 0
	notFound := remotefs.ErrNotFound
	if err := f.m.With(t.Context(), f.key, "s", func(remotefs.RemoteFS) error { calls++; return notFound }); !errors.Is(err, notFound) || calls != 1 {
		t.Errorf("With() failing with a normal error = %v after %d calls; want no retry", err, calls)
	}

	f.m.Close()
	if err := f.m.With(t.Context(), f.key, "s", func(remotefs.RemoteFS) error { return nil }); !errors.Is(err, connections.ErrClosed) {
		t.Errorf("With() on a closed manager = %v", err)
	}
}

// TestManager_Run verifies the background loop.
//
// Scenarios:
//   - the loop sweeps on every tick, closing dead connections
//   - when the context ends the loop closes every connection
func TestManager_Run(t *testing.T) {
	f := newFixture(t, connections.Config{KeepAlive: time.Second})
	f.acquire(t).Release(nil)
	f.conn.connection(1).breakConn()
	f.clock.Advance(2 * time.Second)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		f.m.Run(ctx, 10*time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !f.conn.connection(1).closed.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !f.conn.connection(1).closed.Load() {
		t.Fatal("the loop did not close the dead connection")
	}
	lease := f.acquire(t)
	lease.Release(nil)
	cancel()
	<-done
	if !f.conn.connection(2).closed.Load() {
		t.Error("the loop left a connection open after the context ended")
	}
}

// TestManager_Concurrency hammers the manager from many goroutines, to be run with the race detector.
//
// Scenarios:
//   - many goroutines borrowing, failing, releasing, sweeping and closing users never exceed the limit per site
//     and leave no connection open or counted after Close
func TestManager_Concurrency(t *testing.T) {
	f := newFixture(t, connections.Config{MaxPerSite: 3, KeepAlive: time.Nanosecond})
	var live, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				lease, err := f.m.Acquire(t.Context(), f.key, "s")
				if err != nil {
					continue
				}
				n := live.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				var opErr error
				if j%7 == 0 {
					opErr = errLost
				}
				live.Add(-1)
				lease.Release(opErr)
				if j%10 == 0 {
					f.m.Sweep(t.Context())
				}
			}
		}()
	}
	wg.Wait()
	if peak.Load() > 3 {
		t.Errorf("%d connections were borrowed at once; the limit is 3", peak.Load())
	}
	f.m.Close()
	f.conn.mu.Lock()
	defer f.conn.mu.Unlock()
	for _, c := range f.conn.opened {
		if !c.closed.Load() {
			t.Errorf("connection %d was left open", c.id)
		}
	}
}

// TestConfig_Defaults verifies the defaults through the behavior they configure.
//
// Scenarios:
//   - four connections may be open at once by default, and a fifth waits
//   - the default sleep really waits between attempts, and stops with the context
func TestConfig_Defaults(t *testing.T) {
	f := connections.New(&connector{}, testutil.NewFakeClock(time.Now()), connections.Config{})
	t.Cleanup(f.Close)
	key := connections.Key{UserID: uuid.New()}
	for i := 0; i < 4; i++ {
		if _, err := f.Acquire(t.Context(), key, "s"); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := f.Acquire(ctx, key, "s"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the fifth connection = %v; want to wait", err)
	}

	failing := &connector{failures: []error{errLost, errLost, errLost, errLost, errLost}}
	retry := connections.New(failing, testutil.NewFakeClock(time.Now()), connections.Config{})
	t.Cleanup(retry.Close)
	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := retry.Acquire(ctx, key, "s"); !errors.Is(err, context.DeadlineExceeded) || time.Since(started) < 40*time.Millisecond {
		t.Errorf("Acquire() with the default backoff = %v after %v; want to wait for the deadline", err, time.Since(started))
	}
}

// TestConfig_DefaultSleepWaits verifies that the default backoff waits its full time before trying again.
//
// Scenarios:
//   - after one errLost connection the next attempt happens after about the initial backoff, and succeeds
func TestConfig_DefaultSleepWaits(t *testing.T) {
	c := &connector{failures: []error{errLost}}
	m := connections.New(c, testutil.NewFakeClock(time.Now()), connections.Config{})
	t.Cleanup(m.Close)
	started := time.Now()
	lease, err := m.Acquire(t.Context(), connections.Key{UserID: uuid.New()}, "s")
	if err != nil || time.Since(started) < 150*time.Millisecond {
		t.Fatalf("Acquire() = %v after %v; want a success after the backoff", err, time.Since(started))
	}
	lease.Release(nil)
}

// TestManager_CloseKey verifies closing the connections of one site.
//
// Scenarios:
//   - idle connections of the key are closed and the pool forgotten, other sites of the same user are not touched
//   - a connection in use is closed when released
//   - closing a key without a pool is harmless
func TestManager_CloseKey(t *testing.T) {
	f := newFixture(t, connections.Config{})
	other := connections.Key{UserID: f.key.UserID, WorkspaceID: f.key.WorkspaceID, SiteID: uuid.New()}
	busy := f.acquire(t)
	idle, err := f.m.Acquire(t.Context(), other, "s")
	if err != nil {
		t.Fatal(err)
	}
	idle.Release(nil)

	f.m.CloseKey(f.key)
	f.m.CloseKey(connections.Key{})
	busy.Release(nil)
	if !f.conn.connection(1).closed.Load() {
		t.Error("a connection in use was not closed on release")
	}
	if f.conn.connection(2).closed.Load() {
		t.Error("another site's connection was closed")
	}
	if idleCount, total := f.m.Stats(f.key); idleCount != 0 || total != 0 {
		t.Errorf("Stats() after closing the key = %d, %d", idleCount, total)
	}
	if idleCount, _ := f.m.Stats(other); idleCount != 1 {
		t.Error("another site's pool was affected")
	}
}

// TestManager_CloseSite covers [connections.Manager.CloseSite].
//
// Use cases:
//   - the connections of a site close for every user, idle ones at once and ones in use when released
//   - the connections of other sites stay open
func TestManager_CloseSite(t *testing.T) {
	f := newFixture(t, connections.Config{})
	otherUser := connections.Key{UserID: uuid.New(), WorkspaceID: f.key.WorkspaceID, SiteID: f.key.SiteID}
	otherSite := connections.Key{UserID: f.key.UserID, WorkspaceID: f.key.WorkspaceID, SiteID: uuid.New()}
	busy := f.acquire(t)
	for _, key := range []connections.Key{otherUser, otherSite} {
		lease, err := f.m.Acquire(t.Context(), key, "s")
		if err != nil {
			t.Fatal(err)
		}
		lease.Release(nil)
	}

	f.m.CloseSite(f.key.SiteID)
	if !f.conn.connection(2).closed.Load() {
		t.Error("the idle connection of another user to the site stayed open")
	}
	busy.Release(nil)
	if !f.conn.connection(1).closed.Load() {
		t.Error("a connection in use was not closed on release")
	}
	if f.conn.connection(3).closed.Load() {
		t.Error("the connection of another site was closed")
	}
	if _, total := f.m.Stats(otherSite); total != 1 {
		t.Error("the pool of another site was affected")
	}
}

// admitting is a connector that is also a [connections.Admitter], refusing the sessions it is told to.
type admitting struct {
	*connector
	refused string
}

// errRefused is what [admitting] refuses a session with.
var errRefused = errors.New("session refused")

// Admit implements [connections.Admitter].
func (a *admitting) Admit(_ context.Context, _ connections.Key, session string) error {
	if session == a.refused {
		return errRefused
	}
	return nil
}

// TestManager_Admitter covers how [connections.Manager.Acquire] consults a connector that is a [connections.Admitter].
//
// Use cases:
//   - an admitted session gets a connection, and releases it to the pool
//   - a refused session gets the connector's error, even though an idle connection is waiting, and nothing is opened
func TestManager_Admitter(t *testing.T) {
	conn := &admitting{connector: &connector{}, refused: "other"}
	m := connections.New(conn, testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), connections.Config{})
	t.Cleanup(m.Close)
	key := connections.Key{UserID: uuid.New(), WorkspaceID: uuid.New(), SiteID: uuid.New()}

	lease, err := m.Acquire(t.Context(), key, "typed")
	if err != nil {
		t.Fatalf("Acquire(admitted) = %v", err)
	}
	lease.Release(nil)
	if _, err := m.Acquire(t.Context(), key, "other"); !errors.Is(err, errRefused) {
		t.Fatalf("Acquire(refused) = %v; want the connector's error", err)
	}
	if conn.count() != 1 {
		t.Errorf("connections opened = %d; want 1", conn.count())
	}
}

// TestManager_OnChange covers telling which sites have connections open (4.11).
//
// Use cases:
//   - the first connection of a key is told as open, reusing or pooling it again tells nothing, and losing the last
//     connection is told as closed
//   - closing a key is told as closed at once, even while a connection is still borrowed, and releasing that one later
//     tells nothing more
//   - Open lists the sites of a workspace with connections open, for their user only
func TestManager_OnChange(t *testing.T) {
	var mu sync.Mutex
	var told []connections.State
	f := newFixture(t, connections.Config{OnChange: func(state connections.State) {
		mu.Lock()
		defer mu.Unlock()
		told = append(told, state)
	}})
	states := func() []bool {
		mu.Lock()
		defer mu.Unlock()
		out := make([]bool, 0, len(told))
		for _, s := range told {
			if s.Key != f.key {
				t.Errorf("told about %v; want %v", s.Key, f.key)
			}
			out = append(out, s.Open)
		}
		return out
	}

	first := f.acquire(t)
	first.Release(nil)
	f.acquire(t).Release(nil)
	if got := states(); !slices.Equal(got, []bool{true}) {
		t.Fatalf("after opening and reusing = %v; want [true]", got)
	}
	if open := f.m.Open(f.key.UserID, f.key.WorkspaceID); len(open) != 1 || open[0] != f.key.SiteID {
		t.Errorf("Open() = %v; want the site", open)
	}
	if open := f.m.Open(uuid.New(), f.key.WorkspaceID); len(open) != 0 {
		t.Errorf("Open() of another user = %v; want none", open)
	}

	f.acquire(t).Release(fmt.Errorf("op: %w", remotefs.ErrConnectionLost))
	if got := states(); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("after losing the connection = %v; want [true false]", got)
	}

	borrowed := f.acquire(t)
	f.m.CloseKey(f.key)
	if got := states(); !slices.Equal(got, []bool{true, false, true, false}) {
		t.Fatalf("after closing with a borrowed connection = %v", got)
	}
	if open := f.m.Open(f.key.UserID, f.key.WorkspaceID); len(open) != 0 {
		t.Errorf("Open() after closing = %v; want none", open)
	}
	borrowed.Release(nil)
	if got := states(); len(got) != 4 {
		t.Errorf("releasing a connection of a closed pool told %v", got)
	}
}

// sink keeps the events published to it.
type sink struct{ events []ports.Event }

// Publish implements [ports.EventSink].
func (s *sink) Publish(e ports.Event) { s.events = append(s.events, e) }

// TestPublishTo verifies that changes of the open connections are told to the user of the key as connection.state
// events carrying the change.
func TestPublishTo(t *testing.T) {
	events := &sink{}
	state := connections.State{Key: connections.Key{UserID: uuid.New(), WorkspaceID: uuid.New(), SiteID: uuid.New()}, Open: true}
	connections.PublishTo(events)(state)
	if len(events.events) != 1 || events.events[0].Type != connections.EventState || events.events[0].UserID != state.Key.UserID || events.events[0].Data != state {
		t.Errorf("events = %+v", events.events)
	}
}
