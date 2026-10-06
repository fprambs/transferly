package connect_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/adapters/remotefs/s3fs/s3test"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/testutil"
)

// addS3Site saves an S3 site of the bucket of a test server with the given settings and secrets.
func (e *env) addS3Site(t *testing.T, s *s3test.Server, config map[string]string, secrets site.SecretChanges) site.Site {
	t.Helper()
	host, port, _ := strings.Cut(s.Addr, ":")
	all := map[string]string{"host": host, "port": port, "bucket": s3test.Bucket}
	for name, value := range config {
		all[name] = value
	}
	created, err := e.sites.Create(t.Context(), e.user, e.workspace, session, site.Input{Name: "S3 " + t.Name() + config["bucket"] + config["auth"], Connector: "s3", Config: all}, secrets)
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	return created
}

// TestConnector_S3 verifies connecting to S3 sites (ADR-0023).
//
// Use cases:
//   - a site with an access key over HTTPS connects, verified against the given authorities, and its log names the access key ID but
//     never the secret
//   - an anonymous site over plain HTTP connects and its log warns that the connection is in clear text
//   - a bucket that does not exist is not found, and nothing signs in
//   - the network guard applies to object stores too
func TestConnector_S3(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	sink := &collected{}
	recorder := connlog.New(sink, testutil.NewFakeClock(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)))

	secure := s3test.Start(t, s3test.Options{})
	keyed := e.addS3Site(t, secure, map[string]string{"access_key_id": "AKIATRANSFERLY"}, site.SecretChanges{site.SecretAccessKey: "the secret access key"})
	c := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.CertificateAuthorities(secure.RootCAs), connect.ConnectionLog(recorder))
	fsys, err := c.Connect(t.Context(), e.key(keyed), session)
	if err != nil {
		t.Fatalf("Connect() to an S3 site = %v", err)
	}
	_ = fsys.Close()
	entries := sink.take()
	if got := steps(entries); got != "connecting connected authenticated" {
		t.Errorf("steps of an S3 connection = %q", got)
	}
	if last := entries[len(entries)-1]; last.Params["method"] != "access_key" || last.Params["username"] != "AKIATRANSFERLY" {
		t.Errorf("authenticated = %+v", last)
	}
	for _, entry := range entries {
		for _, value := range entry.Params {
			if strings.Contains(value, "secret access key") {
				t.Errorf("an entry holds the secret access key: %+v", entry)
			}
		}
	}

	plain := s3test.Start(t, s3test.Options{Plain: true})
	public := e.addS3Site(t, plain, map[string]string{"tls": "false", "auth": "anonymous"}, nil)
	if fsys, err := c.Connect(t.Context(), e.key(public), session); err != nil {
		t.Errorf("Connect() to a public bucket over HTTP = %v", err)
	} else {
		_ = fsys.Close()
	}
	if got := steps(sink.take()); got != "connecting connected plain_text authenticated" {
		t.Errorf("steps of a plain connection = %q", got)
	}

	absent := e.addS3Site(t, secure, map[string]string{"bucket": "absent", "access_key_id": "AKIATRANSFERLY"}, site.SecretChanges{site.SecretAccessKey: "x"})
	if _, err := c.Connect(t.Context(), e.key(absent), session); !errors.Is(err, remotefs.ErrNotFound) {
		t.Errorf("Connect() to a missing bucket = %v, want ErrNotFound", err)
	}
	if got := steps(sink.take()); strings.Contains(got, "authenticated") {
		t.Errorf("steps to a missing bucket = %q, want no sign in", got)
	}

	blocked := connect.New(e.sites, e.quick, e.hostKeys, netguard.NewPolicy(nil, nil), 5*time.Second, connect.CertificateAuthorities(secure.RootCAs))
	if _, err := blocked.Connect(t.Context(), e.key(keyed), session); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("Connect() to a forbidden S3 destination = %v, want netguard.ErrBlocked", err)
	}
}
