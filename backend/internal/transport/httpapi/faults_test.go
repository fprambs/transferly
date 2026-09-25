package httpapi_test

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

// errInjected is the failure produced by [flakyStore].
var errInjected = errors.New("injected failure")

// flakyStore is the real store with selected operations made to fail.
type flakyStore struct {
	*postgres.Store
	failCreateSession bool
	failPreferences   bool
	failLookupAfter   int64
	lookups           atomic.Int64
}

// CreateSession fails when configured to.
func (f *flakyStore) CreateSession(ctx context.Context, s auth.Session) error {
	if f.failCreateSession {
		return errInjected
	}
	return f.Store.CreateSession(ctx, s)
}

// UpdatePreferences fails when configured to.
func (f *flakyStore) UpdatePreferences(ctx context.Context, id uuid.UUID, locale, timezone string) error {
	if f.failPreferences {
		return errInjected
	}
	return f.Store.UpdatePreferences(ctx, id, locale, timezone)
}

// SessionByTokenHash fails once the configured number of lookups has been served.
func (f *flakyStore) SessionByTokenHash(ctx context.Context, hash []byte) (auth.Session, error) {
	if f.failLookupAfter > 0 && f.lookups.Add(1) > f.failLookupAfter {
		return auth.Session{}, errInjected
	}
	return f.Store.SessionByTokenHash(ctx, hash)
}

// withFlaky replaces the auth service by one over a store with injected failures.
func withFlaky(configure func(*flakyStore)) func(*httpapi.Deps) {
	return func(d *httpapi.Deps) {
		flaky := &flakyStore{Store: d.Readiness.(*postgres.Store)}
		configure(flaky)
		hasher := auth.NewHasher(vault.NewKDF(2), vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}, rand.Reader)
		d.Auth = auth.NewService(flaky, flaky, hasher, realClock{}, rand.Reader, auth.Config{IdleTimeout: time.Hour, AbsoluteTimeout: 8 * time.Hour})
	}
}

// realClock is the wall clock as a [ports.Clock].
type realClock struct{}

// Now returns the current instant in UTC.
func (realClock) Now() time.Time { return time.Now().UTC() }

// TestHandlerFailures verifies that internal failures inside the handlers are 500 problems without details.
//
// Scenarios:
//   - a session that cannot be stored fails setup and login
//   - a session lookup that fails while signing out, after authentication succeeded, fails logout
//   - a preference update that cannot be stored fails the update
func TestHandlerFailures(t *testing.T) {
	broken := newAPI(t, withFlaky(func(f *flakyStore) { f.failCreateSession = true }))
	expectProblem(t, broken.do(request{method: http.MethodPost, path: "/api/v1/setup", body: map[string]string{"email": adminEmail, "password": adminPassword}}), 500, "internal_error")
	expectProblem(t, broken.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": adminEmail, "password": adminPassword}}), 500, "internal_error")

	logout := newAPI(t, withFlaky(func(f *flakyStore) { f.failLookupAfter = 1 }))
	cookie, csrf := logout.setup()
	expectProblem(t, logout.do(request{method: http.MethodPost, path: "/api/v1/auth/logout", cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf}}), 500, "internal_error")

	prefs := newAPI(t, withFlaky(func(f *flakyStore) { f.failPreferences = true }))
	cookie, csrf = prefs.setup()
	expectProblem(t, prefs.do(request{method: http.MethodPatch, path: "/api/v1/me", cookie: cookie, body: map[string]string{"locale": "en"}, headers: map[string]string{"X-CSRF-Token": csrf}}), 500, "internal_error")
}

// TestClientInfoIsSanitized verifies what is recorded about the client of a session.
//
// Scenarios:
//   - an oversized User-Agent is cut to 256 characters
//   - when the client address cannot be determined it is recorded as unknown, never as an attacker-chosen value
func TestClientInfoIsSanitized(t *testing.T) {
	a := newAPI(t, func(d *httpapi.Deps) { d.TrustedProxies = []string{"127.0.0.0/8"} })
	a.do(request{method: http.MethodPost, path: "/api/v1/setup", body: map[string]string{"email": adminEmail, "password": adminPassword},
		headers: map[string]string{"User-Agent": strings.Repeat("a", 600), "X-Forwarded-For": "not-an-ip"}})

	var agent, ip string
	if err := a.store.DB().QueryRow(t.Context(), "SELECT user_agent, ip FROM user_sessions").Scan(&agent, &ip); err != nil {
		t.Fatal(err)
	}
	if len(agent) != 256 {
		t.Errorf("stored User-Agent has %d characters, want 256", len(agent))
	}
	if ip != "unknown" {
		t.Errorf("stored address = %q, want unknown", ip)
	}
}

// failingWorkspaces is a workspace store whose selected operations fail.
type failingWorkspaces struct {
	*postgres.Store
	failList   bool
	failCreate bool
}

// WorkspacesForUser fails when configured to.
func (f *failingWorkspaces) WorkspacesForUser(ctx context.Context, id uuid.UUID) ([]workspace.Summary, error) {
	if f.failList {
		return nil, errInjected
	}
	return f.Store.WorkspacesForUser(ctx, id)
}

// CreateWorkspace fails when configured to.
func (f *failingWorkspaces) CreateWorkspace(ctx context.Context, w workspace.Workspace) error {
	if f.failCreate {
		return errInjected
	}
	return f.Store.CreateWorkspace(ctx, w)
}

// withFailingWorkspaces replaces the workspace service by one over a store with injected failures.
func withFailingWorkspaces(configure func(*failingWorkspaces)) func(*httpapi.Deps) {
	return func(d *httpapi.Deps) {
		f := &failingWorkspaces{Store: d.Readiness.(*postgres.Store)}
		configure(f)
		d.Workspaces = workspace.NewService(f, realClock{}, nil)
	}
}

// TestWorkspaceFailures verifies that internal failures around workspaces are 500 problems.
//
// Scenarios:
//   - a personal workspace that cannot be created fails setup and login
//   - a workspace listing that fails fails the list and get endpoints
func TestWorkspaceFailures(t *testing.T) {
	create := newAPI(t, withFailingWorkspaces(func(f *failingWorkspaces) { f.failCreate = true }))
	expectProblem(t, create.do(request{method: http.MethodPost, path: "/api/v1/setup", body: map[string]string{"email": adminEmail, "password": adminPassword}}), 500, "internal_error")
	if _, err := create.auth.Register(t.Context(), "u@example.com", adminPassword); err != nil {
		t.Fatal(err)
	}
	expectProblem(t, create.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": "u@example.com", "password": adminPassword}}), 500, "internal_error")

	var toggle *failingWorkspaces
	list := newAPI(t, withFailingWorkspaces(func(f *failingWorkspaces) { toggle = f }))
	cookie, _ := list.setup()
	toggle.failList = true
	expectProblem(t, list.do(request{method: http.MethodGet, path: "/api/v1/workspaces", cookie: cookie}), 500, "internal_error")
	expectProblem(t, list.do(request{method: http.MethodGet, path: "/api/v1/workspaces/" + uuid.NewString(), cookie: cookie}), 500, "internal_error")
}
