package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// syncBuffer is a buffer the server logs into from its goroutines while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends to the buffer.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns what was written so far.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// dialEventsWithBearer opens the event stream with a bearer token and no Origin, as the terminal client does.
func (a *api) dialEventsWithBearer(token string) (*websocket.Conn, int, error) {
	a.t.Helper()
	ctx, cancel := context.WithTimeout(a.t.Context(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(a.srv.URL, "http")+httpapi.EventsPath, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
	if conn != nil {
		a.t.Cleanup(func() { _ = conn.CloseNow() })
	}
	status := 0
	if resp != nil {
		status = resp.StatusCode
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	return conn, status, err
}

// TestTerminalSessions_SecurityReview is the regression net of the security review of the sign-in of the terminal client (T1.7,
// ADR-0029): what a terminal token can reach, how it ends, and what never leaves the server.
//
// Scenarios:
//   - a terminal token has the role of its account and nothing more: a stranger gets 404 on a team and a viewer cannot invite
//   - the event stream accepts the token without an Origin and closes as soon as the session is ended from the browser
//   - a state-changing request with a terminal token and the Origin of another site is still refused by the origin guard
//   - signing out with the token ends that terminal session only, not the browser session of the same account
//   - disabling the account ends its terminal sessions and refuses a code approved before
//   - a code expires 5 minutes after the approval, and a terminal session ends after 7 days unused
//   - neither the token, the code nor the verifier appears in the log, even at debug level, through approvals, exchanges, accepted and
//     refused requests
func TestTerminalSessions_SecurityReview(t *testing.T) {
	t.Parallel()

	t.Run("a terminal token has the role of its account", func(t *testing.T) {
		t.Parallel()
		e := newTeamEnv(t)
		stranger := e.a.cliToken(e.cookie["stranger"], e.csrf["stranger"]).Token
		viewer := e.a.cliToken(e.cookie["viewer"], e.csrf["viewer"]).Token
		team := "/api/v1/workspaces/" + e.ws.String()
		expectProblem(t, e.a.do(request{method: http.MethodGet, path: team + "/members", headers: bearer(stranger)}), 404, "workspace_not_found")
		expectProblem(t, e.a.do(request{method: http.MethodPost, path: team + "/invitations", headers: bearer(viewer), body: map[string]string{"email": "x@example.com", "role": "viewer"}}), 403, "forbidden")
		if r := e.a.do(request{method: http.MethodGet, path: team + "/members", headers: bearer(viewer)}); r.Status != http.StatusOK {
			t.Errorf("members as the viewer with a terminal token = %d %s, want 200", r.Status, r.Body)
		}
	})

	t.Run("the event stream follows the terminal session", func(t *testing.T) {
		t.Parallel()
		a := newAPI(t)
		cookie, csrf := a.setup()
		token := a.cliToken(cookie, csrf).Token
		conn, status, err := a.dialEventsWithBearer(token)
		if err != nil {
			t.Fatalf("dialing with a bearer token = %d, %v", status, err)
		}
		client := newWSClient(t, conn)
		client.expect(t, "snapshot")

		var sessions []httpapi.CliSession
		mustDecode(t, a.do(request{method: http.MethodGet, path: "/api/v1/me/sessions", cookie: cookie}).Body, &sessions)
		if r := a.send(http.MethodDelete, "/api/v1/me/sessions/"+sessions[0].Id.String(), nil, cookie, csrf); r.Status != http.StatusNoContent {
			t.Fatalf("revoke = %d %s", r.Status, r.Body)
		}
		select {
		case <-client.done:
		case <-time.After(5 * time.Second):
			t.Fatal("the event stream of a revoked terminal session stayed open")
		}
		if _, status, err := a.dialEventsWithBearer(token); err == nil || status != http.StatusUnauthorized {
			t.Errorf("dialing with a revoked token = %d, %v, want 401", status, err)
		}
	})

	t.Run("the origin guard still applies to terminal tokens", func(t *testing.T) {
		t.Parallel()
		a := newAPI(t)
		cookie, csrf := a.setup()
		token := a.cliToken(cookie, csrf).Token
		headers := bearer(token)
		headers["Origin"] = "https://evil.example.com"
		expectProblem(t, a.do(request{method: http.MethodPatch, path: "/api/v1/me", headers: headers, body: map[string]string{"theme": "nord"}}), 403, "cross_site_request")
	})

	t.Run("signing out with the token ends that session only", func(t *testing.T) {
		t.Parallel()
		a := newAPI(t)
		cookie, csrf := a.setup()
		token := a.cliToken(cookie, csrf).Token
		if r := a.do(request{method: http.MethodPost, path: "/api/v1/auth/logout", headers: bearer(token)}); r.Status != http.StatusNoContent {
			t.Fatalf("logout with the token = %d %s", r.Status, r.Body)
		}
		expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me", headers: bearer(token)}), 401, "unauthenticated")
		if r := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}); r.Status != http.StatusOK {
			t.Errorf("the browser session after the terminal signed out = %d, want 200", r.Status)
		}
	})

	t.Run("disabling the account ends its terminal sessions and pending codes", func(t *testing.T) {
		t.Parallel()
		a := newAPI(t)
		adminCookie, adminCSRF := a.setup()
		cookie, user := a.signIn("bob@example.com")
		csrf := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}).session(t).CsrfToken
		token := a.cliToken(cookie, csrf).Token
		var code httpapi.CliApprovalCode
		mustDecode(t, a.approveCli(cookie, csrf, approval(nil)).Body, &code)

		if r := a.send(http.MethodPatch, "/api/v1/admin/users/"+user.ID.String(), map[string]bool{"disabled": true}, adminCookie, adminCSRF); r.Status != http.StatusNoContent {
			t.Fatalf("disable = %d %s", r.Status, r.Body)
		}
		expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me", headers: bearer(token)}), 401, "unauthenticated")
		expectProblem(t, a.exchangeCli(code.Code, cliVerifier), 400, "invalid_grant")
	})

	t.Run("codes and terminal sessions expire", func(t *testing.T) {
		t.Parallel()
		a := newAPI(t)
		cookie, csrf := a.setup()
		var code httpapi.CliApprovalCode
		mustDecode(t, a.approveCli(cookie, csrf, approval(nil)).Body, &code)
		a.clock.Advance(5 * time.Minute)
		expectProblem(t, a.exchangeCli(code.Code, cliVerifier), 400, "invalid_grant")

		token := a.cliToken(cookie, csrf).Token
		a.clock.Advance(7 * 24 * time.Hour)
		expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me", headers: bearer(token)}), 401, "unauthenticated")
	})

	t.Run("secrets never reach the log", func(t *testing.T) {
		t.Parallel()
		logs := &syncBuffer{}
		a := newAPI(t, func(d *httpapi.Deps) {
			d.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		})
		cookie, csrf := a.setup()
		approved := a.approveCli(cookie, csrf, approval(nil))
		var code httpapi.CliApprovalCode
		mustDecode(t, approved.Body, &code)
		exchanged := a.exchangeCli(code.Code, cliVerifier)
		var token httpapi.CliToken
		if err := json.Unmarshal(exchanged.Body, &token); err != nil || token.Token == "" {
			t.Fatalf("exchange = %d %s", exchanged.Status, exchanged.Body)
		}
		a.exchangeCli(code.Code, cliVerifier)
		a.do(request{method: http.MethodGet, path: "/api/v1/me", headers: bearer(token.Token)})
		a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: &http.Cookie{Name: "transferly_session", Value: token.Token}})
		a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie, headers: bearer(token.Token)})

		written := logs.String()
		if !strings.Contains(written, "/api/v1/cli/token") {
			t.Fatalf("the log has no request lines, so the check would prove nothing:\n%s", written)
		}
		for name, secret := range map[string]string{"token": token.Token, "code": code.Code, "verifier": cliVerifier, "password": adminPassword} {
			if strings.Contains(written, secret) {
				t.Errorf("the log contains the %s", name)
			}
		}
	})
}
