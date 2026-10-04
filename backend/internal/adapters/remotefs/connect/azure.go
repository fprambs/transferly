package connect

import (
	"context"
	"crypto/tls"
	"net/http"

	"github.com/fprambs/transferly/internal/adapters/remotefs/azblobfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/azfilefs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/azure"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
)

// azureService returns how to reach a service of the storage account of a site (ADR-0025), through the network guard and the
// site's proxy, and records that the connection starts and how it is reached; plain HTTP is marked in the log.
func (c *Connector) azureService(key connections.Key, t target, service string) (azure.Config, hop) {
	account, host := t.Setting("account"), t.Setting(connectors.FieldHost)
	secure := t.Setting("tls") != "false"
	port := t.number(connectors.FieldPort)
	reached, reachedPort := azureHost(service, account, host, port)
	leg := hop{c: c, key: key}
	leg.connecting(t, reached, reachedPort, viaOf(t))
	if !secure {
		leg.record(connlog.Warn, connlog.PlainText, map[string]string{})
	}
	cfg := azure.Config{
		Endpoint: azure.Endpoint(service, account, host, port, secure),
		Account:  account,
		Client: &http.Client{Transport: &http.Transport{
			DialContext:           leg.dialer(c.baseDialer(t)),
			TLSClientConfig:       &tls.Config{RootCAs: c.roots, MinVersion: tls.VersionTLS12},
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   c.timeout,
			ResponseHeaderTimeout: c.timeout,
		}},
	}
	switch t.Setting(connectors.FieldAuth) {
	case connectors.AuthAccountKey:
		cfg.Key = string(t.secret(site.SecretAccountKey))
	case connectors.AuthSAS:
		cfg.SAS = string(t.secret(site.SecretSASToken))
	}
	return cfg, leg
}

// azureHost returns the host and port a service of an account is reached at: those of a site, or the endpoint of Azure when the
// site names none.
func azureHost(service, account, host string, port int) (string, int) {
	if host == "" {
		return account + "." + service + ".core.windows.net", 443
	}
	return host, port
}

// openAzureBlob connects to a container of Azure Blob Storage (ADR-0025).
func openAzureBlob(ctx context.Context, c *Connector, key connections.Key, t target) (remotefs.RemoteFS, error) {
	cfg, leg := c.azureService(key, t, "blob")
	fsys, err := azblobfs.Dial(ctx, azblobfs.Config{Service: cfg, Container: t.Setting("container")})
	if err != nil {
		return nil, err
	}
	leg.authenticated(t.Setting(connectors.FieldAuth), cfg.Account)
	return fsys, nil
}

// openAzureFiles connects to a share of Azure Files (ADR-0025).
func openAzureFiles(ctx context.Context, c *Connector, key connections.Key, t target) (remotefs.RemoteFS, error) {
	cfg, leg := c.azureService(key, t, "file")
	fsys, err := azfilefs.Dial(ctx, azfilefs.Config{Service: cfg, Share: t.Setting("share")})
	if err != nil {
		return nil, err
	}
	leg.authenticated(t.Setting(connectors.FieldAuth), cfg.Account)
	return fsys, nil
}
