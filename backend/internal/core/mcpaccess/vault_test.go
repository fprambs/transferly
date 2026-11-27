package mcpaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// vaultFixture records generation-specific cleanup and can fail either preparation step.
type vaultFixture struct {
	unlockError  error
	refreshError error
	locked       chan string
	closed       chan uuid.UUID
}

// Unlock provides the configured password check result.
func (f *vaultFixture) Unlock(context.Context, uuid.UUID, string, string) error { return f.unlockError }

// Refresh provides the configured workspace key result.
func (f *vaultFixture) Refresh(context.Context, uuid.UUID, string) error { return f.refreshError }

// Lock records the session whose key was removed.
func (f *vaultFixture) Lock(session string) { f.locked <- session }

// CloseSession records the session whose prompted credentials were removed.
func (f *vaultFixture) CloseSession(session string) { f.locked <- session }

// CloseIsolation records the generation whose pools were closed.
func (f *vaultFixture) CloseIsolation(generation uuid.UUID) { f.closed <- generation }

// TestVaultAccess checks that failed unlocks publish no window and locking clears only that generation.
func TestVaultAccess(t *testing.T) {
	failure := errors.New("preparation failed")
	f := &vaultFixture{unlockError: failure, locked: make(chan string, 3), closed: make(chan uuid.UUID, 1)}
	registry := NewUnlocks(&unlockClock{}, 0, 0)
	access := VaultAccess{Windows: registry, Vault: f, Keys: f, Pools: f, Credentials: f}
	link, owner := uuid.New(), uuid.New()
	if window, err := access.Enable(t.Context(), t.Context(), link, owner, "password"); window != nil || err != failure {
		t.Fatal("password failure published a window")
	}
	f.unlockError = nil
	f.refreshError = failure
	if window, err := access.Enable(t.Context(), t.Context(), link, owner, "password"); window != nil || err != failure {
		t.Fatal("workspace key failure published a window")
	}
	failedSession := <-f.locked
	f.refreshError = nil
	window, err := access.Enable(t.Context(), t.Context(), link, owner, "password")
	if err != nil || window.ID().String() == failedSession {
		t.Fatalf("fresh unlock failed: %v", err)
	}
	registry.Lock(link)
	select {
	case generation := <-f.closed:
		if generation != window.ID() || <-f.locked != generation.String() || <-f.locked != generation.String() {
			t.Fatal("cleanup affected the wrong generation")
		}
	case <-time.After(time.Second):
		t.Fatal("generation did not clean up")
	}
}
