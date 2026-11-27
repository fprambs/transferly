// Package mcpaccess defines the authorization boundary for saved sites exposed to MCP clients.
package mcpaccess

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/workspace"
)

// ErrUnavailable represents a target the caller cannot discover or use.
var ErrUnavailable = errors.New("MCP target unavailable")

// Target identifies a saved site within its workspace.
type Target struct {
	// WorkspaceID identifies the workspace that owns the site.
	WorkspaceID uuid.UUID
	// SiteID identifies the saved connection.
	SiteID uuid.UUID
}

// CheckBrowse verifies the user's current permission and the site's workspace association.
type CheckBrowse func(context.Context, uuid.UUID, Target, workspace.Action) error

// Scope holds the immutable set of targets approved for one user's MCP link.
type Scope struct {
	userID  uuid.UUID
	targets map[Target]struct{}
}

// NewScope snapshots the selected targets at approval time.
func NewScope(userID uuid.UUID, targets []Target) Scope {
	scope := Scope{userID: userID, targets: make(map[Target]struct{}, len(targets))}
	for _, target := range targets {
		scope.targets[target] = struct{}{}
	}
	return scope
}

// Authorize checks the approval snapshot before consulting current permissions.
// Missing and forbidden targets produce the same error and reveal no target details.
func (s Scope) Authorize(ctx context.Context, target Target, check CheckBrowse) error {
	if _, approved := s.targets[target]; !approved {
		return ErrUnavailable
	}
	if err := check(ctx, s.userID, target, workspace.ActionBrowse); err != nil {
		return ErrUnavailable
	}
	return nil
}
