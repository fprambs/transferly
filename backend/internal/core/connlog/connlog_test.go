package connlog_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/connlog"
	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/testutil"
)

// sink keeps the events published to it.
type sink struct{ events []ports.Event }

// Publish implements [ports.EventSink].
func (s *sink) Publish(e ports.Event) { s.events = append(s.events, e) }

// TestRecorder_Record covers publishing entries (ADR-0011).
//
// Use cases:
//   - an entry reaches the user of its target as a connection.log event, stamped with the clock in UTC, with its site,
//     level, step and parameters
//   - a nil recorder and one without a sink record nothing and do not fail
func TestRecorder_Record(t *testing.T) {
	events := &sink{}
	now := time.Date(2026, 9, 28, 9, 30, 0, 0, time.FixedZone("CLT", -3*3600))
	r := connlog.New(events, testutil.NewFakeClock(now))
	target := connlog.Target{UserID: uuid.New(), WorkspaceID: uuid.New(), SiteID: uuid.New()}
	r.Record(target, connlog.Warn, connlog.PlainText, map[string]string{"host": "h"})

	if len(events.events) != 1 {
		t.Fatalf("%d events; want 1", len(events.events))
	}
	e := events.events[0]
	entry, ok := e.Data.(connlog.Entry)
	if e.Type != connlog.EventType || e.UserID != target.UserID || !ok {
		t.Fatalf("event = %+v", e)
	}
	if entry.WorkspaceID != target.WorkspaceID || entry.SiteID != target.SiteID || entry.Level != connlog.Warn || entry.Step != connlog.PlainText ||
		entry.Params["host"] != "h" || !entry.Time.Equal(now) || entry.Time.Location() != time.UTC {
		t.Errorf("entry = %+v", entry)
	}

	var none *connlog.Recorder
	none.Record(target, connlog.Info, connlog.Connecting, nil)
	(&connlog.Recorder{}).Record(target, connlog.Info, connlog.Connecting, nil)
}
