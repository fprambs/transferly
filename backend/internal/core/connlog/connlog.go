// Package connlog tells the user, live, what a connection is doing (ADR-0011): each step as it happens (connecting,
// the server key checked, signed in, listing) and each failure with its stable code. Entries carry a step code and
// fixed parameters that the client translates; they never carry a password, key, passphrase or file content.
package connlog

import (
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/ports"
)

// EventType is the type of the events that carry entries.
const EventType = "connection.log"

// Level is how an entry reads: a step, something to notice, or a failure.
type Level string

const (
	// Info is a step that went as expected.
	Info Level = "info"
	// Warn is a step that worked but deserves attention, such as FTP going on without TLS.
	Warn Level = "warn"
	// Error is a failure.
	Error Level = "error"
)

// Step is the stable code of an entry.
type Step string

const (
	// Connecting starts a connection: params host, port, via (direct, proxy or jump), and hop "jump" for the jump host.
	Connecting Step = "connecting"
	// Connected means the TCP connection is open: params address, the address actually reached.
	Connected Step = "connected"
	// HostKeyVerified means the server key or certificate is the trusted one: params keyType, fingerprint.
	HostKeyVerified Step = "host_key_verified"
	// HostKeyUnknown means the server key is not trusted yet and waits for the user: params keyType, fingerprint.
	HostKeyUnknown Step = "host_key_unknown"
	// HostKeyChanged means the server key differs from the trusted one and was refused: params keyType, fingerprint.
	HostKeyChanged Step = "host_key_changed"
	// PlainText means an FTP site with optional encryption went on without TLS because the server offers none.
	PlainText Step = "plain_text"
	// Prompted means the server asked the person connecting something, which an interactive site relays to them
	// (ADR-0016): params name, the server's title of the challenge, which may be empty. The answers are never logged.
	Prompted Step = "prompted"
	// Authenticated means the account signed in: params method, username.
	Authenticated Step = "authenticated"
	// Listing means a folder is being read: params path.
	Listing Step = "listing"
	// Listed means a folder was read: params path, count.
	Listed Step = "listed"
	// Failed means an operation failed: params code, the stable error code the API answers.
	Failed Step = "failed"
	// Canceled means the user canceled an operation before it ended: params path.
	Canceled Step = "canceled"
	// Disconnected means the user closed the connections to the site.
	Disconnected Step = "disconnected"
)

// Entry is one line of the log of a site.
type Entry struct {
	// WorkspaceID and SiteID say which site, or quick connection, the entry is about.
	WorkspaceID, SiteID uuid.UUID
	// Time is when it happened.
	Time time.Time
	// Level is how it reads.
	Level Level
	// Step is its stable code.
	Step Step
	// Params are the values the message shows; never a secret.
	Params map[string]string
}

// Target is the site a connection belongs to, and the user who sees its log.
type Target struct {
	// UserID is the user who receives the entries.
	UserID uuid.UUID
	// WorkspaceID and SiteID say which site.
	WorkspaceID, SiteID uuid.UUID
}

// Recorder publishes entries to the clients of their user. The zero value, and a nil one, record nothing.
type Recorder struct {
	sink  ports.EventSink
	clock ports.Clock
}

// New returns a recorder that publishes to sink, stamping entries with clock.
func New(sink ports.EventSink, clock ports.Clock) *Recorder {
	return &Recorder{sink: sink, clock: clock}
}

// Record publishes one entry about target.
func (r *Recorder) Record(target Target, level Level, step Step, params map[string]string) {
	if r == nil || r.sink == nil {
		return
	}
	entry := Entry{WorkspaceID: target.WorkspaceID, SiteID: target.SiteID, Time: r.clock.Now().UTC(), Level: level, Step: step, Params: params}
	r.sink.Publish(ports.Event{Type: EventType, UserID: target.UserID, Data: entry})
}
