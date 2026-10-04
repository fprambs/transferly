package connect_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/azuretest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/testutil"
)

// TestConnector_Azure verifies connecting to Azure Storage sites (ADR-0025).
//
// Use cases:
//   - a container is reached with a SAS token and, when it is public, anonymously; a share with the account key
//   - a container or a share that does not exist is not found
//   - the log tells the host it reached, that plain HTTP is clear text, and the account that signed in, never the key nor the token
func TestConnector_Azure(t *testing.T) {
	e := newEnv(t)
	sink := &collected{}
	recorder := connlog.New(sink, testutil.NewFakeClock(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)))
	c := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.ConnectionLog(recorder))
	add := func(connector string, s *azuretest.Server, config map[string]string, secrets site.SecretChanges) site.Site {
		host, port, _ := strings.Cut(strings.TrimPrefix(s.URL, "http://"), ":")
		all := map[string]string{"account": azuretest.Account, "host": host, "port": port, "tls": "false"}
		for k, v := range config {
			all[k] = v
		}
		created, err := e.sites.Create(t.Context(), e.user, e.workspace, session, site.Input{Name: connector + " " + config["auth"] + " " + config["container"] + config["share"], Connector: connector, Config: all}, secrets)
		if err != nil {
			t.Fatalf("Create() = %v", err)
		}
		return created
	}
	public := azuretest.StartBlob(t, azuretest.Options{Public: true})
	shares := azuretest.StartFiles(t, azuretest.Options{})
	for name, s := range map[string]site.Site{
		"a container with a SAS token":   add("azblob", public, map[string]string{"container": azuretest.Container, "auth": "sas"}, site.SecretChanges{site.SecretSASToken: azuretest.SAS}),
		"a public container anonymously": add("azblob", public, map[string]string{"container": azuretest.Container, "auth": "anonymous"}, nil),
		"a share with the account key":   add("azfiles", shares, map[string]string{"share": azuretest.Share, "auth": "account_key"}, site.SecretChanges{site.SecretAccountKey: azuretest.Key}),
	} {
		fsys, err := c.Connect(t.Context(), e.key(s), session)
		if err != nil {
			t.Errorf("Connect() to %s = %v", name, err)
			continue
		}
		_ = fsys.Close()
		entries := sink.take()
		if got := steps(entries); got != "connecting plain_text connected authenticated" {
			t.Errorf("steps to %s = %q", name, got)
		}
		if last := entries[len(entries)-1]; last.Params["username"] != azuretest.Account {
			t.Errorf("authenticated to %s = %+v", name, last)
		}
		for _, entry := range entries {
			for _, value := range entry.Params {
				if strings.Contains(value, azuretest.Key) || strings.Contains(value, "sig=") {
					t.Errorf("an entry holds a credential: %+v", entry)
				}
			}
		}
	}
	for name, s := range map[string]site.Site{
		"a missing container": add("azblob", public, map[string]string{"container": "absent", "auth": "anonymous"}, nil),
		"a missing share":     add("azfiles", shares, map[string]string{"share": "absent", "auth": "account_key"}, site.SecretChanges{site.SecretAccountKey: azuretest.Key}),
	} {
		if _, err := c.Connect(t.Context(), e.key(s), session); !errors.Is(err, remotefs.ErrNotFound) {
			t.Errorf("Connect() to %s = %v, want ErrNotFound", name, err)
		}
	}
}
