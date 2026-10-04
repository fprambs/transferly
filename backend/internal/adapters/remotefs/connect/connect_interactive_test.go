package connect_test

import (
	"errors"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/adapters/remotefs/ftpfs/ftptest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/prompts"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/testutil"
)

// promptSink hands the prompts a broker publishes to the test.
type promptSink chan prompts.Prompt

// Publish implements [ports.EventSink]; closed prompts are dropped.
func (s promptSink) Publish(e ports.Event) {
	if p, ok := e.Data.(prompts.Prompt); ok {
		s <- p
	}
}

// next returns the next prompt, failing the test when none comes.
func (s promptSink) next(t *testing.T) prompts.Prompt {
	t.Helper()
	select {
	case p := <-s:
		return p
	case <-time.After(10 * time.Second):
		t.Fatal("nothing was asked")
		return prompts.Prompt{}
	}
}

// connected is what a background Connect returned.
type connected struct {
	fsys remotefs.RemoteFS
	err  error
}

// connectInBackground connects to target with c, which may wait for a person, and returns where the result arrives.
func connectInBackground(t *testing.T, e *env, c *connect.Connector, target site.Site) <-chan connected {
	done := make(chan connected, 1)
	go func() {
		fsys, err := c.Connect(t.Context(), e.key(target), session)
		done <- connected{fsys, err}
	}()
	return done
}

// TestConnector_Interactive covers sites of the ["interactive"] logon type in [connect.Connector.Connect] (ADR-0016).
//
// Use cases:
//   - an SFTP site relays the server's keyboard-interactive challenge, a password and a one-time code, to the user who
//     connects, about that site, and the answers sign in with no secret stored and the vault locked
//   - the connection log tells that the server asked, with the name of the challenge, and never the answers
//   - an FTP site asks for the password the same way
//   - a declined prompt is ErrAuth
//   - without a broker, nobody can answer and the site fails with ErrAuth
func TestConnector_Interactive(t *testing.T) {
	e := newEnv(t)
	asked := make(promptSink, 4)
	logged := &collected{}
	broker := prompts.New(asked, time.Minute)
	recorder := connlog.New(logged, testutil.NewFakeClock(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)))
	c := connect.New(e.sites, e.quick, e.hostKeys, e.allow, 5*time.Second, connect.Prompts(broker), connect.ConnectionLog(recorder))
	e.vaults.Lock(session)

	otp := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}, KeyboardOnly: true, Code: "123456"})
	e.trust(otp)
	target := e.addSite(otp, spec{Name: "One-time codes", AuthMethod: "interactive"}, changes{})
	done := connectInBackground(t, e, c, target)
	p := asked.next(t)
	if p.SiteID != target.ID || p.WorkspaceID != e.workspace || p.Challenge.Name != "Two steps" || len(p.Challenge.Questions) != 2 {
		t.Fatalf("prompt = %+v", p)
	}
	if err := broker.Answer(e.user, p.ID, []string{sshPassword, "123456"}); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil {
		t.Fatalf("Connect() answering the challenge = %v", result.err)
	}
	_ = result.fsys.Close()
	var prompted *connlog.Entry
	for _, entry := range logged.take() {
		for _, value := range entry.Params {
			if value == sshPassword || value == "123456" {
				t.Errorf("the log holds an answer: %+v", entry)
			}
		}
		if entry.Step == connlog.Prompted {
			prompted = &entry
		}
	}
	if prompted == nil || prompted.Params["name"] != "Two steps" {
		t.Errorf("prompted entry = %+v", prompted)
	}

	ftpServer := ftptest.Start(t, ftptest.Options{})
	ftpSite := e.addSiteAt(ftpServer.Addr, spec{Name: "FTP asks", Protocol: "ftp", Username: ftpUser, AuthMethod: "interactive"}, changes{})
	done = connectInBackground(t, e, c, ftpSite)
	p = asked.next(t)
	if len(p.Challenge.Questions) != 1 || p.Challenge.Questions[0].Echo {
		t.Fatalf("FTP prompt = %+v", p)
	}
	if err := broker.Answer(e.user, p.ID, []string{ftpPassword}); err != nil {
		t.Fatal(err)
	}
	if result := <-done; result.err != nil {
		t.Fatalf("Connect() to FTP answering the password = %v", result.err)
	} else {
		_ = result.fsys.Close()
	}

	done = connectInBackground(t, e, c, target)
	if err := broker.Cancel(e.user, asked.next(t).ID); err != nil {
		t.Fatal(err)
	}
	if result := <-done; !errors.Is(result.err, remotefs.ErrAuth) {
		t.Errorf("Connect() declined = %v; want ErrAuth", result.err)
	}

	for _, unanswered := range []site.Site{target, ftpSite} {
		if _, err := e.connector(e.allow).Connect(t.Context(), e.key(unanswered), session); !errors.Is(err, remotefs.ErrAuth) {
			t.Errorf("Connect() to %s without a broker = %v; want ErrAuth", unanswered.Name, err)
		}
	}
}
