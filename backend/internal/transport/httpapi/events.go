package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/events"
	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/connlog"
	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/core/prompts"
	"github.com/fprambs/sentinel/internal/core/transfer"
)

// EventsPath is where clients open the WebSocket that streams events.
const EventsPath = "/api/v1/ws"

// WebSocketConfig tunes the event stream. Zero values select the defaults.
type WebSocketConfig struct {
	// PingEvery is how often the server pings an idle client; 30 seconds by default.
	PingEvery time.Duration
	// PingTimeout is how long a client has to answer a ping before it is dropped; 10 seconds by default.
	PingTimeout time.Duration
	// VerifyEvery is how often the session behind the stream is checked; 30 seconds by default.
	VerifyEvery time.Duration
	// WriteTimeout bounds each message written to the client; 5 seconds by default.
	WriteTimeout time.Duration
}

// withDefaults fills in the zero values.
func (c WebSocketConfig) withDefaults() WebSocketConfig {
	for _, d := range []struct {
		field *time.Duration
		value time.Duration
	}{{&c.PingEvery, 30 * time.Second}, {&c.PingTimeout, 10 * time.Second}, {&c.VerifyEvery, 30 * time.Second}, {&c.WriteTimeout, 5 * time.Second}} {
		if *d.field <= 0 {
			*d.field = d.value
		}
	}
	return c
}

// message is what the server sends to a client.
type message struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// progressData is the payload of a progress message.
type progressData struct {
	ID         uuid.UUID `json:"id"`
	BytesDone  int64     `json:"bytesDone"`
	TotalBytes *int64    `json:"totalBytes,omitempty"`
}

// stateData is the payload of a connection.state message: whether the user has connections open to a site (4.11).
type stateData struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
	SiteID      uuid.UUID `json:"siteId"`
	Open        bool      `json:"open"`
}

// logData is the payload of a connection log message: one step of a connection to a site (ADR-0011).
type logData struct {
	WorkspaceID uuid.UUID         `json:"workspaceId"`
	SiteID      uuid.UUID         `json:"siteId"`
	Time        time.Time         `json:"time"`
	Level       string            `json:"level"`
	Step        string            `json:"step"`
	Params      map[string]string `json:"params"`
}

// questionData is one question of a prompt.
type questionData struct {
	Text string `json:"text"`
	Echo bool   `json:"echo"`
}

// promptData is the payload of a connection.prompt message: what a server asks while an interactive site connects
// (ADR-0016).
type promptData struct {
	ID          uuid.UUID      `json:"id"`
	WorkspaceID uuid.UUID      `json:"workspaceId"`
	SiteID      uuid.UUID      `json:"siteId"`
	Name        string         `json:"name"`
	Instruction string         `json:"instruction"`
	Questions   []questionData `json:"questions"`
}

// promptClosedData is the payload of a connection.prompt_closed message: a prompt that no longer waits.
type promptClosedData struct {
	ID uuid.UUID `json:"id"`
}

// promptDTO renders a prompt as the payload of its message.
func promptDTO(p prompts.Prompt) promptData {
	questions := make([]questionData, len(p.Challenge.Questions))
	for i, q := range p.Challenge.Questions {
		questions[i] = questionData{Text: q.Text, Echo: q.Echo}
	}
	return promptData{ID: p.ID, WorkspaceID: p.WorkspaceID, SiteID: p.SiteID, Name: p.Challenge.Name, Instruction: p.Challenge.Instruction, Questions: questions}
}

// snapshotData is the payload of the first message: what is running right now.
type snapshotData struct {
	Transfers []Transfer `json:"transfers"`
}

// toMessage renders an event of the core as a message. Events with a payload the stream does not know are dropped.
func toMessage(e ports.Event) (message, bool) {
	switch data := e.Data.(type) {
	case nil:
		return message{Type: e.Type}, true
	case transfer.Transfer:
		return message{Type: e.Type, Data: transferDTO(data)}, true
	case transfer.Progress:
		return message{Type: e.Type, Data: progressData{ID: data.ID, BytesDone: data.BytesDone, TotalBytes: data.TotalBytes}}, true
	case connections.State:
		return message{Type: e.Type, Data: stateData{WorkspaceID: data.Key.WorkspaceID, SiteID: data.Key.SiteID, Open: data.Open}}, true
	case connlog.Entry:
		return message{Type: e.Type, Data: logData{WorkspaceID: data.WorkspaceID, SiteID: data.SiteID, Time: data.Time, Level: string(data.Level), Step: string(data.Step), Params: data.Params}}, true
	case prompts.Prompt:
		return message{Type: e.Type, Data: promptDTO(data)}, true
	case prompts.Closed:
		return message{Type: e.Type, Data: promptClosedData{ID: data.ID}}, true
	}
	return message{}, false
}

// originPatterns turns the allowed origins into the patterns of the WebSocket library, escaping the characters
// that its glob matching would give a meaning.
func originPatterns(origins []string) []string {
	escape := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`)
	patterns := make([]string, 0, len(origins))
	for _, origin := range origins {
		patterns = append(patterns, escape.Replace(origin))
	}
	return patterns
}

// eventsHandler serves the WebSocket that streams events to a signed-in user.
//
// A browser that opens it from another origin is refused, since a page of another site could otherwise use the
// user's cookie to listen (cross-site WebSocket hijacking). The stream only sends: a client that sends data is
// disconnected. It ends when the session ends, which is checked without counting as use, and when the client
// stops answering pings or cannot keep up.
func eventsHandler(d Deps) http.HandlerFunc {
	cfg := d.WebSocket.withDefaults()
	patterns := originPatterns(d.AllowedOrigins)
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r.Context())
		if !ok {
			writeProblem(w, http.StatusUnauthorized, "unauthenticated", nil)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !contains(d.AllowedOrigins, origin) {
			writeProblem(w, http.StatusForbidden, "cross_site_request", nil)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: patterns})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		streamEvents(r.Context(), conn, d, p, cfg)
	}
}

var (
	// errSessionEnded stops a stream whose session is no longer valid.
	errSessionEnded = errors.New("session ended")
	// errDropped stops a stream that the hub dropped: the client could not keep up or the session signed out.
	errDropped = errors.New("dropped by the hub")
	// errLoadFailed stops a stream that could not read the current transfers.
	errLoadFailed = errors.New("could not load the transfers")
	// errClientGone stops a stream whose client disconnected.
	errClientGone = errors.New("client gone")
)

// stream is one connected client.
type stream struct {
	conn *websocket.Conn
	d    Deps
	p    authenticated
	cfg  WebSocketConfig
	sub  *events.Subscription

	verify *time.Ticker
	ping   *time.Ticker
}

// write sends one message with a deadline.
func (s *stream) write(ctx context.Context, m message) error {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.WriteTimeout)
	defer cancel()
	return wsjson.Write(ctx, s.conn, m)
}

// snapshot sends what is running right now.
func (s *stream) snapshot(ctx context.Context) error {
	current, err := s.d.Transfers.List(ctx, s.p.User.ID, uuid.Nil, 0)
	if err != nil {
		return errLoadFailed
	}
	running := snapshotData{Transfers: []Transfer{}}
	for _, t := range current {
		if t.Status.Active() {
			running.Transfers = append(running.Transfers, transferDTO(t))
		}
	}
	return s.write(ctx, message{Type: "snapshot", Data: running})
}

// step waits for the next thing to do and does it. It returns nil to be called again, or why the stream ends.
func (s *stream) step(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return errClientGone
	case <-s.sub.Done():
		return errDropped
	case e := <-s.sub.Events():
		m, ok := toMessage(e)
		if !ok {
			return nil
		}
		return s.write(ctx, m)
	case <-s.verify.C:
		if _, err := s.d.Auth.Verify(ctx, s.p.token); isAnonymous(err) {
			return errSessionEnded
		}
		return nil
	case <-s.ping.C:
		pingCtx, cancel := context.WithTimeout(ctx, s.cfg.PingTimeout)
		defer cancel()
		return s.conn.Ping(pingCtx)
	}
}

// closeStatus says how a stream that ended with err is closed. Only the endings the server chose get a close
// handshake; when the client stopped answering or the connection failed there is nobody to shake hands with.
func closeStatus(err error) (code websocket.StatusCode, reason string, graceful bool) {
	switch {
	case errors.Is(err, errSessionEnded):
		return websocket.StatusPolicyViolation, "session ended", true
	case errors.Is(err, errDropped):
		return websocket.StatusPolicyViolation, "closed by the server", true
	case errors.Is(err, errLoadFailed):
		return websocket.StatusInternalError, "could not load the transfers", true
	}
	return 0, "", false
}

// streamEvents sends the snapshot and then the events of the user until the connection or the session ends.
func streamEvents(ctx context.Context, conn *websocket.Conn, d Deps, p authenticated, cfg WebSocketConfig) {
	ctx = conn.CloseRead(ctx)
	sub := d.Events.Subscribe(p.User.ID, vaultKey(p))
	defer d.Events.Unsubscribe(sub)
	s := &stream{conn: conn, d: d, p: p, cfg: cfg, sub: sub, verify: time.NewTicker(cfg.VerifyEvery), ping: time.NewTicker(cfg.PingEvery)}
	defer s.verify.Stop()
	defer s.ping.Stop()

	err := s.snapshot(ctx)
	for err == nil {
		err = s.step(ctx)
	}
	if code, reason, graceful := closeStatus(err); graceful {
		_ = conn.Close(code, reason)
	}
}
