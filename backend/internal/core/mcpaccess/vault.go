package mcpaccess

import (
	"context"

	"github.com/google/uuid"
)

// Vault unlocks and clears credentials using the generation's private session identifier.
type Vault interface {
	Unlock(ctx context.Context, userID uuid.UUID, session, password string) error
	Lock(session string)
}

// WorkspaceKeys loads the team keys available to a user in an unlocked session.
type WorkspaceKeys interface {
	Refresh(ctx context.Context, userID uuid.UUID, session string) error
}

// Pools closes connections belonging to one unlock generation.
type Pools interface {
	CloseIsolation(uuid.UUID)
}

// SessionCredentials discards credentials prompted for an unlocked session.
type SessionCredentials interface {
	CloseSession(string)
}

// VaultAccess installs and removes the resources of an approved link's unlock window.
type VaultAccess struct {
	// Windows tracks the currently active generation for each link.
	Windows *Unlocks
	// Vault unlocks the personal key.
	Vault Vault
	// Keys loads current workspace keys.
	Keys WorkspaceKeys
	// Pools owns remote connections.
	Pools Pools
	// Credentials tracks prompted credentials.
	Credentials SessionCredentials
}

// Enable verifies the password before publishing a new window for the authenticated link owner.
// lifetime is the service lifetime. The caller must authorize the link before calling Enable.
func (a VaultAccess) Enable(ctx, lifetime context.Context, link, owner uuid.UUID, password string) (*Window, error) {
	generation := uuid.New()
	session := generation.String()
	if err := a.Vault.Unlock(ctx, owner, session, password); err != nil {
		return nil, err
	}
	if err := a.Keys.Refresh(ctx, owner, session); err != nil {
		a.Vault.Lock(session)
		return nil, err
	}
	window := a.Windows.open(lifetime, link, generation)
	context.AfterFunc(window.Context(), func() {
		a.Vault.Lock(session)
		a.Credentials.CloseSession(session)
		a.Pools.CloseIsolation(generation)
	})
	return window, nil
}
