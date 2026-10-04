package connect_test

import (
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/fprambs/transferly/internal/adapters/googleauth"
	"github.com/fprambs/transferly/internal/adapters/remotefs/azuretest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/adapters/remotefs/connectortest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/davfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/davfs/davtest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/ftpfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/ftpfs/ftptest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/gdrivefs/googletest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/s3fs/s3test"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/hostkey"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
)

// endpoint is a reference server of a connector: where it listens, the key it presents for the user to trust, if any, the
// authorities its certificate is verified against, if they are not the system's, and the settings and secrets of a site that
// signs in to it.
type endpoint struct {
	address, keyType, fingerprint string
	roots                         *x509.CertPool
	google                        *googleauth.Service
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
	"s3":  bucket,
	"gcs": bucket,
	"azblob": func(t *testing.T) endpoint {
		s := azuretest.StartBlob(t, azuretest.Options{})
		return endpoint{
			address: strings.TrimPrefix(s.URL, "http://"),
			config:  map[string]string{"account": azuretest.Account, "container": azuretest.Container, "tls": "false"},
			secrets: site.SecretChanges{site.SecretAccountKey: azuretest.Key},
		}
	},
	"azfiles": func(t *testing.T) endpoint {
		s := azuretest.StartFiles(t, azuretest.Options{})
		return endpoint{
			address: strings.TrimPrefix(s.URL, "http://"),
			config:  map[string]string{"account": azuretest.Account, "share": azuretest.Share, "tls": "false"},
			secrets: site.SecretChanges{site.SecretAccountKey: azuretest.Key},
		}
	},
	"webdav": func(t *testing.T) endpoint {
		server := httptest.NewUnstartedServer(davtest.New(davtest.Options{}).Handler())
		server.StartTLS()
		t.Cleanup(server.Close)
		return endpoint{
			address: strings.TrimPrefix(server.URL, "https://"), keyType: davfs.CertificateKeyType, fingerprint: davfs.Fingerprint(server.Certificate().Raw),
			config:  map[string]string{"path": davtest.Root, "username": davtest.Username},
			secrets: site.SecretChanges{site.SecretPassword: davtest.Password},
		}
	},
	"gdrive": func(t *testing.T) endpoint {
		s := googletest.Start(t, googletest.Options{})
		google := googleauth.New(googleauth.Config{
			ClientID: googletest.ClientID, ClientSecret: googletest.ClientSecret, AuthURL: s.URL + "/o/oauth2/auth", TokenURL: s.URL + "/token", APIURL: s.URL,
		}, http.DefaultClient, time.Now)
		return endpoint{
			address: strings.TrimPrefix(s.URL, "http://"), google: google,
			config:  map[string]string{"account_email": googletest.Email},
			secrets: site.SecretChanges{site.SecretRefreshToken: s.RefreshToken()},
		}
	},
}

// bucket starts the reference server of the connectors of object stores that speak S3: an S3 server in memory with one bucket.
func bucket(t *testing.T) endpoint {
	s := s3test.Start(t, s3test.Options{})
	return endpoint{
		address: s.Addr, roots: s.RootCAs,
		config:  map[string]string{"bucket": s3test.Bucket, "access_key_id": "transferly"},
		secrets: site.SecretChanges{site.SecretAccessKey: "a secret access key"},
	}
}

// open saves a site of a connector that reaches a new reference server, trusts the server's key and connects to it the way
// the explorer does. A new server may listen on the port of one the suite already closed, so its key replaces the one trusted there.
func (e *env) open(t *testing.T, d connectors.Descriptor, reference func(t *testing.T) endpoint) remotefs.RemoteFS {
	t.Helper()
	server := reference(t)
	host, port, err := net.SplitHostPort(server.address)
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]string{}
	if _, ok := d.Field(connectors.FieldHost); ok {
		config[connectors.FieldHost], config[connectors.FieldPort] = host, port
	}
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
	if server.keyType != "" {
		if _, err := e.hostKeys.Trust(t.Context(), e.user, e.workspace, host, portNumber, server.keyType, server.fingerprint, hostkey.TrustReplace); err != nil {
			t.Fatalf("Trust() = %v", err)
		}
	}
	c := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.CertificateAuthorities(server.roots), connect.Google(server.google))
	fsys, err := c.Connect(t.Context(), e.key(created), session)
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
