package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/transfer"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// wsMessage is a message received from the event stream.
type wsMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// dialEvents opens the event stream with a session and an optional Origin header.
func (a *api) dialEvents(cookie *http.Cookie, origin string) (*websocket.Conn, int, error) {
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
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(a.srv.URL, "http")+httpapi.EventsPath, &websocket.DialOptions{HTTPHeader: header})
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

// wsClient reads a stream in the background, so that waiting for a message never closes the connection.
type wsClient struct {
	conn *websocket.Conn
	msgs chan wsMessage
	done chan struct{}
	err  error
}

// newWSClient starts reading a connection.
func newWSClient(t *testing.T, conn *websocket.Conn) *wsClient {
	t.Helper()
	c := &wsClient{conn: conn, msgs: make(chan wsMessage, 256), done: make(chan struct{})}
	go func() {
		defer close(c.done)
		for {
			_, raw, err := conn.Read(context.Background())
			if err != nil {
				c.err = err
				return
			}
			var m wsMessage
			if err := json.Unmarshal(raw, &m); err != nil {
				c.err = err
				return
			}
			c.msgs <- m
		}
	}()
	return c
}

// connectEvents opens the stream and reads its snapshot.
func (a *api) connectEvents(cookie *http.Cookie) (*wsClient, []httpapi.Transfer) {
	a.t.Helper()
	conn, _, err := a.dialEvents(cookie, localOrigin)
	if err != nil {
		a.t.Fatalf("dialing the event stream: %v", err)
	}
	client := newWSClient(a.t, conn)
	first := client.expect(a.t, "snapshot")
	var data struct {
		Transfers []httpapi.Transfer `json:"transfers"`
	}
	if err := json.Unmarshal(first.Data, &data); err != nil {
		a.t.Fatal(err)
	}
	return client, data.Transfers
}

// expect waits until a message of the given type arrives, skipping others.
func (c *wsClient) expect(t *testing.T, kind string) wsMessage {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case m := <-c.msgs:
			if m.Type == kind {
				return m
			}
		case <-c.done:
			t.Fatalf("the stream ended (%v) while waiting for a %q message", c.err, kind)
		case <-timeout:
			t.Fatalf("timed out waiting for a %q message", kind)
		}
	}
}

// silent fails when a message arrives within a short time.
func (c *wsClient) silent(t *testing.T) {
	t.Helper()
	select {
	case m := <-c.msgs:
		t.Fatalf("unexpected message %s %s", m.Type, m.Data)
	case <-time.After(250 * time.Millisecond):
	}
}

// closeCode waits for the stream to end and returns the status it was closed with.
func (c *wsClient) closeCode(t *testing.T) websocket.StatusCode {
	t.Helper()
	select {
	case <-c.done:
		return websocket.CloseStatus(c.err)
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end")
		return 0
	}
}

// slowUpload starts an upload whose body the test feeds by hand.
func slowUpload(t *testing.T, e *fileEnv, id uuid.UUID, name string) (*io.PipeWriter, chan reply) {
	t.Helper()
	pr, pw := io.Pipe()
	replies := make(chan reply, 1)
	go func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, e.a.srv.URL+filesPath(e.ws, e.site, "/content", q("path", name, "transferId", id.String(), "size", "20")), pr)
		if err != nil {
			t.Error(err)
			return
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-CSRF-Token", e.csrf)
		req.AddCookie(e.cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		replies <- reply{Status: resp.StatusCode, Header: resp.Header, Body: raw}
	}()
	return pw, replies
}

// TestEvents_Access verifies who may open the stream.
//
// Scenarios:
//   - anonymous callers get 401 and pages of other origins 403, before any upgrade
//   - a request that does not ask for a WebSocket upgrade is refused by the library, not served
//   - an allowed origin connects, and so does a client that sends no Origin, such as a script
//   - the first message is a snapshot with what is running, and nothing else was sent yet
func TestEvents_Access(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	a := e.a

	if _, status, err := a.dialEvents(nil, localOrigin); err == nil || status != http.StatusUnauthorized {
		t.Errorf("anonymous dial = %d, %v; want 401", status, err)
	}
	for _, origin := range []string{"https://evil.example", "http://localhost:8081", "null"} {
		if _, status, err := a.dialEvents(e.cookie, origin); err == nil || status != http.StatusForbidden {
			t.Errorf("Origin %q dial = %d, %v; want 403", origin, status, err)
		}
	}
	if plain := a.do(request{method: http.MethodGet, path: httpapi.EventsPath, cookie: e.cookie}); plain.Status < 400 || plain.Status >= 500 {
		t.Errorf("a plain GET = %d; want a client error", plain.Status)
	}
	for _, origin := range []string{localOrigin, ""} {
		raw, _, err := a.dialEvents(e.cookie, origin)
		if err != nil {
			t.Fatalf("Origin %q dial = %v", origin, err)
		}
		if snapshot := newWSClient(t, raw).expect(t, "snapshot"); !strings.Contains(string(snapshot.Data), `"transfers":[]`) {
			t.Errorf("snapshot = %s; want no transfers", snapshot.Data)
		}
	}

	id := uuid.New()
	pw, replies := slowUpload(t, e, id, "/running")
	if _, err := pw.Write([]byte("start")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the upload to run", func() bool { return a.transferState(e.cookie, id) == "running" })
	conn, running := a.connectEvents(e.cookie)
	if len(running) != 1 || running[0].Id != id || running[0].Status != "running" {
		t.Errorf("snapshot transfers = %+v; want the running upload", running)
	}
	_ = conn
	_ = pw.Close()
	<-replies
}

// TestEvents_Stream verifies what is delivered while transfers move.
//
// Scenarios:
//   - state changes and progress of the user's transfers arrive as messages, with the same fields as the REST record
//   - the transfers of another user are never delivered
//   - locking the vault sends vault.locked to the user's clients
//   - events with a payload the stream does not know are skipped
func TestEvents_Stream(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	a := e.a
	conn, _ := a.connectEvents(e.cookie)

	otherCookie, other := a.signIn("other@example.com")
	otherConn, _ := a.connectEvents(otherCookie)

	id := uuid.New()
	pw, replies := slowUpload(t, e, id, "/streamed")
	if _, err := pw.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	state := conn.expect(t, "transfer.state")
	var tr httpapi.Transfer
	if err := json.Unmarshal(state.Data, &tr); err != nil || tr.Id != id || tr.Direction != "upload" || tr.Path != "/streamed" {
		t.Fatalf("state message = %s (%v)", state.Data, err)
	}
	waitFor(t, "the upload to run", func() bool { return a.transferState(e.cookie, id) == "running" })
	a.clock.Advance(time.Second)
	if _, err := pw.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	progress := conn.expect(t, "transfer.progress")
	var moved struct {
		ID         uuid.UUID `json:"id"`
		BytesDone  int64     `json:"bytesDone"`
		TotalBytes *int64    `json:"totalBytes"`
	}
	if err := json.Unmarshal(progress.Data, &moved); err != nil || moved.ID != id || moved.BytesDone < 10 || moved.TotalBytes == nil || *moved.TotalBytes != 20 {
		t.Errorf("progress message = %s (%v)", progress.Data, err)
	}
	_ = pw.Close()
	<-replies
	done := conn.expect(t, "transfer.state")
	for !strings.Contains(string(done.Data), `"done"`) {
		done = conn.expect(t, "transfer.state")
	}
	otherConn.silent(t)

	a.hub.Publish(ports.Event{Type: "mystery", UserID: a.userID(e.cookie), Data: 42})
	if r := a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("vault setup = %d", r.Status)
	}
	if r := a.vaultCall("/lock", nil, e.cookie, e.csrf); r.Status != http.StatusNoContent {
		t.Fatalf("lock = %d", r.Status)
	}
	locked := conn.expect(t, "vault.locked")
	if string(locked.Data) != "null" {
		t.Errorf("vault.locked data = %s", locked.Data)
	}
	_ = other
}

// userID returns the identifier of a session's user.
func (a *api) userID(cookie *http.Cookie) uuid.UUID {
	a.t.Helper()
	return a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}).session(a.t).User.Id
}

// TestEvents_Lifecycle verifies how streams end.
//
// Scenarios:
//   - signing out closes the session's streams with a policy violation, and not those of other sessions
//   - a session that expired is detected by the periodic check and its stream is closed, without the stream having
//     kept the session alive
//   - a client that sends data is disconnected
//   - a client that stops answering pings is dropped by the server
//   - a stream the hub dropped is closed
//   - when the transfers cannot be read the stream is closed with an internal error
func TestEvents_Lifecycle(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	a := e.a

	second := a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": adminEmail, "password": adminPassword}})
	staying, _ := a.connectEvents(second.sessionCookie())
	leaving, _ := a.connectEvents(e.cookie)
	if r := a.do(request{method: http.MethodPost, path: "/api/v1/auth/logout", cookie: e.cookie, headers: map[string]string{"X-CSRF-Token": e.csrf}}); r.Status != http.StatusNoContent {
		t.Fatalf("logout = %d", r.Status)
	}
	if code := leaving.closeCode(t); code != websocket.StatusPolicyViolation {
		t.Errorf("stream of a signed-out session closed with %d; want a policy violation", code)
	}
	staying.silent(t)

	a.clock.Advance(31 * time.Minute)
	if code := staying.closeCode(t); code != websocket.StatusPolicyViolation {
		t.Errorf("stream of an expired session closed with %d; want a policy violation", code)
	}

	fresh := newFileEnv(t)
	talker, _ := fresh.a.connectEvents(fresh.cookie)
	if err := talker.conn.Write(t.Context(), websocket.MessageText, []byte(`{"type":"hello"}`)); err != nil {
		t.Fatal(err)
	}
	if code := talker.closeCode(t); code != websocket.StatusPolicyViolation {
		t.Errorf("a client that sends data was closed with %d; want a policy violation", code)
	}

	deaf, _, err := fresh.a.dialEvents(fresh.cookie, localOrigin)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the deaf client to be dropped", func() bool { return fresh.a.hub.Count() == 0 })
	_ = deaf

	dropped, _ := fresh.a.connectEvents(fresh.cookie)
	fresh.a.hub.CloseSession(fresh.a.sessionOf(fresh.cookie))
	if code := dropped.closeCode(t); code != websocket.StatusPolicyViolation {
		t.Errorf("a dropped stream closed with %d", code)
	}
}

// sessionOf returns the identifier of the session behind a cookie, as the hub knows it.
func (a *api) sessionOf(cookie *http.Cookie) string {
	a.t.Helper()
	principal, err := a.auth.Authenticate(a.t.Context(), cookie.Value)
	if err != nil {
		a.t.Fatal(err)
	}
	return principal.Session.ID.String()
}

// TestEvents_Failures verifies how the stream reports failures of the server.
//
// Scenarios:
//   - when the current transfers cannot be read, the stream is closed with an internal error
func TestEvents_Failures(t *testing.T) {
	t.Parallel()
	a := newAPI(t, func(d *httpapi.Deps) {
		d.Transfers = transfer.NewService(brokenTransfers{d.Readiness.(*postgres.Store)}, &recordedEvents{}, realClock{}, transfer.Config{}, nil)
	})
	cookie, _ := a.setup()
	raw, _, err := a.dialEvents(cookie, localOrigin)
	if err != nil {
		t.Fatal(err)
	}
	if code := newWSClient(t, raw).closeCode(t); code != websocket.StatusInternalError {
		t.Errorf("closed with %d; want an internal error", code)
	}
}

// TestEvents_DoesNotKeepSessionsAlive verifies that an open stream does not count as activity.
//
// Scenarios:
//   - a stream stays open while its session is valid, and closes once the session was idle for the idle limit in
//     total, although the stream checked it many times in between
func TestEvents_DoesNotKeepSessionsAlive(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	client, _ := e.a.connectEvents(e.cookie)

	e.a.clock.Advance(20 * time.Minute)
	client.silent(t)
	if e.a.hub.Count() != 1 {
		t.Fatal("the stream closed while its session was still valid")
	}
	e.a.clock.Advance(20 * time.Minute)
	if code := client.closeCode(t); code != websocket.StatusPolicyViolation {
		t.Errorf("stream closed with %d; want a policy violation once the session was idle for 40 minutes", code)
	}
}
