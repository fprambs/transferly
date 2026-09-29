package httpapi

import "context"

// AnswerPrompt hands the answers of the signed-in user to a connection that waits for them (ADR-0016).
func (s *server) AnswerPrompt(ctx context.Context, request AnswerPromptRequestObject) (AnswerPromptResponseObject, error) {
	p, _ := principalFrom(ctx)
	err := s.deps.Prompts.Answer(p.User.ID, request.PromptId, request.Body.Answers)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	return AnswerPrompt204Response{}, nil
}

// CancelPrompt declines a prompt of the signed-in user, failing the connection that asked.
func (s *server) CancelPrompt(ctx context.Context, request CancelPromptRequestObject) (CancelPromptResponseObject, error) {
	p, _ := principalFrom(ctx)
	if problem, ok := asProblem(s.deps.Prompts.Cancel(p.User.ID, request.PromptId)); ok {
		return problem, nil
	}
	return CancelPrompt204Response{}, nil
}
