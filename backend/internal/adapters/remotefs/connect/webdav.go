package connect

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/fprambs/transferly/internal/adapters/remotefs/davfs"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
)

// openWebDAV connects to the root of a site on a WebDAV server (ADR-0026). HTTPS is trusted when the authorities of the system,
// or those the connector was given, accept the certificate; otherwise its fingerprint is offered to the user as an FTPS
// certificate is. The certificate is checked on every connection the client opens, so the check outlives the context of the
// dial. Plain HTTP is marked in the log.
func openWebDAV(ctx context.Context, c *Connector, key connections.Key, t target) (remotefs.RemoteFS, error) {
	host, port := t.Setting(connectors.FieldHost), t.number(connectors.FieldPort)
	auth, secure := t.Setting(connectors.FieldAuth), t.Setting("tls") != "false"
	leg := hop{c: c, key: key}
	leg.connecting(t, host, port, viaOf(t))
	scheme := "https"
	if !secure {
		scheme = "http"
		leg.record(connlog.Warn, connlog.PlainText, map[string]string{})
	}
	cfg := davfs.Config{
		Endpoint: (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: t.Setting("path")}).String(),
		Client: &http.Client{Transport: &http.Transport{
			DialContext:           leg.dialer(c.baseDialer(t)),
			TLSClientConfig:       davfs.TLSConfig(c.roots, leg.verify(context.WithoutCancel(ctx), host, port)),
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   c.timeout,
			ResponseHeaderTimeout: c.timeout,
		}},
	}
	if auth != connectors.AuthAnonymous {
		cfg.Username, cfg.Password = t.Setting(connectors.FieldUsername), string(t.secret(site.SecretPassword))
	}
	fsys, err := davfs.Dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	leg.authenticated(auth, t.Setting(connectors.FieldUsername))
	return fsys, nil
}
