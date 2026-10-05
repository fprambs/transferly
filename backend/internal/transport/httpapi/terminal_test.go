package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// terminalSite saves a site of the server that runs commands, with its terminal turned on or off, and returns its identifier.
func terminalSite(t *testing.T, a *api, ws uuid.UUID, cookie *http.Cookie, csrf, terminal string) uuid.UUID {
	t.Helper()
	name := shellSite
	if terminal != "true" {
		name += " without a terminal"
	}
	body := map[string]any{"name": name, "connector": "sftp", "config": map[string]any{"host": "8.8.8.8", "username": "deploy", "terminal": terminal}}
	created := a.send(http.MethodPost, sitesPath(ws), body, cookie, csrf)
	if created.Status != http.StatusCreated {
		t.Fatalf("create site = %d %s", created.Status, created.Body)
	}
	return created.site(t).Id
}

// dialTerminal opens the terminal of a site with a session, an Origin and a query, and answers the connection or the status of a
// refusal.
func (a *api) dialTerminal(ws, site string, cookie *http.Cookie, origin, query string) (*websocket.Conn, int) {
	a.t.Helper()
	header := http.Header{}
	if cookie != nil {
		header.Set("Cookie", cookie.String())
	}
	if origin != "" {
		header.Set("Origin", origin)
	}
	ctx, cancel := context.WithTimeout(a.t.Context(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(a.srv.URL, "http") + "/api/v1/workspaces/" + ws + "/sites/" + site + "/terminal" + query
	conn, resp, _ := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
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
	return conn, status
}

// readTerminal reads the next message of a terminal.
func readTerminal(t *testing.T, conn *websocket.Conn) (websocket.MessageType, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	kind, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	return kind, string(data)
}

// writeTerminal sends a message to a terminal.
func writeTerminal(t *testing.T, conn *websocket.Conn, kind websocket.MessageType, data string) {
	t.Helper()
	if err := conn.Write(t.Context(), kind, []byte(data)); err != nil {
		t.Fatalf("Write() = %v", err)
	}
}

// TestTerminal verifies the terminal of a site (ADR-0027).
//
// Scenarios:
//   - an admin opens the terminal of a site that has it on, in a folder and on a screen of the size asked for; what is typed reaches
//     the shell and what it prints comes back; a resize reaches it, and malformed or oversized controls are ignored; when the shell
//     ends the client is told its status and the connection closes normally; the activity records the terminal, never what was typed
//   - a terminal whose client goes away ends its shell, and a shell that refuses what is typed ends with no status; a request that
//     is not a WebSocket upgrade is refused
//   - a site with its terminal off is 403 terminal_off; anonymous callers 401, a foreign origin 403 cross_site_request, and identifiers
//     that are not UUIDs 400
//   - a connection that cannot be opened is reported before the upgrade, as a 500 without details when it is unexpected
func TestTerminal(t *testing.T) {
	e := newFileEnv(t)
	a := e.a
	site := terminalSite(t, a, e.ws, e.cookie, e.csrf, "true").String()
	conn, status := a.dialTerminal(e.ws.String(), site, e.cookie, "", "?path=%2Fsrv&cols=100&rows=30")
	if conn == nil {
		t.Fatalf("dial = %d", status)
	}
	if kind, text := readTerminal(t, conn); kind != websocket.MessageBinary || text != "ready in /srv 100x30\n" {
		t.Errorf("first message = %v %q", kind, text)
	}
	writeTerminal(t, conn, websocket.MessageBinary, "ls secret-word\n")
	if _, text := readTerminal(t, conn); text != "echo: ls secret-word\n" {
		t.Errorf("echo = %q", text)
	}
	writeTerminal(t, conn, websocket.MessageText, `{"type":"resize","cols":5000,"rows":40}`)
	writeTerminal(t, conn, websocket.MessageText, `not json`)
	writeTerminal(t, conn, websocket.MessageText, `{"type":"resize","cols":120,"rows":40}`)
	if _, text := readTerminal(t, conn); text != "resized 120x40\n" {
		t.Errorf("resize = %q", text)
	}
	writeTerminal(t, conn, websocket.MessageBinary, "exit\n")
	if kind, text := readTerminal(t, conn); kind != websocket.MessageText || text != `{"type":"exit","code":5}` {
		t.Errorf("exit = %v %q", kind, text)
	}
	if _, _, err := conn.Read(t.Context()); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Errorf("after exit = %v, want a normal closure", err)
	}
	waitFor(t, "the terminal to be logged", func() bool {
		for _, entry := range e.activityOf("") {
			if entry.Kind == "terminal" && entry.Path == "/srv" && !strings.Contains(*entry.Detail, "secret-word") {
				return true
			}
		}
		return false
	})

	gone, _ := a.dialTerminal(e.ws.String(), site, e.cookie, "", "")
	if _, text := readTerminal(t, gone); text != "ready in  80x24\n" {
		t.Errorf("default screen = %q", text)
	}
	_ = gone.CloseNow()
	waitFor(t, "the closed terminal to be logged", func() bool {
		count := 0
		for _, entry := range e.activityOf("") {
			if entry.Kind == "terminal" {
				count++
			}
		}
		return count == 2
	})

	refusing, _ := a.dialTerminal(e.ws.String(), site, e.cookie, "", "")
	readTerminal(t, refusing)
	writeTerminal(t, refusing, websocket.MessageBinary, "fail\n")
	if _, text := readTerminal(t, refusing); text != `{"type":"exit","code":-1}` {
		t.Errorf("exit of a shell that refused input = %q", text)
	}
	if r := a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + e.ws.String() + "/sites/" + site + "/terminal", cookie: e.cookie}); r.Status != http.StatusUpgradeRequired {
		t.Errorf("plain GET of a terminal = %d, want 426", r.Status)
	}

	off := terminalSite(t, a, e.ws, e.cookie, e.csrf, "false").String()
	if _, status := a.dialTerminal(e.ws.String(), off, e.cookie, "", ""); status != http.StatusForbidden {
		t.Errorf("terminal off = %d, want 403", status)
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + e.ws.String() + "/sites/" + off + "/terminal", cookie: e.cookie}), 403, "terminal_off")
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + e.ws.String() + "/sites/" + site + "/terminal"}), 401, "unauthenticated")
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + e.ws.String() + "/sites/" + site + "/terminal", cookie: e.cookie, headers: map[string]string{"Origin": "https://evil.example"}}), 403, "cross_site_request")
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/x/sites/y/terminal", cookie: e.cookie}), 400, "invalid_request")
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + e.ws.String() + "/sites/" + uuid.NewString() + "/terminal", cookie: e.cookie}), 404, "site_not_found")

	broken := newAPI(t, withConnectError(errors.New("boom: secret detail")))
	cookie, csrf := broken.setup()
	ws := broken.personal(cookie)
	brokenSite := terminalSite(t, broken, ws, cookie, csrf, "true").String()
	expectProblem(t, broken.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + ws.String() + "/sites/" + brokenSite + "/terminal", cookie: cookie}), 500, "internal_error")
}

// TestTerminalNeedsAdmin verifies that only admins open terminals, even on a site that has it on.
func TestTerminalNeedsAdmin(t *testing.T) {
	e := newTeamEnv(t)
	created := e.call("admin", http.MethodPost, "/sites", map[string]any{"name": shellSite, "connector": "sftp", "config": map[string]any{"host": "8.8.8.8", "username": "deploy", "terminal": "true"}})
	if created.Status != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Status, created.Body)
	}
	path := "/api/v1/workspaces/" + e.ws.String() + "/sites/" + created.site(t).Id.String() + "/terminal"
	expectProblem(t, e.a.do(request{method: http.MethodGet, path: path, cookie: e.cookie["editor"]}), 403, "forbidden")
	_ = httpapi.TerminalPath
}
