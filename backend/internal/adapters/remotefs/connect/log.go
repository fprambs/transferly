package connect

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"

	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/connlog"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/site"
)

// ConnectionLog sets where the steps of each connection are told to the user (ADR-0011). Without it, nothing is told.
func ConnectionLog(r *connlog.Recorder) Option {
	return func(c *Connector) { c.log = r }
}

// dialFunc opens a network connection, as [net.Dialer.DialContext] does.
type dialFunc = func(ctx context.Context, network, address string) (net.Conn, error)

// hop is one leg of a connection, the site itself or its jump host, and records the steps of that leg in the log of
// the site. Entries about the jump host are marked with the parameter hop "jump".
type hop struct {
	c    *Connector
	key  connections.Key
	jump bool
}

// record publishes one entry about the site of the hop.
func (h hop) record(level connlog.Level, step connlog.Step, params map[string]string) {
	if h.jump {
		params["hop"] = "jump"
	}
	h.c.log.Record(connlog.Target{UserID: h.key.UserID, WorkspaceID: h.key.WorkspaceID, SiteID: h.key.SiteID}, level, step, params)
}

// connecting records the start of a connection to host:port, and how it is reached: directly, through the site's
// proxy (named), or through its jump host.
func (h hop) connecting(target site.Site, host string, port int, via string) {
	params := map[string]string{"host": host, "port": strconv.Itoa(port), "via": via}
	if via == "proxy" {
		params["proxy"] = net.JoinHostPort(target.Proxy.Host, strconv.Itoa(target.Proxy.Port))
	}
	h.record(connlog.Info, connlog.Connecting, params)
}

// viaOf is how a site, or its jump host, is reached from Sentinel: through its proxy or directly.
func viaOf(target site.Site) string {
	if target.Proxy.Set() {
		return "proxy"
	}
	return "direct"
}

// dialer wraps dial so that the first connection it opens is recorded with the address it reached. FTP opens a data
// connection per transfer through the same dialer, and those are not recorded.
func (h hop) dialer(dial dialFunc) dialFunc {
	var once sync.Once
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err == nil {
			once.Do(func() {
				h.record(connlog.Info, connlog.Connected, map[string]string{"address": conn.RemoteAddr().String()})
			})
		}
		return conn, err
	}
}

// verify returns the check of the server key of host:port, which records its verdict: trusted, not trusted yet, or
// changed.
func (h hop) verify(ctx context.Context, host string, port int) func(keyType, fingerprint string) error {
	return func(keyType, fingerprint string) error {
		err := h.c.verifyHost(ctx, h.key.WorkspaceID, host, port, keyType, fingerprint)
		params := map[string]string{"keyType": keyType, "fingerprint": fingerprint}
		var mismatch *remotefs.HostKeyError
		switch {
		case err == nil:
			h.record(connlog.Info, connlog.HostKeyVerified, params)
		case errors.As(err, &mismatch) && mismatch.Changed:
			h.record(connlog.Error, connlog.HostKeyChanged, params)
		case errors.As(err, &mismatch):
			h.record(connlog.Warn, connlog.HostKeyUnknown, params)
		}
		return err
	}
}

// authenticated records that the account signed in, with the logon type it used; never its secret.
func (h hop) authenticated(method site.AuthMethod, username string) {
	h.record(connlog.Info, connlog.Authenticated, map[string]string{"method": string(method), "username": username})
}
