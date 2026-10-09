package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/fprambs/transferly/internal/adapters/remotefs/memfs"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/testutil"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// TestTerminalRevocation verifies that an existing WebSocket cannot retain access after logout or absolute expiry.
func TestTerminalRevocation(t *testing.T) {
	for _, logout := range []bool{true, false} {
		t.Run(map[bool]string{true: "logout", false: "expiry"}[logout], func(t *testing.T) {
			e := newFileEnv(t)
			site := terminalSite(t, e.a, e.ws, e.cookie, e.csrf, "true")
			conn, status := e.a.dialTerminal(e.ws.String(), site.String(), e.cookie, "", "")
			if conn == nil {
				t.Fatalf("dial = %d", status)
			}
			readTerminal(t, conn)
			if logout {
				if r := e.a.send(http.MethodPost, "/api/v1/auth/logout", nil, e.cookie, e.csrf); r.Status != http.StatusNoContent {
					t.Fatalf("logout = %d", r.Status)
				}
			} else {
				e.a.clock.Advance(9 * time.Hour)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_ = conn.Write(ctx, websocket.MessageBinary, []byte("forbidden\n"))
			kind, data, err := conn.Read(ctx)
			if err == nil && kind == websocket.MessageBinary {
				t.Fatalf("revoked terminal accepted input: %q", data)
			}
		})
	}
}

// TestTerminalPermissionChanges verifies revocation even when permissions change outside the handler that opened the shell.
func TestTerminalPermissionChanges(t *testing.T) {
	for _, change := range []string{"role", "deleted", "disabled"} {
		t.Run(change, func(t *testing.T) {
			e := newFileEnv(t)
			site := terminalSite(t, e.a, e.ws, e.cookie, e.csrf, "true")
			conn, status := e.a.dialTerminal(e.ws.String(), site.String(), e.cookie, "", "")
			if conn == nil {
				t.Fatalf("dial = %d", status)
			}
			readTerminal(t, conn)
			var err error
			switch change {
			case "role":
				_, err = e.a.store.DB().Exec(t.Context(), "UPDATE memberships SET role='viewer' WHERE workspace_id=$1", e.ws)
			case "deleted":
				_, err = e.a.store.DB().Exec(t.Context(), "DELETE FROM sites WHERE id=$1", site)
			case "disabled":
				_, err = e.a.store.DB().Exec(t.Context(), `UPDATE sites SET config=jsonb_set(config,'{terminal}','"false"') WHERE id=$1`, site)
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_ = conn.Write(ctx, websocket.MessageBinary, []byte("no longer allowed\n"))
			kind, data, err := conn.Read(ctx)
			if err == nil && kind == websocket.MessageBinary {
				t.Fatalf("accepted revoked permission: %q", data)
			}
		})
	}
}

// revokingShell runs a hook after shell creation to model session revocation while the terminal opens.
type revokingShell struct {
	shellFS
	hook func(context.Context)
}

// Shell exposes the opening race without accepting any interactive input before the handler validates again.
func (f revokingShell) Shell(ctx context.Context, dir string, cols, rows int) (remotefs.Terminal, error) {
	term, err := f.shellFS.Shell(ctx, dir, cols, rows)
	if err == nil {
		f.hook(ctx)
	}
	return term, err
}

// terminalConnector lends a test shell independently of the name of a saved or quick site.
type terminalConnector struct{ fs remotefs.RemoteFS }

// Connect returns the shell-capable test connection.
func (c terminalConnector) Connect(context.Context, connections.Key, string) (remotefs.RemoteFS, error) {
	return c.fs, nil
}

// TestTerminalOpeningRace checks the session again after a shell opens, before accepting a WebSocket.
func TestTerminalOpeningRace(t *testing.T) {
	var token string
	a := newAPI(t, func(d *httpapi.Deps) {
		clock := testutil.NewFakeClock(time.Now())
		fs := revokingShell{shellFS: shellFS{&sharedFS{memfs.New(clock)}}, hook: func(ctx context.Context) {
			if err := d.Auth.Logout(ctx, token); err != nil {
				t.Error(err)
			}
		}}
		pool := connections.New(terminalConnector{fs}, clock, connections.Config{Attempts: 1})
		t.Cleanup(pool.Close)
		d.Connections = pool
		d.Files = files.NewService(d.Workspaces, pool).WithCommands(d.Quotas.ServerCommandsAllowed)
	})
	cookie, csrf := a.setup()
	token = cookie.Value
	ws := a.personal(cookie)
	site := terminalSite(t, a, ws, cookie, csrf, "true")
	if conn, status := a.dialTerminal(ws.String(), site.String(), cookie, "", ""); conn != nil || status != http.StatusForbidden {
		t.Fatalf("accepted revoked opening: %d", status)
	}
}

// TestTerminalExpiryDuringOpening revokes a shell still opening without waiting for the WebSocket handshake or input.
func TestTerminalExpiryDuringOpening(t *testing.T) {
	var expire func()
	a := newAPI(t, func(d *httpapi.Deps) {
		clock := testutil.NewFakeClock(time.Now())
		fs := revokingShell{shellFS: shellFS{&sharedFS{memfs.New(clock)}}, hook: func(ctx context.Context) {
			expire()
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
				t.Error("session expiry did not cancel shell opening")
			}
		}}
		pool := connections.New(terminalConnector{fs}, clock, connections.Config{Attempts: 1})
		t.Cleanup(pool.Close)
		d.Connections = pool
		d.Files = files.NewService(d.Workspaces, pool).WithCommands(d.Quotas.ServerCommandsAllowed)
	})
	expire = func() { a.clock.Advance(9 * time.Hour) }
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	site := terminalSite(t, a, ws, cookie, csrf, "true")
	if conn, status := a.dialTerminal(ws.String(), site.String(), cookie, "", ""); conn != nil || status != http.StatusForbidden {
		t.Fatalf("accepted expired opening: %d", status)
	}
}

// TestQuickTerminal validates the lifetime of quick-site terminals as well as saved ones.
func TestQuickTerminal(t *testing.T) {
	a := newAPI(t, func(d *httpapi.Deps) {
		clock := testutil.NewFakeClock(time.Now())
		pool := connections.New(terminalConnector{shellFS{&sharedFS{memfs.New(clock)}}}, clock, connections.Config{Attempts: 1})
		t.Cleanup(pool.Close)
		d.Connections = pool
		d.Files = files.NewService(d.Workspaces, pool).WithCommands(d.Quotas.ServerCommandsAllowed)
	})
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	q := a.send(http.MethodPost, "/api/v1/workspaces/"+ws.String()+"/quick-connections", map[string]any{"connector": "sftp", "config": map[string]string{"host": "8.8.8.8", "username": "deploy", "auth": "password", "terminal": "true"}, "secrets": map[string]string{"password": "synthetic-test-password"}}, cookie, csrf)
	if q.Status != http.StatusCreated {
		t.Fatalf("quick = %d %s", q.Status, q.Body)
	}
	quick := q.quickConnection(t)
	conn, status := a.dialTerminal(ws.String(), quick.Id.String(), cookie, "", "")
	if conn == nil {
		t.Fatalf("quick terminal = %d", status)
	}
	readTerminal(t, conn)
	writeTerminal(t, conn, websocket.MessageBinary, "exit\n")
	readTerminal(t, conn)
}
