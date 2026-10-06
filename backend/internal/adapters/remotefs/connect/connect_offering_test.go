package connect_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/connectors"
)

// offering offers every connector but the ones it refuses.
type offering map[string]bool

// CheckConnector implements [site.Offering].
func (o offering) CheckConnector(_ context.Context, kind string) error {
	if o[kind] {
		return connectors.ErrDisabled
	}
	return nil
}

// TestConnector_Offering covers connectors the instance does not offer in [connect.Connector.Connect] (ADR-0019).
//
// Use cases:
//   - a site of a disabled connector fails with ErrDisabled and its server is never dialed
//   - a site of a connector that is offered connects
func TestConnector_Offering(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	e.trust(server)
	target := e.addSite(server, spec{Name: "Offered"}, changes{Password: text(sshPassword)})

	refused := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.Offering(offering{"sftp": true}))
	if _, err := refused.Connect(t.Context(), e.key(target), session); !errors.Is(err, connectors.ErrDisabled) {
		t.Fatalf("Connect() of a disabled connector = %v; want ErrDisabled", err)
	}
	offered := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.Offering(offering{"ftp": true}))
	fsys, err := offered.Connect(t.Context(), e.key(target), session)
	if err != nil {
		t.Fatalf("Connect() of an offered connector = %v", err)
	}
	_ = fsys.Close()
}
