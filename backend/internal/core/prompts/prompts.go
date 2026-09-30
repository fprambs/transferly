// Package prompts relays the questions a server asks while a connection signs in to the person who is connecting, and
// their answers back (ADR-0016): a one-time code, a password asked every time, a custom challenge. A question reaches
// the browser as an event; the answer comes back through the API while the handshake waits. Answers are handed to
// the connection and forgotten: they are never stored, logged or audited.
package prompts

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/ports"
)

const (
	// PromptEvent is the type of the event that asks the questions.
	PromptEvent = "connection.prompt"
	// ClosedEvent is the type of the event that says a prompt no longer waits, answered or not.
	ClosedEvent = "connection.prompt_closed"
	// DefaultWait is how long a prompt waits for its answers.
	DefaultWait = 3 * time.Minute
	// MaxAnswerLength is the longest answer taken, in bytes; no password or code comes near it.
	MaxAnswerLength = 1024
	// MaxWaiting is the most prompts that wait for one user at once: each holds a connection to a server open while it waits, so a
	// user opening many interactive connections from many tabs cannot pile them up (security review of phase 4).
	MaxWaiting = 16
)

var (
	// ErrNotFound means there is no waiting prompt with that id for that user.
	ErrNotFound = errors.New("prompt not found")
	// ErrInvalidAnswers means the answers do not match the questions one to one, or one is longer than [MaxAnswerLength].
	ErrInvalidAnswers = errors.New("one answer per question is required, each of at most 1024 bytes")
	// ErrCanceled means the person declined to answer.
	ErrCanceled = errors.New("prompt canceled")
	// ErrExpired means nobody answered in time.
	ErrExpired = errors.New("prompt expired")
	// ErrTooMany means [MaxWaiting] prompts already wait for the user.
	ErrTooMany = errors.New("too many prompts wait for an answer")
)

// Question is one thing the server asks.
type Question struct {
	// Text is the question as the server words it, such as "Verification code:".
	Text string
	// Echo tells whether the answer may be shown while typed; a password or a code is not.
	Echo bool
}

// Challenge is a set of questions the server asks at once.
type Challenge struct {
	// Name and Instruction are the server's title and explanation of the challenge; either may be empty.
	Name, Instruction string
	// Questions are answered in order.
	Questions []Question
}

// Target is the connection that asks, and the user who answers.
type Target struct {
	// UserID is the user who is connecting.
	UserID uuid.UUID
	// WorkspaceID and SiteID say which site, or quick connection, asks.
	WorkspaceID, SiteID uuid.UUID
}

// Prompt is the payload of a [PromptEvent].
type Prompt struct {
	// ID identifies the prompt when answering it.
	ID uuid.UUID
	// WorkspaceID and SiteID say which site asks.
	WorkspaceID, SiteID uuid.UUID
	// Challenge is what the server asks.
	Challenge Challenge
}

// Closed is the payload of a [ClosedEvent].
type Closed struct {
	// ID is the prompt that no longer waits.
	ID uuid.UUID
}

// reply is how a waiting prompt ends from the API: answers, or a cancel when answers is nil.
type reply struct {
	answers []string
}

// waiting is a prompt waiting for its answers.
type waiting struct {
	user      uuid.UUID
	questions int
	replies   chan reply
}

// Broker hands questions to their users and waits for the answers. It is safe for concurrent use.
type Broker struct {
	sink ports.EventSink
	wait time.Duration

	mu      sync.Mutex
	pending map[uuid.UUID]*waiting
}

// New returns a broker that publishes prompts to sink and waits up to wait for each; zero waits [DefaultWait].
func New(sink ports.EventSink, wait time.Duration) *Broker {
	if wait <= 0 {
		wait = DefaultWait
	}
	return &Broker{sink: sink, wait: wait, pending: map[uuid.UUID]*waiting{}}
}

// Wait is how long a prompt waits for its answers, for callers whose own timeout must leave room for a person.
func (b *Broker) Wait() time.Duration {
	return b.wait
}

// Ask shows challenge to the user of target and returns their answers, one per question. A challenge without
// questions is acknowledged at once. It fails with [ErrTooMany] when [MaxWaiting] prompts already wait for the user,
// [ErrCanceled] when the user declines, [ErrExpired] when nobody answers in time, and the context error when ctx ends first.
func (b *Broker) Ask(ctx context.Context, target Target, challenge Challenge) ([]string, error) {
	if len(challenge.Questions) == 0 {
		return []string{}, nil
	}
	id := uuid.New()
	w := &waiting{user: target.UserID, questions: len(challenge.Questions), replies: make(chan reply, 1)}
	b.mu.Lock()
	if b.waitingFor(target.UserID) >= MaxWaiting {
		b.mu.Unlock()
		return nil, ErrTooMany
	}
	b.pending[id] = w
	b.mu.Unlock()
	defer b.close(id, target.UserID)

	b.sink.Publish(ports.Event{Type: PromptEvent, UserID: target.UserID, Data: Prompt{
		ID: id, WorkspaceID: target.WorkspaceID, SiteID: target.SiteID, Challenge: challenge,
	}})
	timer := time.NewTimer(b.wait)
	defer timer.Stop()
	select {
	case r := <-w.replies:
		if r.answers == nil {
			return nil, ErrCanceled
		}
		return r.answers, nil
	case <-timer.C:
		return nil, ErrExpired
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// waitingFor counts the prompts that wait for user; the caller holds the lock.
func (b *Broker) waitingFor(user uuid.UUID) int {
	n := 0
	for _, w := range b.pending {
		if w.user == user {
			n++
		}
	}
	return n
}

// close forgets prompt id and tells every client of user that it no longer waits.
func (b *Broker) close(id, user uuid.UUID) {
	b.mu.Lock()
	delete(b.pending, id)
	b.mu.Unlock()
	b.sink.Publish(ports.Event{Type: ClosedEvent, UserID: user, Data: Closed{ID: id}})
}

// take removes and returns the prompt id of user, if it waits.
func (b *Broker) take(user, id uuid.UUID, check func(*waiting) error) (*waiting, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w, ok := b.pending[id]
	if !ok || w.user != user {
		return nil, ErrNotFound
	}
	if err := check(w); err != nil {
		return nil, err
	}
	delete(b.pending, id)
	return w, nil
}

// Answer hands answers, one per question, to the prompt id of user; answers that do not fit are [ErrInvalidAnswers] and
// the prompt keeps waiting. A prompt of another user is [ErrNotFound].
func (b *Broker) Answer(user, id uuid.UUID, answers []string) error {
	w, err := b.take(user, id, func(w *waiting) error {
		if len(answers) != w.questions {
			return ErrInvalidAnswers
		}
		for _, answer := range answers {
			if len(answer) > MaxAnswerLength {
				return ErrInvalidAnswers
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	w.replies <- reply{answers: append([]string{}, answers...)}
	return nil
}

// Cancel declines the prompt id of user, which fails the connection that asked.
func (b *Broker) Cancel(user, id uuid.UUID) error {
	w, err := b.take(user, id, func(*waiting) error { return nil })
	if err != nil {
		return err
	}
	w.replies <- reply{}
	return nil
}
