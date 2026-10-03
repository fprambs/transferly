package connect_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/site"
)

// offering offers every protocol but the ones it refuses.
type offering map[site.Protocol]bool

// CheckProtocol implements [site.Offering].
func (o offering) CheckProtocol(_ context.Context, protocol site.Protocol) error {
	if o[protocol] {
		return site.ErrConnectorDisabled
	}
	return nil
}

// TestConnector_Offering covers connectors the instance does not offer in [connect.Connector.Connect] (ADR-0019).
//
// Use cases:
//   - a site of a disabled connector fails with ErrConnectorDisabled and its server is never dialed
//   - a site of a connector that is offered connects
func TestConnector_Offering(t *testing.T) {
	e := newEnv(t)
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	e.trust(server)
	target := e.addSite(server, site.Input{Name: "Offered"}, site.SecretChanges{Password: text(sshPassword)})

	refused := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.Offering(offering{site.SFTP: true}))
	if _, err := refused.Connect(t.Context(), e.key(target), session); !errors.Is(err, site.ErrConnectorDisabled) {
		t.Fatalf("Connect() of a disabled connector = %v; want ErrConnectorDisabled", err)
	}
	offered := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.Offering(offering{site.FTP: true}))
	fsys, err := offered.Connect(t.Context(), e.key(target), session)
	if err != nil {
		t.Fatalf("Connect() of an offered connector = %v", err)
	}
	_ = fsys.Close()
}
