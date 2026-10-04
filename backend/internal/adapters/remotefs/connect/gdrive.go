package connect

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/fprambs/transferly/internal/adapters/remotefs/gdrivefs"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
)

// openGDrive connects to Google Drive as the account of the site's refresh token (ADR-0024), through the network guard and the
// site's proxy like every connection. The certificate of Google is verified against the authorities of the system, or those the
// connector was given.
func openGDrive(ctx context.Context, c *Connector, key connections.Key, t target) (remotefs.RemoteFS, error) {
	if c.google == nil || !c.google.Config().Enabled() {
		return nil, fmt.Errorf("%w: signing in with Google is not configured on this instance", remotefs.ErrInvalid)
	}
	refresh := t.secret(site.SecretRefreshToken)
	if len(refresh) == 0 {
		return nil, fmt.Errorf("%w: the site has no Google account connected", remotefs.ErrAuth)
	}
	api := c.google.Config().APIURL
	endpoint, _ := url.Parse(api)
	port, _ := strconv.Atoi(endpoint.Port())
	if port == 0 {
		port = map[string]int{"http": 80, "https": 443}[endpoint.Scheme]
	}
	leg := hop{c: c, key: key}
	leg.connecting(t, endpoint.Hostname(), port, viaOf(t))
	base := &http.Client{Transport: &http.Transport{
		DialContext:           leg.dialer(c.baseDialer(t)),
		TLSClientConfig:       &tls.Config{RootCAs: c.roots, MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   c.timeout,
		ResponseHeaderTimeout: c.timeout,
	}}
	fsys, err := gdrivefs.Dial(ctx, gdrivefs.Config{APIURL: api, Client: c.google.Client(base, string(refresh)), SharedDrive: t.Setting("shared_drive")})
	if err != nil {
		return nil, err
	}
	leg.authenticated(connectors.ProviderGoogle, t.Setting("account_email"))
	return fsys, nil
}
