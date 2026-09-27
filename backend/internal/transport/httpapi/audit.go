package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/audit"
)

// record adds an event to the audit trail of a workspace. A failure to record is logged and never fails what happened.
func (s *server) record(ctx context.Context, actor, workspaceID uuid.UUID, action, target string, meta map[string]string) {
	err := s.deps.Audit.Record(ctx, audit.Event{WorkspaceID: workspaceID, ActorID: actor, Action: action, Target: target, IP: clientFrom(ctx).IP, Meta: meta})
	if err != nil {
		s.deps.Logger.WarnContext(ctx, "audit event not recorded", "action", action, "error", err)
	}
}

// memberEmail returns the email of a member of a team workspace, or an empty text when it cannot be found: the audit line is written
// with what is known, because looking the person up must not decide whether the change happens.
func (s *server) memberEmail(ctx context.Context, actor, workspaceID, userID uuid.UUID) string {
	members, err := s.deps.Workspaces.Members(ctx, actor, workspaceID)
	if err != nil {
		return ""
	}
	for _, m := range members {
		if m.UserID == userID {
			return m.Email
		}
	}
	return ""
}

// ListAudit returns the audit trail of a workspace to the members who manage it.
func (s *server) ListAudit(ctx context.Context, request ListAuditRequestObject) (ListAuditResponseObject, error) {
	p, _ := principalFrom(ctx)
	filter, limit := audit.Filter{}, 0
	if request.Params.Action != nil {
		filter.Action = *request.Params.Action
	}
	if request.Params.Before != nil {
		filter.Before = *request.Params.Before
	}
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	events, err := s.deps.Audit.List(ctx, p.User.ID, request.WorkspaceId, filter, limit)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return ListAudit200JSONResponse(auditEvents(events)), nil
}

// auditEvents turns events into what the API answers with.
func auditEvents(events []audit.Event) []AuditEvent {
	out := make([]AuditEvent, 0, len(events))
	for _, e := range events {
		event := AuditEvent{Id: e.ID, Action: e.Action, Target: e.Target, Meta: e.Meta, At: e.At}
		if e.ActorEmail != "" {
			event.Actor = &e.ActorEmail
		}
		if e.IP != "" {
			event.Ip = &e.IP
		}
		out = append(out, event)
	}
	return out
}
