package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/audit"
	"github.com/fprambs/sentinel/internal/core/invite"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// invitationDTO converts an invitation to its public representation.
func invitationDTO(i invite.Invitation) Invitation {
	out := Invitation{Id: i.ID, Email: i.Email, Role: InvitationRole(i.Role), CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt}
	if i.InvitedByEmail != "" {
		out.InvitedBy = &i.InvitedByEmail
	}
	return out
}

// ListInvitations lists the invitations waiting in a team workspace.
func (s *server) ListInvitations(ctx context.Context, request ListInvitationsRequestObject) (ListInvitationsResponseObject, error) {
	p, _ := principalFrom(ctx)
	list, err := s.deps.Invites.List(ctx, p.User.ID, request.WorkspaceId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(ListInvitations200JSONResponse, 0, len(list))
	for _, i := range list {
		out = append(out, invitationDTO(i))
	}
	return out, nil
}

// CreateInvitation invites an email address and returns the token once.
func (s *server) CreateInvitation(ctx context.Context, request CreateInvitationRequestObject) (CreateInvitationResponseObject, error) {
	p, _ := principalFrom(ctx)
	created, token, err := s.deps.Invites.Create(ctx, p.User.ID, request.WorkspaceId, request.Body.Email, workspace.Role(request.Body.Role))
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.InvitationCreated, created.Email, map[string]string{"role": string(created.Role)})
	return CreateInvitation201JSONResponse{Invitation: invitationDTO(created), Token: token}, nil
}

// RevokeInvitation cancels an invitation.
func (s *server) RevokeInvitation(ctx context.Context, request RevokeInvitationRequestObject) (RevokeInvitationResponseObject, error) {
	p, _ := principalFrom(ctx)
	email := s.invitationEmail(ctx, p.User.ID, request.WorkspaceId, request.InvitationId)
	err := s.deps.Invites.Revoke(ctx, p.User.ID, request.WorkspaceId, request.InvitationId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.InvitationRevoked, email, nil)
	return RevokeInvitation204Response{}, nil
}

// invitationLimited reports whether the client address has used up its invitation attempts, and how long to wait.
func (s *server) invitationLimited(ctx context.Context) (bool, RateLimitedApplicationProblemPlusJSONResponse) {
	ok, wait := s.deps.Limits.Invitations.Allow("invite:" + clientFrom(ctx).IP)
	return !ok, RateLimitedApplicationProblemPlusJSONResponse{
		Body:    problemBody(429, "rate_limited", nil),
		Headers: RateLimitedResponseHeaders{RetryAfter: retryAfter(wait)},
	}
}

// PreviewInvitation tells the holder of a token what it offers.
func (s *server) PreviewInvitation(ctx context.Context, request PreviewInvitationRequestObject) (PreviewInvitationResponseObject, error) {
	if limited, answer := s.invitationLimited(ctx); limited {
		return PreviewInvitation429ApplicationProblemPlusJSONResponse{answer}, nil
	}
	preview, err := s.deps.Invites.Preview(ctx, request.Body.Token)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	out := PreviewInvitation200JSONResponse{
		WorkspaceName: preview.WorkspaceName, Email: preview.Email, Role: InvitationPreviewRole(preview.Role),
		ExpiresAt: preview.ExpiresAt, AccountExists: preview.AccountExists,
	}
	if preview.InvitedByEmail != "" {
		out.InvitedBy = &preview.InvitedByEmail
	}
	return out, nil
}

// AcceptInvitation makes the signed-in user a member.
func (s *server) AcceptInvitation(ctx context.Context, request AcceptInvitationRequestObject) (AcceptInvitationResponseObject, error) {
	if limited, answer := s.invitationLimited(ctx); limited {
		return AcceptInvitation429ApplicationProblemPlusJSONResponse{answer}, nil
	}
	p, _ := principalFrom(ctx)
	w, err := s.deps.Invites.Accept(ctx, p.User, request.Body.Token)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, w.ID, audit.MemberJoined, p.User.Email, map[string]string{"role": string(w.Role)})
	return AcceptInvitation200JSONResponse(workspaceDTO(w)), nil
}

// JoinWithInvitation creates the invited account, makes it a member and signs it in.
func (s *server) JoinWithInvitation(ctx context.Context, request JoinWithInvitationRequestObject) (JoinWithInvitationResponseObject, error) {
	if limited, answer := s.invitationLimited(ctx); limited {
		return JoinWithInvitation429ApplicationProblemPlusJSONResponse{answer}, nil
	}
	user, joined, err := s.deps.Invites.Join(ctx, request.Body.Token, request.Body.Password)
	if code, ok := isValidation(err); ok {
		return JoinWithInvitation422ApplicationProblemPlusJSONResponse(problemBody(422, code, nil)), nil
	}
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, user.ID, joined.ID, audit.MemberJoined, user.Email, map[string]string{"role": string(joined.Role)})
	if _, err := s.deps.Workspaces.EnsurePersonal(ctx, user.ID); err != nil {
		return nil, err
	}
	token, principal, err := s.deps.Auth.StartSession(ctx, user, clientFrom(ctx))
	if err != nil {
		return nil, err
	}
	cookie := newSessionCookie(token, s.deps.Cookie)
	return JoinWithInvitation201JSONResponse{Body: s.sessionInfo(principal), Headers: JoinWithInvitation201ResponseHeaders{SetCookie: &cookie}}, nil
}

// invitationEmail returns the address an invitation was for, or an empty text when it cannot be found: the audit line is written with
// what is known.
func (s *server) invitationEmail(ctx context.Context, actor, workspaceID, id uuid.UUID) string {
	list, err := s.deps.Invites.List(ctx, actor, workspaceID)
	if err != nil {
		return ""
	}
	for _, i := range list {
		if i.ID == id {
			return i.Email
		}
	}
	return ""
}
