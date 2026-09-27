package httpapi

import (
	"context"
	"net/http"
	"net/netip"
	"strconv"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/audit"
	"github.com/fprambs/sentinel/internal/core/auth"
)

// recordInstance adds an event of an administrator to the audit trail of the instance, which belongs to no workspace.
func (s *server) recordInstance(ctx context.Context, actor auth.User, action, target string, meta map[string]string) {
	s.record(ctx, actor.ID, uuid.Nil, action, target, meta)
}

// prefixStrings writes networks as text.
func prefixStrings(prefixes []netip.Prefix) []string {
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, p.String())
	}
	return out
}

// ListAdminUsers lists the accounts of the instance to its administrators.
func (s *server) ListAdminUsers(ctx context.Context, _ ListAdminUsersRequestObject) (ListAdminUsersResponseObject, error) {
	p, _ := principalFrom(ctx)
	accounts, err := s.deps.Admin.Users(ctx, p.User)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(ListAdminUsers200JSONResponse, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, AdminUser{Id: a.ID, Email: a.Email, Role: AdminUserRole(a.Role), Disabled: a.Disabled, SecondFactor: a.SecondFactor, CreatedAt: a.CreatedAt})
	}
	return out, nil
}

// UpdateAdminUser disables, enables or changes the role of an account.
func (s *server) UpdateAdminUser(ctx context.Context, request UpdateAdminUserRequestObject) (UpdateAdminUserResponseObject, error) {
	p, _ := principalFrom(ctx)
	body := request.Body
	if body.Disabled == nil && body.Role == nil {
		return domainProblem{status: http.StatusUnprocessableEntity, code: "empty_patch"}, nil
	}
	if body.Role != nil {
		account, err := s.deps.Admin.SetRole(ctx, p.User, request.UserId, auth.Role(*body.Role))
		if problem, ok := asProblem(err); ok {
			return problem, nil
		}
		if err != nil {
			return nil, err
		}
		s.recordInstance(ctx, p.User, audit.AdminRoleChanged, account.Email, map[string]string{"role": string(account.Role)})
	}
	if body.Disabled != nil {
		account, err := s.deps.Admin.SetDisabled(ctx, p.User, request.UserId, *body.Disabled)
		if problem, ok := asProblem(err); ok {
			return problem, nil
		}
		if err != nil {
			return nil, err
		}
		action := audit.AdminUserEnabled
		if account.Disabled {
			action = audit.AdminUserDisabled
		}
		s.recordInstance(ctx, p.User, action, account.Email, nil)
	}
	return UpdateAdminUser204Response{}, nil
}

// ResetSecondFactor removes the second factor of an account whose owner lost it.
func (s *server) ResetSecondFactor(ctx context.Context, request ResetSecondFactorRequestObject) (ResetSecondFactorResponseObject, error) {
	p, _ := principalFrom(ctx)
	account, err := s.deps.Admin.ResetSecondFactor(ctx, p.User, request.UserId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.recordInstance(ctx, p.User, audit.AdminSecondFactorReset, account.Email, nil)
	return ResetSecondFactor204Response{}, nil
}

// GetNetworks returns the private networks saved sites may reach.
func (s *server) GetNetworks(ctx context.Context, _ GetNetworksRequestObject) (GetNetworksResponseObject, error) {
	p, _ := principalFrom(ctx)
	networks, err := s.deps.Admin.Networks(ctx, p.User)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return GetNetworks200JSONResponse{Fixed: prefixStrings(networks.Fixed), Custom: prefixStrings(networks.Custom)}, nil
}

// SetNetworks replaces the private networks an administrator allows.
func (s *server) SetNetworks(ctx context.Context, request SetNetworksRequestObject) (SetNetworksResponseObject, error) {
	p, _ := principalFrom(ctx)
	networks, err := s.deps.Admin.SetNetworks(ctx, p.User, request.Body.Networks)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.recordInstance(ctx, p.User, audit.AdminNetworksChanged, "", map[string]string{"count": strconv.Itoa(len(networks.Custom))})
	return SetNetworks200JSONResponse{Fixed: prefixStrings(networks.Fixed), Custom: prefixStrings(networks.Custom)}, nil
}

// ListAdminAudit returns the audit trail of the instance to its administrators.
func (s *server) ListAdminAudit(ctx context.Context, request ListAdminAuditRequestObject) (ListAdminAuditResponseObject, error) {
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
	events, err := s.deps.Audit.ListInstance(ctx, p.User, filter, limit)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return ListAdminAudit200JSONResponse(auditEvents(events)), nil
}
