package httpapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/events"
	"github.com/fprambs/transferly/internal/adapters/ratelimit"
	"github.com/fprambs/transferly/internal/adapters/remotefs/memfs"
	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/core/activity"
	"github.com/fprambs/transferly/internal/core/admin"
	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/backup"
	"github.com/fprambs/transferly/internal/core/bookmark"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/credentials"
	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/fzimport"
	"github.com/fprambs/transferly/internal/core/hostkey"
	"github.com/fprambs/transferly/internal/core/invite"
	"github.com/fprambs/transferly/internal/core/limits"
	"github.com/fprambs/transferly/internal/core/mfa"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/passkey"
	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/prompts"
	"github.com/fprambs/transferly/internal/core/quick"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/transfer"
	"github.com/fprambs/transferly/internal/core/vault"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/core/wskey"
	"github.com/fprambs/transferly/internal/testutil"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

const (
	adminEmail    = "admin@example.com"
	adminPassword = "a long enough password"
	localOrigin   = "http://localhost:8080"
)

var instanceKey = []byte("0123456789abcdef0123456789abcdef")

// api is a running router over a real database, with controllable time.
type api struct {
	t      *testing.T
	srv    *httptest.Server
	store  *postgres.Store
	clock  *testutil.FakeClock
	auth   *auth.Service
	hash   *auth.Hasher
	events *recordedEvents
	hub    *events.Hub
	policy *netguard.Policy
	pool   *connections.Manager
	prompt *prompts.Broker
}

// reply is a fully read HTTP response.
type reply struct {
	Status  int
	Header  http.Header
	Body    []byte
	Cookies []*http.Cookie
}

// newAPI starts the router with tight rate limits, adjusting the dependencies with tweaks.
func newAPI(t *testing.T, tweaks ...func(*httpapi.Deps)) *api {
	t.Helper()
	return newAPIWithTransfers(t, transfer.Config{EventEvery: time.Millisecond}, tweaks...)
}

// newAPIWithTransfers is newAPI with the configuration of the transfer service, for tests that need limits.
func newAPIWithTransfers(t *testing.T, transfers transfer.Config, tweaks ...func(*httpapi.Deps)) *api {
	t.Helper()
	dbURL := testutil.NewDatabase(t)
	if err := postgres.Migrate(t.Context(), dbURL); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(t.Context(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	hasher := auth.NewHasher(vault.NewKDF(2), vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}, rand.Reader)
	service := auth.NewService(store, store, hasher, clock, rand.Reader, auth.Config{IdleTimeout: 30 * time.Minute, AbsoluteTimeout: 8 * time.Hour})
	workspaces := workspace.NewService(store, clock, nil)
	vaults := newTestVault(store, clock)
	keys := wskey.NewService(store, vaults, workspaces, rand.Reader)
	passkeys, err := passkey.NewService(store, passkey.Config{RPID: "localhost", DisplayName: "Transferly", Origins: []string{localOrigin}}, clock, rand.Reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	auditTrail := audit.NewService(store, workspaces, clock)
	keys.OnRotate(func(ctx context.Context, workspaceID, actor uuid.UUID, version int) {
		_ = auditTrail.Record(ctx, audit.Event{WorkspaceID: workspaceID, ActorID: actor, Action: audit.KeyRotated, Meta: map[string]string{"version": strconv.Itoa(version)}})
	})
	policy := netguard.NewPolicy(nil, nil)
	sites := site.NewService(store, workspaces, keys, policy, clock, rand.Reader, nil)
	quickConnections := quick.NewService(workspaces, policy, sites, clock, nil)
	asked := credentials.New(clock, 8*time.Hour)
	hub := events.New(64)
	pool := connections.New(newMemConnector(clock, sites, quickConnections, asked), clock, connections.Config{Attempts: 1, OnChange: connections.PublishTo(hub)})
	recorded := &recordedEvents{}
	broker := prompts.New(hub, time.Minute)
	importer := fzimport.NewService(workspaces, sites)
	activityLog := activity.NewService(store, clock, 30*24*time.Hour, nil)
	if transfers.OnFinish == nil {
		transfers.OnFinish = func(t transfer.Transfer) {
			_ = activityLog.Record(context.Background(), activity.Entry{
				UserID: t.UserID, WorkspaceID: t.WorkspaceID, SiteID: t.SiteID, SiteName: t.SiteName, Kind: activity.Transfer,
				Path: t.Path, Detail: string(t.Direction) + ":" + string(t.Status), ErrorCode: t.ErrorCode,
			})
		}
	}
	invites := invite.NewService(store, workspaces, store, service, clock, rand.Reader, 0, nil)
	transferService := transfer.NewService(store, fanout{recorded, hub}, clock, transfers, nil)
	quotas := limits.NewService(store, transferService, limits.Limits{}, clock)
	sites.UseQuota(quotas)
	offered := connectors.NewService(connectors.Builtin(), store, clock)
	sites.UseOffering(offered)
	invites.UseQuota(quotas)
	transferService.UseQuota(quotas)
	deps := httpapi.Deps{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Readiness:     store,
		Auth:          service,
		Workspaces:    workspaces,
		Vault:         vaults,
		Keys:          keys,
		HostKeys:      hostkey.NewService(store, workspaces, clock),
		Bookmarks:     bookmark.NewService(store, store, workspaces, clock, nil),
		Connections:   pool,
		Credentials:   asked,
		Sites:         sites,
		Connectors:    offered,
		Files:         files.NewService(workspaces, pool),
		Transfers:     transferService,
		Activity:      activityLog,
		Import:        importer,
		Backup:        backup.NewService(workspaces, sites, importer, backup.NewSealer(vault.NewKDF(2), vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}, rand.Reader), clock),
		Audit:         auditTrail,
		Admin:         admin.NewService(store, policy, quotas, clock),
		Quotas:        quotas,
		MFA:           mfa.NewService(store, instanceKey, rand.Reader, clock),
		Passkeys:      passkeys,
		Invites:       invites,
		Quick:         quickConnections,
		Events:        hub,
		ConnectionLog: connlog.New(hub, clock),
		Prompts:       broker,
		WebSocket:     httpapi.WebSocketConfig{PingEvery: 50 * time.Millisecond, PingTimeout: time.Second, VerifyEvery: 20 * time.Millisecond},
		Limits: httpapi.Limits{
			SetupIP:       ratelimit.New(clock, ratelimit.Rule{Burst: 5, Every: time.Minute}, 100),
			LoginIP:       ratelimit.New(clock, ratelimit.Rule{Burst: 5, Every: 10 * time.Second}, 100),
			LoginAccount:  ratelimit.New(clock, ratelimit.Rule{Burst: 3, Every: 10 * time.Second}, 100),
			VaultAttempts: ratelimit.New(clock, ratelimit.Rule{Burst: 4, Every: 10 * time.Second}, 100),
			QuickConnect:  ratelimit.New(clock, ratelimit.Rule{Burst: 3, Every: 10 * time.Second}, 100),
			MFA:           ratelimit.New(clock, ratelimit.Rule{Burst: 6, Every: 10 * time.Second}, 100),
			Invitations:   ratelimit.New(clock, ratelimit.Rule{Burst: 20, Every: 10 * time.Second}, 100),
		},
		InstanceKey:    instanceKey,
		Cookie:         httpapi.CookieConfig{MaxAge: 8 * time.Hour},
		AllowedOrigins: []string{localOrigin},
	}
	for _, tweak := range tweaks {
		tweak(&deps)
	}
	srv := httptest.NewServer(httpapi.NewRouter(deps))
	t.Cleanup(srv.Close)
	return &api{t: t, srv: srv, store: store, clock: clock, auth: service, hash: hasher, events: recorded, hub: hub, policy: policy, pool: pool, prompt: broker}
}

// request describes one call.
type request struct {
	method, path string
	body         any
	headers      map[string]string
	cookie       *http.Cookie
}

// do sends a request and reads the whole reply.
func (a *api) do(r request) reply {
	a.t.Helper()
	var reader io.Reader
	if r.body != nil {
		switch b := r.body.(type) {
		case string:
			reader = strings.NewReader(b)
		default:
			raw, err := json.Marshal(b)
			if err != nil {
				a.t.Fatal(err)
			}
			reader = bytes.NewReader(raw)
		}
	}
	req, err := http.NewRequestWithContext(a.t.Context(), r.method, a.srv.URL+r.path, reader)
	if err != nil {
		a.t.Fatal(err)
	}
	if r.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	if r.cookie != nil {
		req.AddCookie(r.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		a.t.Fatal(err)
	}
	return reply{Status: resp.StatusCode, Header: resp.Header, Body: raw, Cookies: resp.Cookies()}
}

// problem decodes the reply as a problem document.
func (r reply) problem(t *testing.T) httpapi.Problem {
	t.Helper()
	var p httpapi.Problem
	if err := json.Unmarshal(r.Body, &p); err != nil {
		t.Fatalf("decoding problem from %q: %v", r.Body, err)
	}
	return p
}

// session decodes the reply as a session document.
func (r reply) session(t *testing.T) httpapi.SessionInfo {
	t.Helper()
	var s httpapi.SessionInfo
	if err := json.Unmarshal(r.Body, &s); err != nil {
		t.Fatalf("decoding session from %q: %v", r.Body, err)
	}
	return s
}

// sessionCookie returns the session cookie set by a reply, or nil.
func (r reply) sessionCookie() *http.Cookie {
	for _, c := range r.Cookies {
		if c.Name == "transferly_session" {
			return c
		}
	}
	return nil
}

// expectProblem fails unless the reply is a problem with the given status and stable code.
func expectProblem(t *testing.T, r reply, status int, code string) {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status = %d (%s), want %d %s", r.Status, r.Body, status, code)
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	if got := r.problem(t).Code; got != code {
		t.Errorf("code = %q, want %q", got, code)
	}
}

// setup creates the administrator and returns the session cookie and anti-CSRF token.
func (a *api) setup() (*http.Cookie, string) {
	a.t.Helper()
	r := a.do(request{method: http.MethodPost, path: "/api/v1/setup", body: map[string]string{"email": adminEmail, "password": adminPassword}})
	if r.Status != http.StatusCreated || r.sessionCookie() == nil {
		a.t.Fatalf("setup = %d %s", r.Status, r.Body)
	}
	return r.sessionCookie(), r.session(a.t).CsrfToken
}

// TestSetup verifies the first-run flow.
//
// Scenarios:
//   - setup is required until the first administrator exists
//   - setup creates an administrator, signs them in and sets a hardened cookie
//   - setup is refused once an account exists
//   - invalid emails and weak passwords are 422 with stable codes
//   - malformed JSON and a missing body are 400
func TestSetup(t *testing.T) {
	a := newAPI(t)

	status := a.do(request{method: http.MethodGet, path: "/api/v1/setup/status"})
	if status.Status != 200 || !strings.Contains(string(status.Body), `"required":true`) {
		t.Fatalf("status before setup = %d %s", status.Status, status.Body)
	}

	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/setup", body: map[string]string{"email": "nope", "password": adminPassword}}), 422, "invalid_email")
	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/setup", body: map[string]string{"email": adminEmail, "password": "short"}}), 422, "weak_password")
	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/setup", body: "{not json"}), 400, "invalid_request")
	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/setup"}), 400, "invalid_request")

	created := a.do(request{method: http.MethodPost, path: "/api/v1/setup", body: map[string]string{"email": "  Admin@Example.com ", "password": adminPassword}})
	if created.Status != http.StatusCreated {
		t.Fatalf("setup = %d %s", created.Status, created.Body)
	}
	info := created.session(t)
	if info.User.Email != adminEmail || info.User.Role != httpapi.UserRole("admin") || info.CsrfToken == "" {
		t.Errorf("session = %+v, want the normalized administrator and a CSRF token", info)
	}
	cookie := created.sessionCookie()
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Secure || cookie.MaxAge != 8*3600 {
		t.Errorf("cookie = %+v, want HttpOnly, SameSite=Lax, Path=/, not Secure over http, 8 hours", cookie)
	}
	if strings.Contains(string(created.Body), adminPassword) || strings.Contains(string(created.Body), "argon2") {
		t.Error("the response exposes the password or its hash")
	}

	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/setup", body: map[string]string{"email": "b@example.com", "password": adminPassword}}), 409, "setup_complete")
	if after := a.do(request{method: http.MethodGet, path: "/api/v1/setup/status"}); !strings.Contains(string(after.Body), `"required":false`) {
		t.Errorf("status after setup = %s", after.Body)
	}
}

// TestSessionLifecycle verifies sign-in, the current session and sign-out.
//
// Scenarios:
//   - a wrong password and an unknown email give the same 401
//   - a correct login sets a cookie and returns the same anti-CSRF token as the current session
//   - the current session needs the cookie: without it or with a forged one it is 401
//   - logout needs the anti-CSRF token: without it or with a wrong one it is 403 and keeps the session
//   - logout clears the cookie and the session stops working
func TestSessionLifecycle(t *testing.T) {
	a := newAPI(t)
	a.setup()

	wrong := a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": adminEmail, "password": "wrong password!!"}})
	unknown := a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": "nobody@example.com", "password": "wrong password!!"}})
	expectProblem(t, wrong, 401, "invalid_credentials")
	expectProblem(t, unknown, 401, "invalid_credentials")
	if string(wrong.Body) != string(unknown.Body) {
		t.Error("a wrong password and an unknown email are distinguishable")
	}

	login := a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": adminEmail, "password": adminPassword}})
	if login.Status != 200 || login.sessionCookie() == nil {
		t.Fatalf("login = %d %s", login.Status, login.Body)
	}
	cookie, csrf := login.sessionCookie(), login.session(t).CsrfToken

	me := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie})
	if me.Status != 200 || me.session(t).CsrfToken != csrf || me.session(t).User.Email != adminEmail {
		t.Errorf("me = %d %s, want the same session", me.Status, me.Body)
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me"}), 401, "unauthenticated")
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: &http.Cookie{Name: "transferly_session", Value: "forged"}}), 401, "unauthenticated")
	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/auth/logout"}), 401, "unauthenticated")

	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/auth/logout", cookie: cookie}), 403, "csrf_invalid")
	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/auth/logout", cookie: cookie, headers: map[string]string{"X-CSRF-Token": "wrong"}}), 403, "csrf_invalid")
	if still := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}); still.Status != 200 {
		t.Error("a rejected logout ended the session")
	}

	out := a.do(request{method: http.MethodPost, path: "/api/v1/auth/logout", cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf}})
	if out.Status != 204 || out.sessionCookie() == nil || out.sessionCookie().MaxAge != -1 {
		t.Errorf("logout = %d, cookie %+v, want 204 and a cleared cookie", out.Status, out.sessionCookie())
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}), 401, "unauthenticated")
}

// TestCSRFTokensAreBoundToTheirSession verifies that a token from one session is useless in another.
func TestCSRFTokensAreBoundToTheirSession(t *testing.T) {
	a := newAPI(t)
	first, firstToken := a.setup()
	second := a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": adminEmail, "password": adminPassword}})
	secondToken := second.session(t).CsrfToken
	if firstToken == secondToken {
		t.Fatal("two sessions share an anti-CSRF token")
	}

	expectProblem(t, a.do(request{method: http.MethodPatch, path: "/api/v1/me", cookie: first, body: map[string]string{"locale": "en"}, headers: map[string]string{"X-CSRF-Token": secondToken}}), 403, "csrf_invalid")
	if ok := a.do(request{method: http.MethodPatch, path: "/api/v1/me", cookie: first, body: map[string]string{"locale": "en"}, headers: map[string]string{"X-CSRF-Token": firstToken}}); ok.Status != 200 {
		t.Errorf("own token = %d %s, want 200", ok.Status, ok.Body)
	}
}

// TestUpdateMe verifies preference updates.
//
// Scenarios:
//   - a valid language, zone, theme and double click action are stored and returned, the language canonicalized, and a new user has the
//     default theme and downloads on double click
//   - invalid languages, zones, themes and double click actions are 422 with stable codes
//   - a missing or malformed body is 400
func TestUpdateMe(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	patch := func(body any) reply {
		return a.do(request{method: http.MethodPatch, path: "/api/v1/me", cookie: cookie, body: body, headers: map[string]string{"X-CSRF-Token": csrf}})
	}

	if first := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}); first.session(t).User.Theme != "transferly-dark" || first.session(t).User.FileDoubleClick != httpapi.UserFileDoubleClickDownload {
		t.Errorf("the default theme and double click = %+v", first.session(t).User)
	}
	ok := patch(map[string]string{"locale": "es-cl", "timezone": "America/Santiago", "theme": "tokyo-night", "fileDoubleClick": "edit"})
	var user httpapi.User
	if err := json.Unmarshal(ok.Body, &user); err != nil || ok.Status != 200 || user.Locale != "es-CL" || user.Timezone != "America/Santiago" || user.Theme != "tokyo-night" || user.FileDoubleClick != httpapi.UserFileDoubleClickEdit {
		t.Fatalf("update = %d %s", ok.Status, ok.Body)
	}
	if me := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}); me.session(t).User.Timezone != "America/Santiago" || me.session(t).User.Theme != "tokyo-night" || me.session(t).User.FileDoubleClick != httpapi.UserFileDoubleClickEdit {
		t.Error("the update was not persisted")
	}
	expectProblem(t, patch(map[string]string{"locale": "not a tag!"}), 422, "invalid_locale")
	expectProblem(t, patch(map[string]string{"timezone": "Mars/Olympus"}), 422, "invalid_timezone")
	expectProblem(t, patch(map[string]string{"theme": "Not A Theme"}), 422, "invalid_theme")
	expectProblem(t, patch(map[string]string{"fileDoubleClick": "open"}), 422, "invalid_file_double_click")
	expectProblem(t, patch("{oops"), 400, "invalid_request")
	expectProblem(t, a.do(request{method: http.MethodPatch, path: "/api/v1/me", cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf}}), 400, "invalid_request")
}

// TestOriginGuard verifies the defenses against cross-site requests.
//
// Scenarios:
//   - state-changing requests from another origin or marked cross-site or same-site by Fetch Metadata are 403
//   - the configured origin, same-origin requests and clients that send neither header are accepted
//   - safe methods are never blocked
func TestOriginGuard(t *testing.T) {
	a := newAPI(t)
	attempts := 0
	login := func(headers map[string]string) reply {
		attempts++
		email := "guest" + strings.Repeat("x", attempts) + "@example.com"
		return a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": email, "password": "some password here"}, headers: headers})
	}

	for name, headers := range map[string]map[string]string{
		"foreign origin":  {"Origin": "https://evil.example"},
		"cross-site":      {"Sec-Fetch-Site": "cross-site"},
		"same-site only":  {"Sec-Fetch-Site": "same-site"},
		"foreign + fetch": {"Origin": "https://evil.example", "Sec-Fetch-Site": "same-origin"},
	} {
		expectProblem(t, login(headers), 403, "cross_site_request")
		_ = name
	}
	for name, headers := range map[string]map[string]string{
		"configured origin": {"Origin": localOrigin},
		"origin case":       {"Origin": strings.ToUpper(localOrigin[:4]) + localOrigin[4:]},
		"same origin":       {"Sec-Fetch-Site": "same-origin"},
		"user navigation":   {"Sec-Fetch-Site": "none"},
		"no browser":        nil,
	} {
		if r := login(headers); r.Status != 401 {
			t.Errorf("%s = %d, want the request to reach the handler (401)", name, r.Status)
		}
	}
	if r := a.do(request{method: http.MethodGet, path: "/api/v1/setup/status", headers: map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}}); r.Status != 200 {
		t.Errorf("a safe cross-site request = %d, want it served", r.Status)
	}
}

// TestRateLimits verifies brute-force protection.
//
// Scenarios:
//   - the fourth attempt on one account is refused with Retry-After even though each attempt fails
//   - other accounts from the same address are still served until the address limit is reached
//   - waiting earns attempts back
//   - the setup endpoint is limited per address
func TestRateLimits(t *testing.T) {
	a := newAPI(t)
	a.setup()
	attempt := func(email string) reply {
		return a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": email, "password": "wrong password here"}})
	}

	for range 3 {
		expectProblem(t, attempt(adminEmail), 401, "invalid_credentials")
	}
	blocked := attempt(adminEmail)
	expectProblem(t, blocked, 429, "rate_limited")
	if retry := blocked.Header.Get("Retry-After"); retry == "" || retry == "0" {
		t.Errorf("Retry-After = %q, want a positive number of seconds", retry)
	}
	expectProblem(t, attempt("second@example.com"), 401, "invalid_credentials")
	expectProblem(t, attempt("third@example.com"), 429, "rate_limited")

	a.clock.Advance(time.Minute)
	expectProblem(t, attempt(adminEmail), 401, "invalid_credentials")

	limited := newAPI(t)
	for range 5 {
		limited.do(request{method: http.MethodPost, path: "/api/v1/setup", body: map[string]string{"email": "bad", "password": "short"}})
	}
	expectProblem(t, limited.do(request{method: http.MethodPost, path: "/api/v1/setup", body: map[string]string{"email": adminEmail, "password": adminPassword}}), 429, "rate_limited")
}

// TestClientAddress verifies which address rate limits are keyed on.
//
// Scenarios:
//   - without trusted proxies X-Forwarded-For is ignored, so spoofing it does not evade the limit
//   - behind a trusted proxy the forwarded address is used, so each client has its own limit
func TestClientAddress(t *testing.T) {
	attempts := func(a *api, spoof bool) int {
		limited := 0
		for i := range 8 {
			headers := map[string]string{}
			if spoof {
				headers["X-Forwarded-For"] = "203.0.113." + string(rune('1'+i))
			}
			r := a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", headers: headers,
				body: map[string]string{"email": "user" + string(rune('a'+i)) + "@example.com", "password": "wrong password here"}})
			if r.Status == 429 {
				limited++
			}
		}
		return limited
	}

	direct := newAPI(t)
	if got := attempts(direct, true); got != 3 {
		t.Errorf("without trusted proxies %d of 8 spoofed attempts were limited, want 3", got)
	}
	proxied := newAPI(t, func(d *httpapi.Deps) { d.TrustedProxies = []string{"127.0.0.0/8"} })
	if got := attempts(proxied, true); got != 0 {
		t.Errorf("behind a trusted proxy %d of 8 attempts from different clients were limited, want 0", got)
	}
}

// TestCookieSecureFlag verifies that the session cookie is https only when configured so.
func TestCookieSecureFlag(t *testing.T) {
	a := newAPI(t, func(d *httpapi.Deps) { d.Cookie.Secure = true })

	cookie, _ := a.setup()

	if !cookie.Secure {
		t.Error("the cookie is not Secure although the deployment uses https")
	}
}

// TestSessionsExpire verifies expiry through the API: an idle session stops working.
func TestSessionsExpire(t *testing.T) {
	a := newAPI(t)
	cookie, _ := a.setup()

	a.clock.Advance(20 * time.Minute)
	if r := a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}); r.Status != 200 {
		t.Fatalf("active session = %d", r.Status)
	}
	a.clock.Advance(31 * time.Minute)
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}), 401, "unauthenticated")
}

// TestDisabledAccounts verifies that a disabled account cannot sign in and loses its sessions' effect.
func TestDisabledAccounts(t *testing.T) {
	a := newAPI(t)
	hash, _ := a.hash.Hash(t.Context(), adminPassword)
	id, _ := uuid.NewV7()
	user := auth.User{ID: id, Email: "off@example.com", PasswordHash: hash, Role: auth.RoleUser, Disabled: true, Locale: "en", Timezone: "UTC", CreatedAt: a.clock.Now()}
	if err := a.store.CreateUser(t.Context(), user); err != nil {
		t.Fatal(err)
	}

	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": "off@example.com", "password": adminPassword}}), 403, "user_disabled")

	user.Disabled, user.ID, user.Email = false, uuid.New(), "on@example.com"
	_ = a.store.CreateUser(t.Context(), user)
	login := a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": "on@example.com", "password": adminPassword}})
	cookie := login.sessionCookie()
	if cookie == nil {
		t.Fatalf("login = %d %s", login.Status, login.Body)
	}
	if _, err := a.store.DB().Exec(t.Context(), "UPDATE users SET status = 'disabled' WHERE email = 'on@example.com'"); err != nil {
		t.Fatal(err)
	}
	expectProblem(t, a.do(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}), 401, "unauthenticated")
}

// TestInfrastructureFailures verifies that database failures are reported as 500 without details.
//
// Scenarios:
//   - endpoints that need the store answer internal_error and never expose the cause
//   - a request carrying a session cookie also fails cleanly when the session lookup fails
func TestInfrastructureFailures(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	a.store.Close()

	for name, r := range map[string]request{
		"setup status": {method: http.MethodGet, path: "/api/v1/setup/status"},
		"setup":        {method: http.MethodPost, path: "/api/v1/setup", body: map[string]string{"email": "z@example.com", "password": adminPassword}},
		"login":        {method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": adminEmail, "password": adminPassword}},
		"session":      {method: http.MethodGet, path: "/api/v1/me", cookie: cookie},
		"logout":       {method: http.MethodPost, path: "/api/v1/auth/logout", cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf}},
	} {
		reply := a.do(r)
		if reply.Status != 500 || reply.problem(t).Code != "internal_error" || strings.Contains(string(reply.Body), "closed") {
			t.Errorf("%s = %d %s, want a plain internal_error", name, reply.Status, reply.Body)
		}
	}
}

// TestContract_AuthEndpointsMatchTheSpec verifies that real responses of the authentication endpoints,
// successes and errors, satisfy api/openapi.yaml, including which status codes are declared.
func TestContract_AuthEndpointsMatchTheSpec(t *testing.T) {
	_, router := loadSpec(t)
	a := newAPI(t)
	check := func(r request, want int) reply {
		t.Helper()
		got := a.do(r)
		if got.Status != want {
			t.Fatalf("%s %s = %d %s, want %d", r.method, r.path, got.Status, got.Body, want)
		}
		lookup := httptest.NewRequestWithContext(t.Context(), r.method, r.path, nil)
		route, params, err := router.FindRoute(lookup)
		if err != nil {
			t.Fatalf("finding %s %s in the spec: %v", r.method, r.path, err)
		}
		input := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: &openapi3filter.RequestValidationInput{Request: lookup, PathParams: params, Route: route},
			Status:                 got.Status,
			Header:                 got.Header,
			Options:                &openapi3filter.Options{IncludeResponseStatus: true},
		}
		input.SetBodyBytes(got.Body)
		if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
			t.Errorf("%s %s (%d) does not satisfy the spec: %v", r.method, r.path, got.Status, err)
		}
		return got
	}
	creds := func(email, password string) map[string]string {
		return map[string]string{"email": email, "password": password}
	}

	check(request{method: http.MethodGet, path: "/api/v1/setup/status"}, 200)
	check(request{method: http.MethodPost, path: "/api/v1/setup", body: creds("bad", adminPassword)}, 422)
	check(request{method: http.MethodPost, path: "/api/v1/setup", body: "{"}, 400)
	created := check(request{method: http.MethodPost, path: "/api/v1/setup", body: creds(adminEmail, adminPassword)}, 201)
	check(request{method: http.MethodPost, path: "/api/v1/setup", body: creds("b@example.com", adminPassword)}, 409)
	cookie, csrf := created.sessionCookie(), created.session(t).CsrfToken
	check(request{method: http.MethodPost, path: "/api/v1/auth/login", body: creds(adminEmail, "wrong password here")}, 401)
	check(request{method: http.MethodPost, path: "/api/v1/auth/login", body: creds(adminEmail, adminPassword)}, 200)
	check(request{method: http.MethodGet, path: "/api/v1/me", cookie: cookie}, 200)
	check(request{method: http.MethodGet, path: "/api/v1/me"}, 401)
	check(request{method: http.MethodPatch, path: "/api/v1/me", cookie: cookie, body: map[string]string{"locale": "en"}, headers: map[string]string{"X-CSRF-Token": csrf}}, 200)
	check(request{method: http.MethodPatch, path: "/api/v1/me", cookie: cookie, body: map[string]string{"locale": "x!"}, headers: map[string]string{"X-CSRF-Token": csrf}}, 422)
	check(request{method: http.MethodPatch, path: "/api/v1/me", cookie: cookie, body: map[string]string{"locale": "en"}}, 403)
	for range 3 {
		a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: creds(adminEmail, "wrong password here")})
	}
	check(request{method: http.MethodPost, path: "/api/v1/auth/login", body: creds(adminEmail, "wrong password here")}, 429)
	check(request{method: http.MethodPost, path: "/api/v1/auth/logout", cookie: cookie, headers: map[string]string{"X-CSRF-Token": csrf}}, 204)
}

// mustDecode parses JSON or fails the test.
func mustDecode(t *testing.T, raw []byte, into any) {
	t.Helper()
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decoding %q: %v", raw, err)
	}
}

// memConnector serves one in-memory file system per site, after checking that the site exists like the real
// connector does, and that a site asking for its password got one in the session. The file systems outlive the
// connections that use them.
type memConnector struct {
	clock ports.Clock
	sites *site.Service
	quick *quick.Service
	asked *credentials.Store
	mu    *sync.Mutex
	byID  map[uuid.UUID]*sharedFS
}

// sharedFS is an in-memory file system that stays open when a connection closes it.
type sharedFS struct{ *memfs.FS }

// Close does nothing: the data outlives the connection.
func (sharedFS) Close() error { return nil }

// selfWrites counts the downloads whose file wrote itself to the response.
var selfWrites atomic.Int64

// OpenRead returns a stream that can write itself, as an SFTP file reading ahead does, so the tests see whether a download uses it.
func (f sharedFS) OpenRead(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	r, err := f.FS.OpenRead(ctx, p, offset)
	if err != nil {
		return nil, err
	}
	return selfWriting{r}, nil
}

// selfWriting is a stream that writes itself to a destination.
type selfWriting struct{ io.ReadCloser }

// WriteTo copies the rest of the stream and counts the copy.
func (s selfWriting) WriteTo(w io.Writer) (int64, error) {
	selfWrites.Add(1)
	return io.Copy(w, struct{ io.Reader }{s.ReadCloser})
}

// newMemConnector returns a connector over the sites service.
func newMemConnector(clock ports.Clock, sites *site.Service, quickConnections *quick.Service, asked *credentials.Store) memConnector {
	return memConnector{clock: clock, sites: sites, quick: quickConnections, asked: asked, mu: &sync.Mutex{}, byID: map[uuid.UUID]*sharedFS{}}
}

// slowSite is the name of a site whose connection never opens: connecting waits until the request is given up.
const slowSite = "Never answers"

// strictSite is the name of a site whose server's sizes are not trusted, as with a strict SFTP gateway.
const strictSite = "Strict gateway"

// strictFS is a connection to a server whose sizes are not trusted.
type strictFS struct{ *sharedFS }

// SizesTrusted implements [remotefs.SizeChecker].
func (strictFS) SizesTrusted() bool { return false }

// NoteWholeRead implements [remotefs.SizeChecker].
func (strictFS) NoteWholeRead(int64, int64) {}

// Connect returns a connection to the file system of the site; one to the site named [slowSite] waits for the request
// to end, and one to the site named [strictSite] does not trust its sizes.
func (c memConnector) Connect(ctx context.Context, key connections.Key, session string) (remotefs.RemoteFS, error) {
	strict := false
	if _, ok := c.quick.Describe(key.UserID, key.WorkspaceID, key.SiteID); !ok {
		found, err := c.sites.Get(ctx, key.UserID, key.WorkspaceID, key.SiteID)
		if err != nil {
			return nil, err
		}
		if _, typed := c.asked.Password(credentials.Key{Session: session, UserID: key.UserID, WorkspaceID: key.WorkspaceID, SiteID: key.SiteID}); found.AuthMethod == site.AuthAsk && !typed {
			return nil, credentials.ErrRequired
		}
		if found.Name == slowSite {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		strict = found.Name == strictSite
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fsys, ok := c.byID[key.SiteID]
	if !ok {
		fsys = &sharedFS{memfs.New(c.clock)}
		c.byID[key.SiteID] = fsys
	}
	if strict {
		return strictFS{fsys}, nil
	}
	return fsys, nil
}

// recordedEvents keeps the events the server published.
type recordedEvents struct {
	mu   sync.Mutex
	list []ports.Event
}

// Publish records an event.
func (r *recordedEvents) Publish(e ports.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.list = append(r.list, e)
}

// ofType returns the recorded events of a type.
func (r *recordedEvents) ofType(kind string) []ports.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ports.Event
	for _, e := range r.list {
		if e.Type == kind {
			out = append(out, e)
		}
	}
	return out
}

// fanout publishes every event to several sinks.
type fanout []ports.EventSink

// Publish hands the event to every sink.
func (f fanout) Publish(e ports.Event) {
	for _, s := range f {
		s.Publish(e)
	}
}
