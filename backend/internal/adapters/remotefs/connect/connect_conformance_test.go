package connect_test

import (
	"net"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/fprambs/transferly/internal/adapters/remotefs/connectortest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/ftpfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/ftpfs/ftptest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/hostkey"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
)

// endpoint is a reference server of a connector: where it listens, the key it presents and the settings and secrets of a site
// that signs in to it.
type endpoint struct {
	address, keyType, fingerprint string
	config                        map[string]string
	secrets                       site.SecretChanges
}

// references start, for each connector Transferly is built with, a local server that supports everything the protocol can do.
var references = map[string]func(t *testing.T) endpoint{
	"sftp": func(t *testing.T) endpoint {
		s := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
		return endpoint{
			address: s.Addr, keyType: s.HostKey.Type(), fingerprint: ssh.FingerprintSHA256(s.HostKey),
			config:  map[string]string{"username": sshUser},
			secrets: site.SecretChanges{site.SecretPassword: sshPassword},
		}
	},
	"ftp": func(t *testing.T) endpoint {
		s := ftptest.Start(t, ftptest.Options{Encryption: ftptest.Explicit})
		return endpoint{
			address: s.Addr, keyType: ftpfs.CertificateKeyType, fingerprint: s.Fingerprint,
			config:  map[string]string{"username": ftpUser, "encryption": connectors.EncryptionExplicit},
			secrets: site.SecretChanges{site.SecretPassword: ftpPassword},
		}
	},
}

// open saves a site of a connector that reaches a new reference server, trusts the server's key and connects to it the way
// the explorer does.
func (e *env) open(t *testing.T, d connectors.Descriptor, reference func(t *testing.T) endpoint) remotefs.RemoteFS {
	t.Helper()
	server := reference(t)
	host, port, err := net.SplitHostPort(server.address)
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]string{connectors.FieldHost: host, connectors.FieldPort: port}
	for name, value := range server.config {
		config[name] = value
	}
	in := site.Input{Name: d.Type + " " + uuid.NewString(), Connector: d.Type, Config: config}
	created, err := e.sites.Create(t.Context(), e.user, e.workspace, session, in, server.secrets)
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.hostKeys.Trust(t.Context(), e.user, e.workspace, host, portNumber, server.keyType, server.fingerprint, hostkey.TrustNew); err != nil {
		t.Fatalf("Trust() = %v", err)
	}
	fsys, err := e.connector(e.allow).Connect(t.Context(), e.key(created), session)
	if err != nil {
		t.Fatalf("Connect() = %v", err)
	}
	return fsys
}

// TestConformance_Catalog runs the conformance suite of connectors (ADR-0019) against every connector Transferly is built
// with, reached through its schema, a saved site, its opener and its adapter, on a local reference server.
//
// Use cases:
//   - each built-in connector has a reference server; a connector added to the catalog without one fails here
//   - its descriptor is valid, declares the capabilities its adapter has and no other, and the adapter passes the shared suite
func TestConformance_Catalog(t *testing.T) {
	e := newEnv(t)
	for _, d := range connectors.Builtin().All() {
		t.Run(d.Type, func(t *testing.T) {
			reference, ok := references[d.Type]
			if !ok {
				t.Fatalf("connector %q has no reference server for the conformance suite", d.Type)
			}
			connectortest.Run(t, d, func(t *testing.T) remotefs.RemoteFS { return e.open(t, d, reference) })
		})
	}
}
