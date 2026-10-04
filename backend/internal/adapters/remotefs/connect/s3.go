package connect

import (
	"context"

	"github.com/fprambs/transferly/internal/adapters/remotefs/s3fs"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
)

// openS3 connects to a bucket of S3 or a compatible service (ADR-0023), Google Cloud Storage among them (ADR-0024). HTTPS is
// verified against the authorities of the system, or those the connector was given, so no server key is trusted by the user;
// plain HTTP, which only connectors with a tls setting allow, is marked in the log.
func openS3(ctx context.Context, c *Connector, key connections.Key, t target) (remotefs.RemoteFS, error) {
	host, port := t.Setting(connectors.FieldHost), t.number(connectors.FieldPort)
	auth, secure := t.Setting(connectors.FieldAuth), t.Setting("tls") != "false"
	leg := hop{c: c, key: key}
	leg.connecting(t, host, port, viaOf(t))
	cfg := s3fs.Config{
		Endpoint:    t.address(connectors.FieldHost, connectors.FieldPort),
		Secure:      secure,
		RootCAs:     c.roots,
		Region:      t.Setting("region"),
		Bucket:      t.Setting("bucket"),
		DialContext: leg.dialer(c.baseDialer(t)),
		Timeout:     c.timeout,
	}
	if auth == connectors.AuthAccessKey {
		cfg.AccessKeyID, cfg.SecretAccessKey = t.Setting("access_key_id"), string(t.secret(site.SecretAccessKey))
	}
	fsys, err := s3fs.Dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if !secure {
		leg.record(connlog.Warn, connlog.PlainText, map[string]string{})
	}
	leg.authenticated(auth, cfg.AccessKeyID)
	return fsys, nil
}
