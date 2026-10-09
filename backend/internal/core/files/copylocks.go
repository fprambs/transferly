package files

import (
	"context"
	"sort"
	"sync"
)

// copyLock reserves a site's copy operations while retaining only keys with active users.
type copyLock struct {
	token chan struct{}
	users int
}

// copyLocks coordinates both ends of copies in a stable order, preventing opposite copies from waiting on each other.
type copyLocks struct {
	mu   sync.Mutex
	keys map[string]*copyLock
}

// acquire reserves one site or returns cancellation, releasing unused map entries.
func (c *copyLocks) acquire(ctx context.Context, key string) (func(), error) {
	c.mu.Lock()
	if c.keys == nil {
		c.keys = make(map[string]*copyLock)
	}
	l := c.keys[key]
	if l == nil {
		l = &copyLock{token: make(chan struct{}, 1)}
		c.keys[key] = l
	}
	l.users++
	c.mu.Unlock()
	forget := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		l.users--
		if l.users == 0 {
			delete(c.keys, key)
		}
	}
	select {
	case l.token <- struct{}{}:
		return func() { <-l.token; forget() }, nil
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	}
}

// pair holds the sites of a copy until it ends; acquisition is ordered and cancellable.
func (c *copyLocks) pair(ctx context.Context, l CopyLocation) (func(), error) {
	a, b := l.Source.key(), l.Destination.key()
	keys := []string{a.UserID.String() + a.WorkspaceID.String() + a.SiteID.String()}
	if a != b {
		keys = append(keys, b.UserID.String()+b.WorkspaceID.String()+b.SiteID.String())
	}
	sort.Strings(keys)
	var releases []func()
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	for _, key := range keys {
		done, err := c.acquire(ctx, key)
		if err != nil {
			release()
			return nil, err
		}
		releases = append(releases, done)
	}
	return release, nil
}
