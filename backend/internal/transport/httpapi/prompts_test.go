package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/prompts"
)

// promptMessage is the payload of a connection.prompt message.
type promptMessage struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
	SiteID      uuid.UUID `json:"siteId"`
	Name        string    `json:"name"`
	Instruction string    `json:"instruction"`
	Questions   []struct {
		Text string `json:"text"`
		Echo bool   `json:"echo"`
	} `json:"questions"`
}

// asked is what a background Ask returned.
type asked struct {
	answers []string
	err     error
}

// askInBackground asks target a two-question challenge and returns where the result arrives.
func askInBackground(a *api, target prompts.Target) <-chan asked {
	done := make(chan asked, 1)
	go func() {
		answers, err := a.prompt.Ask(context.Background(), target, prompts.Challenge{
			Name: "Login", Instruction: "Two steps", Questions: []prompts.Question{{Text: "Password:"}, {Text: "Code:", Echo: true}},
		})
		done <- asked{answers, err}
	}()
	return done
}

// promptPath is the path of a prompt.
func promptPath(id uuid.UUID) string { return "/api/v1/prompts/" + id.String() }

// TestPrompts covers answering and declining what a server asks while an interactive site connects (ADR-0016).
//
// Use cases:
//   - the questions reach the event stream of the user as connection.prompt, with the site, the challenge and whether each
//     answer may be shown
//   - answering hands the answers to the connection and closes the prompt on every client with connection.prompt_closed
//   - another account cannot answer or decline the prompt (404 prompt_not_found), nor can the user answer with a
//     different number of answers (422 invalid_answers); the prompt keeps waiting
//   - declining fails the connection with ErrCanceled, and a prompt that no longer waits is 404 prompt_not_found
func TestPrompts(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, token := a.setup()
	conn, _ := a.connectEvents(cookie)
	target := prompts.Target{UserID: a.userID(cookie), WorkspaceID: uuid.New(), SiteID: uuid.New()}
	strangerCookie, _ := a.signIn("stranger@example.com")
	strangerToken := csrf(a, strangerCookie)

	done := askInBackground(a, target)
	var p promptMessage
	if err := json.Unmarshal(conn.expect(t, prompts.PromptEvent).Data, &p); err != nil {
		t.Fatal(err)
	}
	if p.WorkspaceID != target.WorkspaceID || p.SiteID != target.SiteID || p.Name != "Login" || p.Instruction != "Two steps" ||
		len(p.Questions) != 2 || p.Questions[0].Text != "Password:" || p.Questions[0].Echo || !p.Questions[1].Echo {
		t.Fatalf("prompt = %+v", p)
	}
	answers := map[string][]string{"answers": {"secret", "123456"}}
	if r := a.send(http.MethodPost, promptPath(p.ID), answers, strangerCookie, strangerToken); r.Status != http.StatusNotFound {
		t.Errorf("stranger answer = %d %s", r.Status, r.Body)
	}
	if r := a.send(http.MethodDelete, promptPath(p.ID), nil, strangerCookie, strangerToken); r.Status != http.StatusNotFound {
		t.Errorf("stranger cancel = %d %s", r.Status, r.Body)
	}
	expectProblem(t, a.send(http.MethodPost, promptPath(p.ID), map[string][]string{"answers": {"one"}}, cookie, token), http.StatusUnprocessableEntity, "invalid_answers")
	if r := a.send(http.MethodPost, promptPath(p.ID), answers, cookie, token); r.Status != http.StatusNoContent {
		t.Fatalf("answer = %d %s", r.Status, r.Body)
	}
	if got := <-done; got.err != nil || len(got.answers) != 2 || got.answers[1] != "123456" {
		t.Errorf("Ask = %+v", got)
	}
	var closed struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(conn.expect(t, prompts.ClosedEvent).Data, &closed); err != nil || closed.ID != p.ID {
		t.Errorf("closed = %+v, %v", closed, err)
	}

	done = askInBackground(a, target)
	if err := json.Unmarshal(conn.expect(t, prompts.PromptEvent).Data, &p); err != nil {
		t.Fatal(err)
	}
	if r := a.send(http.MethodDelete, promptPath(p.ID), nil, cookie, token); r.Status != http.StatusNoContent {
		t.Fatalf("cancel = %d %s", r.Status, r.Body)
	}
	if got := <-done; !errors.Is(got.err, prompts.ErrCanceled) {
		t.Errorf("Ask = %+v; want ErrCanceled", got)
	}
	expectProblem(t, a.send(http.MethodDelete, promptPath(p.ID), nil, cookie, token), http.StatusNotFound, "prompt_not_found")
}
