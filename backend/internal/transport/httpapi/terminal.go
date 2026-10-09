package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/activity"
	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/workspace"
)

// TerminalPath is where clients open the WebSocket of a terminal on the server of a site (ADR-0027).
const TerminalPath = "/api/v1/workspaces/{workspaceId}/sites/{siteId}/terminal"

// The bounds of the screen a terminal is opened with, and its size when the client gives none.
const (
	defaultCols, defaultRows = 80, 24
	maxScreen                = 1000
	terminalReadLimit        = 1 << 20
	terminalWriteTimeout     = 10 * time.Second
)

// errTerminalOff means the site does not have its terminal turned on.
var errTerminalOff = errors.New("the terminal of this site is off")

// terminalControl is a text message of a terminal: the client resizes the screen, and the server tells the shell ended.
type terminalControl struct {
	Type string `json:"type"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
	Code *int   `json:"code,omitempty"`
}

// screenSize reads a dimension of the screen from the query, or its default when it is missing or out of bounds.
func screenSize(r *http.Request, name string, fallback int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || n < 1 || n > maxScreen {
		return fallback
	}
	return n
}

// terminalHandler opens a terminal on the server of a site and relays it over a WebSocket: binary messages carry what is typed
// and what the shell prints, a text message {"type":"resize","cols","rows"} resizes the screen, and {"type":"exit","code"} tells
// the shell ended. The site must have its terminal on, only admins may open it, and the request needs a session and comes from an
// allowed origin. The activity of the person records the terminal and how long it was open, never what was typed.
func terminalHandler(d Deps) http.HandlerFunc {
	patterns := originPatterns(d.AllowedOrigins)
	s := newServer(d)
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
		workspaceID, errWS := uuid.Parse(chi.URLParam(r, "workspaceId"))
		siteID, errSite := uuid.Parse(chi.URLParam(r, "siteId"))
		if errWS != nil || errSite != nil {
			writeProblem(w, http.StatusBadRequest, "invalid_request", nil)
			return
		}
		ctx, unregister := d.terminals.register(r.Context(), vaultKey(p), p.User.ID, workspaceID)
		defer unregister()
		t := target(ctx, workspaceID, siteID)
		valid := func() bool { return s.terminalValid(ctx, p, t) }
		go monitorTerminal(ctx, unregister, d.WebSocket.withDefaults().VerifyEvery, valid)
		dir := r.URL.Query().Get("path")
		term, release, err := s.openTerminal(ctx, t.Actor, workspaceID, siteID, dir, screenSize(r, "cols", defaultCols), screenSize(r, "rows", defaultRows))
		if err != nil {
			if problem, ok := asProblem(err); ok {
				_ = problem.write(w)
				return
			}
			writeProblem(w, http.StatusInternalServerError, "internal_error", nil)
			return
		}
		defer release()
		defer func() { _ = term.Close() }()
		if !valid() {
			writeProblem(w, http.StatusForbidden, "forbidden", nil)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: patterns})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		stop := context.AfterFunc(ctx, func() { _ = conn.CloseNow(); _ = term.Close() })
		defer stop()
		conn.SetReadLimit(terminalReadLimit)
		opened := time.Now()
		relayTerminal(ctx, conn, term, valid)
		s.audit(context.WithoutCancel(ctx), t, activity.Terminal, dir, time.Since(opened).Round(time.Second).String(), nil)
	}
}

// terminalValid rechecks the session and current permissions before accepting terminal input.
func (s *server) terminalValid(ctx context.Context, p authenticated, t files.Target) bool {
	if ctx.Err() != nil {
		return false
	}
	if _, err := s.deps.Auth.Verify(ctx, p.token); err != nil {
		return false
	}
	if err := s.deps.Files.Check(ctx, t, workspace.ActionElevate); err != nil {
		return false
	}
	if found, ok := s.deps.Quick.Describe(t.Actor, t.Workspace, t.Site); ok {
		return found.Setting("terminal") == "true" && s.deps.Quotas.ServerCommandsAllowed()
	}
	found, err := s.deps.Sites.Get(ctx, t.Actor, t.Workspace, t.Site)
	if err != nil {
		return false
	}
	if found.Setting("terminal") != "true" {
		return false
	}
	return !s.deps.Quotas.Current().ServerCommandsOff
}

// monitorTerminal revokes idle terminals when their session or authorization expires.
func monitorTerminal(ctx context.Context, cancel func(), every time.Duration, valid func() bool) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !valid() {
				cancel()
				return
			}
		}
	}
}

// openTerminal checks that the site has its terminal on and opens it in dir.
func (s *server) openTerminal(ctx context.Context, actor, workspaceID, siteID uuid.UUID, dir string, cols, rows int) (remotefs.Terminal, func(), error) {
	found, ok := s.deps.Quick.Describe(actor, workspaceID, siteID)
	if !ok {
		var err error
		if found, err = s.deps.Sites.Get(ctx, actor, workspaceID, siteID); err != nil {
			return nil, nil, err
		}
	}
	if found.Setting("terminal") != "true" {
		return nil, nil, errTerminalOff
	}
	return s.deps.Files.Terminal(ctx, target(ctx, workspaceID, siteID), dir, cols, rows)
}

// relayTerminal copies what the shell prints to the client and what the client sends to the shell until either ends, then tells
// the client how the shell ended. When the client goes away, or the shell refuses what is typed, the shell is closed, which ends
// the copy of what it prints.
func relayTerminal(ctx context.Context, conn *websocket.Conn, term remotefs.Terminal, valid func() bool) {
	reading, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer func() {
			cancel()
			_ = term.Close()
		}()
		for {
			kind, data, err := conn.Read(reading)
			if err != nil {
				return
			}
			if !valid() {
				return
			}
			if kind == websocket.MessageBinary {
				if _, err := term.Write(data); err != nil {
					return
				}
				continue
			}
			var control terminalControl
			if json.Unmarshal(data, &control) == nil && control.Type == "resize" && control.Cols > 0 && control.Rows > 0 && control.Cols <= maxScreen && control.Rows <= maxScreen {
				_ = term.Resize(control.Cols, control.Rows)
			}
		}
	}()
	buf := make([]byte, 32<<10)
	for {
		n, err := term.Read(buf)
		if n > 0 {
			_ = writeTerminal(ctx, conn, websocket.MessageBinary, buf[:n])
		}
		if err != nil {
			break
		}
	}
	code, err := term.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		code = -1
	}
	message, _ := json.Marshal(terminalControl{Type: "exit", Code: &code})
	if writeTerminal(ctx, conn, websocket.MessageText, message) == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "the shell ended")
	}
}

// writeTerminal writes one message to the client with a deadline.
func writeTerminal(ctx context.Context, conn *websocket.Conn, kind websocket.MessageType, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, terminalWriteTimeout)
	defer cancel()
	return conn.Write(ctx, kind, data)
}
