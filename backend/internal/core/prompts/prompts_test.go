package prompts_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/core/prompts"
)

// sink hands the events published to it to the test, in order.
type sink struct{ events chan ports.Event }

// newSink returns a sink with room for the events of a test.
func newSink() *sink { return &sink{events: make(chan ports.Event, 16)} }

// Publish implements [ports.EventSink].
func (s *sink) Publish(e ports.Event) { s.events <- e }

// next returns the next event, failing the test when none comes.
func (s *sink) next(t *testing.T) ports.Event {
	t.Helper()
	select {
	case e := <-s.events:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no event published")
		return ports.Event{}
	}
}

// result is what Ask returned.
type result struct {
	answers []string
	err     error
}

// ask runs Ask in the background and returns where its result arrives.
func ask(ctx context.Context, b *prompts.Broker, target prompts.Target, challenge prompts.Challenge) <-chan result {
	done := make(chan result, 1)
	go func() {
		answers, err := b.Ask(ctx, target, challenge)
		done <- result{answers, err}
	}()
	return done
}

// wait returns the result of Ask, failing the test when it does not return.
func wait(t *testing.T, done <-chan result) result {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("Ask did not return")
		return result{}
	}
}

// challenge is a two-question challenge, as a server asking for a password and a code would send.
var challenge = prompts.Challenge{Name: "Login", Instruction: "Two steps", Questions: []prompts.Question{{Text: "Password:"}, {Text: "Code:", Echo: true}}}

// TestBroker_Answer covers answering a prompt (ADR-0016).
//
// Use cases:
//   - Ask publishes connection.prompt to the user who connects, with its id, site and challenge, and returns the
//     answers given through Answer
//   - once answered, connection.prompt_closed tells every client of the user that the prompt no longer waits, and the
//     prompt cannot be answered again
//   - another user cannot answer it, nor answer with fewer answers than questions or with a too long answer; the prompt keeps waiting
func TestBroker_Answer(t *testing.T) {
	events := newSink()
	b := prompts.New(events, time.Minute)
	target := prompts.Target{UserID: uuid.New(), WorkspaceID: uuid.New(), SiteID: uuid.New()}
	done := ask(context.Background(), b, target, challenge)

	e := events.next(t)
	p, ok := e.Data.(prompts.Prompt)
	if e.Type != prompts.PromptEvent || e.UserID != target.UserID || !ok {
		t.Fatalf("event = %+v", e)
	}
	if p.WorkspaceID != target.WorkspaceID || p.SiteID != target.SiteID || p.Challenge.Name != "Login" || len(p.Challenge.Questions) != 2 {
		t.Fatalf("prompt = %+v", p)
	}
	if err := b.Answer(uuid.New(), p.ID, []string{"a", "b"}); !errors.Is(err, prompts.ErrNotFound) {
		t.Errorf("answer by another user = %v; want ErrNotFound", err)
	}
	if err := b.Answer(target.UserID, p.ID, []string{"a"}); !errors.Is(err, prompts.ErrInvalidAnswers) {
		t.Errorf("one answer = %v; want ErrInvalidAnswers", err)
	}
	if err := b.Answer(target.UserID, p.ID, []string{"a", strings.Repeat("x", prompts.MaxAnswerLength+1)}); !errors.Is(err, prompts.ErrInvalidAnswers) {
		t.Errorf("too long answer = %v; want ErrInvalidAnswers", err)
	}
	if err := b.Answer(target.UserID, p.ID, []string{"secret", "123456"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	r := wait(t, done)
	if r.err != nil || len(r.answers) != 2 || r.answers[0] != "secret" || r.answers[1] != "123456" {
		t.Errorf("Ask = %+v", r)
	}
	closed := events.next(t)
	if c, ok := closed.Data.(prompts.Closed); closed.Type != prompts.ClosedEvent || closed.UserID != target.UserID || !ok || c.ID != p.ID {
		t.Errorf("closed = %+v", closed)
	}
	if err := b.Answer(target.UserID, p.ID, []string{"a", "b"}); !errors.Is(err, prompts.ErrNotFound) {
		t.Errorf("second answer = %v; want ErrNotFound", err)
	}
}

// TestBroker_Ask_Ends covers the ways a prompt ends without answers (ADR-0016).
//
// Use cases:
//   - Cancel by the user fails Ask with ErrCanceled; a cancel by another user or of an unknown prompt is ErrNotFound
//   - nobody answering in time fails Ask with ErrExpired
//   - the connection's context ending fails Ask with its error
//   - every ending publishes connection.prompt_closed
//   - a challenge without questions, such as a banner, is acknowledged at once without asking
//   - a zero wait means the default one
//   - a user with MaxWaiting prompts waiting cannot open another, while other users still can
func TestBroker_Ask_Ends(t *testing.T) {
	target := prompts.Target{UserID: uuid.New()}

	t.Run("canceled", func(t *testing.T) {
		events := newSink()
		b := prompts.New(events, time.Minute)
		done := ask(context.Background(), b, target, challenge)
		p := events.next(t).Data.(prompts.Prompt)
		if err := b.Cancel(uuid.New(), p.ID); !errors.Is(err, prompts.ErrNotFound) {
			t.Errorf("cancel by another user = %v; want ErrNotFound", err)
		}
		if err := b.Cancel(target.UserID, p.ID); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		if r := wait(t, done); !errors.Is(r.err, prompts.ErrCanceled) {
			t.Errorf("Ask = %+v; want ErrCanceled", r)
		}
		if e := events.next(t); e.Type != prompts.ClosedEvent {
			t.Errorf("event = %+v", e)
		}
	})

	t.Run("expired", func(t *testing.T) {
		events := newSink()
		b := prompts.New(events, 10*time.Millisecond)
		if r := wait(t, ask(context.Background(), b, target, challenge)); !errors.Is(r.err, prompts.ErrExpired) {
			t.Errorf("Ask = %+v; want ErrExpired", r)
		}
		events.next(t)
		if e := events.next(t); e.Type != prompts.ClosedEvent {
			t.Errorf("event = %+v", e)
		}
	})

	t.Run("context ended", func(t *testing.T) {
		events := newSink()
		b := prompts.New(events, time.Minute)
		ctx, cancel := context.WithCancel(context.Background())
		done := ask(ctx, b, target, challenge)
		events.next(t)
		cancel()
		if r := wait(t, done); !errors.Is(r.err, context.Canceled) {
			t.Errorf("Ask = %+v; want context.Canceled", r)
		}
	})

	t.Run("too many waiting", func(t *testing.T) {
		events := make(chan ports.Event, 4*prompts.MaxWaiting)
		b := prompts.New(&sink{events: events}, time.Minute)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		for range prompts.MaxWaiting {
			ask(ctx, b, target, challenge)
			<-events
		}
		if _, err := b.Ask(ctx, target, challenge); !errors.Is(err, prompts.ErrTooMany) {
			t.Errorf("Ask over the limit = %v; want ErrTooMany", err)
		}
		other := prompts.Target{UserID: uuid.New()}
		done := ask(ctx, b, other, challenge)
		p := (<-events).Data.(prompts.Prompt)
		if err := b.Cancel(other.UserID, p.ID); err != nil {
			t.Fatal(err)
		}
		if r := wait(t, done); !errors.Is(r.err, prompts.ErrCanceled) {
			t.Errorf("another user = %+v; want their own prompt", r)
		}
	})

	t.Run("no questions", func(t *testing.T) {
		events := newSink()
		b := prompts.New(events, 0)
		answers, err := b.Ask(context.Background(), target, prompts.Challenge{Instruction: "Welcome"})
		if err != nil || len(answers) != 0 || len(events.events) != 0 {
			t.Errorf("Ask = %v, %v; %d events", answers, err, len(events.events))
		}
		if b.Wait() != prompts.DefaultWait {
			t.Errorf("Wait = %v; want the default", b.Wait())
		}
	})
}
